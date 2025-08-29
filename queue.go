package taskforge

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Enqueue adds a new job to the processing queue
func (tf *TaskForge) Enqueue(jobType string, payload map[string]interface{}) (*Job, error) {
	return tf.EnqueueWithOptions(jobType, payload, nil)
}

// EnqueueOptions provides additional options when enqueueing jobs
type EnqueueOptions struct {
	MaxRetries *int           // Override default max retries
	Priority   *int           // Job priority (higher = more important)
	Delay      *time.Duration // Delay before processing
}

// EnqueueWithOptions adds a job to the queue with custom options
func (tf *TaskForge) EnqueueWithOptions(jobType string, payload map[string]interface{}, options *EnqueueOptions) (*Job, error) {
	tf.mu.RLock()
	if !tf.running {
		tf.mu.RUnlock()
		return nil, ErrQueueClosed
	}
	tf.mu.RUnlock()

	// Check if handler exists
	tf.mu.RLock()
	_, exists := tf.handlers[jobType]
	tf.mu.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("no handler registered for job type: %s", jobType)
	}

	job := &Job{
		ID:        uuid.New().String(),
		Type:      jobType,
		Payload:   payload,
		Status:    StatusPending,
		CreatedAt: time.Now(),
		Attempts:  0,
		Priority:  0,
		Delay:     0,
	}

	// Apply default max retries
	job.MaxRetries = tf.config.MaxRetries

	// Apply options if provided
	if options != nil {
		if options.MaxRetries != nil {
			job.MaxRetries = *options.MaxRetries
		}
		if options.Priority != nil {
			job.Priority = *options.Priority
		}
		if options.Delay != nil {
			job.Delay = *options.Delay
		}
	}

	// Handle delayed jobs
	if job.Delay > 0 {
		go func() {
			select {
			case <-time.After(job.Delay):
				select {
				case tf.jobQueue <- job:
				case <-tf.ctx.Done():
				}
			case <-tf.ctx.Done():
			}
		}()
	} else {
		select {
		case tf.jobQueue <- job:
		case <-tf.ctx.Done():
			return nil, ErrQueueClosed
		default:
			return nil, fmt.Errorf("queue is full")
		}
	}

	return job, nil
}

// GetQueueLength returns the current number of jobs waiting in the queue
func (tf *TaskForge) GetQueueLength() int {
	return len(tf.jobQueue)
}

// GetRetryQueueLength returns the current number of jobs in the retry queue
func (tf *TaskForge) GetRetryQueueLength() int {
	return len(tf.retryQueue)
}

// GetDeadLetterQueue returns a copy of all jobs in the dead letter queue
func (tf *TaskForge) GetDeadLetterQueue() map[string]*Job {
	tf.dlqMutex.RLock()
	defer tf.dlqMutex.RUnlock()

	dlqCopy := make(map[string]*Job)
	for id, job := range tf.dlq {
		dlqCopy[id] = job
	}
	return dlqCopy
}

// RequeueFromDLQ moves a job from the dead letter queue back to the main queue
func (tf *TaskForge) RequeueFromDLQ(jobID string) error {
	tf.dlqMutex.Lock()
	job, exists := tf.dlq[jobID]
	if !exists {
		tf.dlqMutex.Unlock()
		return ErrJobNotFound
	}
	delete(tf.dlq, jobID)
	tf.dlqMutex.Unlock()

	// Reset job status and attempts for reprocessing
	job.Status = StatusPending
	job.Attempts = 0
	job.LastError = ""

	select {
	case tf.jobQueue <- job:
		tf.metrics.mutex.Lock()
		tf.metrics.JobsInDLQ--
		tf.metrics.mutex.Unlock()
		return nil
	case <-tf.ctx.Done():
		return ErrQueueClosed
	default:
		// Put it back in DLQ if queue is full
		tf.dlqMutex.Lock()
		tf.dlq[jobID] = job
		tf.dlqMutex.Unlock()
		return fmt.Errorf("queue is full, job returned to DLQ")
	}
}

// ClearDeadLetterQueue removes all jobs from the dead letter queue
func (tf *TaskForge) ClearDeadLetterQueue() int {
	tf.dlqMutex.Lock()
	defer tf.dlqMutex.Unlock()

	count := len(tf.dlq)
	tf.dlq = make(map[string]*Job)
	
	tf.metrics.mutex.Lock()
	tf.metrics.JobsInDLQ = 0
	tf.metrics.mutex.Unlock()

	return count
}