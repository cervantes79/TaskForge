// Package taskforge provides a high-performance background job processing system
// with concurrent workers, retry logic, and dead letter queue capabilities.
// It offers significant performance improvements over Python-based solutions like Celery.
package taskforge

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"time"
)

var (
	ErrJobNotFound    = errors.New("job not found")
	ErrQueueClosed    = errors.New("queue is closed")
	ErrInvalidHandler = errors.New("invalid job handler")
)

// JobStatus represents the current state of a job
type JobStatus int

const (
	StatusPending JobStatus = iota
	StatusProcessing
	StatusCompleted
	StatusFailed
	StatusRetrying
	StatusDeadLetter
)

func (s JobStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusProcessing:
		return "processing"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	case StatusRetrying:
		return "retrying"
	case StatusDeadLetter:
		return "dead_letter"
	default:
		return "unknown"
	}
}

// Job represents a background task to be processed
type Job struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Payload     map[string]interface{} `json:"payload"`
	Status      JobStatus              `json:"status"`
	CreatedAt   time.Time              `json:"created_at"`
	ProcessedAt *time.Time             `json:"processed_at,omitempty"`
	Attempts    int                    `json:"attempts"`
	MaxRetries  int                    `json:"max_retries"`
	LastError   string                 `json:"last_error,omitempty"`
	Priority    int                    `json:"priority"` // Higher values = higher priority
	Delay       time.Duration          `json:"delay"`    // Delay before processing
}

// JobHandler defines the interface for processing jobs
type JobHandler interface {
	Handle(ctx context.Context, job *Job) error
}

// JobHandlerFunc is an adapter to allow ordinary functions to be JobHandlers
type JobHandlerFunc func(ctx context.Context, job *Job) error

func (f JobHandlerFunc) Handle(ctx context.Context, job *Job) error {
	return f(ctx, job)
}

// Config holds configuration options for the TaskForge instance
type Config struct {
	WorkerCount        int           // Number of concurrent workers
	QueueSize          int           // Size of the job queue buffer
	MaxRetries         int           // Default maximum retry attempts
	RetryDelay         time.Duration // Base delay between retries
	MaxRetryDelay      time.Duration // Maximum delay between retries
	DeadLetterQueueTTL time.Duration // How long to keep jobs in DLQ
	ShutdownTimeout    time.Duration // Timeout for graceful shutdown
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		WorkerCount:        runtime.NumCPU(),
		QueueSize:          1000,
		MaxRetries:         3,
		RetryDelay:         time.Second * 5,
		MaxRetryDelay:      time.Minute * 10,
		DeadLetterQueueTTL: time.Hour * 24,
		ShutdownTimeout:    time.Second * 30,
	}
}

// TaskForge is the main job processing engine
type TaskForge struct {
	config     *Config
	handlers   map[string]JobHandler
	jobQueue   chan *Job
	retryQueue chan *Job
	dlq        map[string]*Job // Dead Letter Queue
	dlqMutex   sync.RWMutex
	workers    []*worker
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	metrics    *Metrics
	running    bool
	mu         sync.RWMutex
}

// Metrics holds performance and operational metrics
type Metrics struct {
	JobsProcessed   int64 `json:"jobs_processed"`
	JobsFailed      int64 `json:"jobs_failed"`
	JobsRetried     int64 `json:"jobs_retried"`
	JobsInDLQ       int64 `json:"jobs_in_dlq"`
	AverageExecTime int64 `json:"avg_exec_time_ms"`
	WorkersActive   int   `json:"workers_active"`
	QueueLength     int   `json:"queue_length"`
	mutex           sync.RWMutex
}

// GetMetrics returns a copy of current metrics
func (m *Metrics) GetMetrics() Metrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return Metrics{
		JobsProcessed:   m.JobsProcessed,
		JobsFailed:      m.JobsFailed,
		JobsRetried:     m.JobsRetried,
		JobsInDLQ:       m.JobsInDLQ,
		AverageExecTime: m.AverageExecTime,
		WorkersActive:   m.WorkersActive,
		QueueLength:     m.QueueLength,
	}
}

func (m *Metrics) incrementProcessed() {
	m.mutex.Lock()
	m.JobsProcessed++
	m.mutex.Unlock()
}

func (m *Metrics) incrementFailed() {
	m.mutex.Lock()
	m.JobsFailed++
	m.mutex.Unlock()
}

func (m *Metrics) incrementRetried() {
	m.mutex.Lock()
	m.JobsRetried++
	m.mutex.Unlock()
}

func (m *Metrics) incrementDLQ() {
	m.mutex.Lock()
	m.JobsInDLQ++
	m.mutex.Unlock()
}

// New creates a new TaskForge instance with the given configuration
func New(config *Config) *TaskForge {
	if config == nil {
		config = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &TaskForge{
		config:     config,
		handlers:   make(map[string]JobHandler),
		jobQueue:   make(chan *Job, config.QueueSize),
		retryQueue: make(chan *Job, config.QueueSize/2),
		dlq:        make(map[string]*Job),
		ctx:        ctx,
		cancel:     cancel,
		metrics:    &Metrics{},
	}
}

// RegisterHandler registers a job handler for a specific job type
func (tf *TaskForge) RegisterHandler(jobType string, handler JobHandler) error {
	if handler == nil {
		return ErrInvalidHandler
	}

	tf.mu.Lock()
	tf.handlers[jobType] = handler
	tf.mu.Unlock()

	return nil
}

// RegisterHandlerFunc is a convenience method to register a function as a job handler
func (tf *TaskForge) RegisterHandlerFunc(jobType string, handlerFunc func(ctx context.Context, job *Job) error) error {
	return tf.RegisterHandler(jobType, JobHandlerFunc(handlerFunc))
}

// GetMetrics returns a copy of current metrics
func (tf *TaskForge) GetMetrics() Metrics {
	return tf.metrics.GetMetrics()
}