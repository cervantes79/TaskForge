package taskforge

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// worker represents a single worker that processes jobs from the queue
type worker struct {
	id       int
	taskForge *TaskForge
	active   bool
	mu       sync.RWMutex
}

// newWorker creates a new worker instance
func newWorker(id int, tf *TaskForge) *worker {
	return &worker{
		id:        id,
		taskForge: tf,
		active:    false,
	}
}

// isActive returns whether the worker is currently processing a job
func (w *worker) isActive() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.active
}

// setActive sets the worker's active status
func (w *worker) setActive(active bool) {
	w.mu.Lock()
	w.active = active
	w.mu.Unlock()
}

// start begins the worker's main processing loop
func (w *worker) start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case job := <-w.taskForge.jobQueue:
			w.processJob(ctx, job)
		case job := <-w.taskForge.retryQueue:
			w.processJob(ctx, job)
		}
	}
}

// processJob handles the execution of a single job
func (w *worker) processJob(ctx context.Context, job *Job) {
	w.setActive(true)
	defer w.setActive(false)

	startTime := time.Now()
	job.Status = StatusProcessing
	job.Attempts++

	// Get handler for this job type
	w.taskForge.mu.RLock()
	handler, exists := w.taskForge.handlers[job.Type]
	w.taskForge.mu.RUnlock()

	if !exists {
		w.handleJobError(job, fmt.Errorf("no handler registered for job type: %s", job.Type))
		return
	}

	// Create a context with timeout for job execution
	jobCtx, cancel := context.WithTimeout(ctx, time.Minute*5) // 5 minute timeout
	defer cancel()

	// Execute the job
	err := handler.Handle(jobCtx, job)
	execTime := time.Since(startTime)

	if err != nil {
		w.handleJobError(job, err)
		log.Printf("Worker %d: Job %s failed after %v: %v", w.id, job.ID, execTime, err)
	} else {
		w.handleJobSuccess(job, execTime)
		log.Printf("Worker %d: Job %s completed successfully in %v", w.id, job.ID, execTime)
	}
}

// handleJobSuccess processes a successfully completed job
func (w *worker) handleJobSuccess(job *Job, execTime time.Duration) {
	now := time.Now()
	job.Status = StatusCompleted
	job.ProcessedAt = &now
	job.LastError = ""

	w.taskForge.metrics.incrementProcessed()
	
	// Update average execution time (simple moving average approximation)
	w.taskForge.metrics.mutex.Lock()
	if w.taskForge.metrics.AverageExecTime == 0 {
		w.taskForge.metrics.AverageExecTime = execTime.Milliseconds()
	} else {
		// Simple exponential moving average
		w.taskForge.metrics.AverageExecTime = (w.taskForge.metrics.AverageExecTime*9 + execTime.Milliseconds()) / 10
	}
	w.taskForge.metrics.mutex.Unlock()
}

// handleJobError processes a failed job, implementing retry logic
func (w *worker) handleJobError(job *Job, err error) {
	job.LastError = err.Error()
	w.taskForge.metrics.incrementFailed()

	if job.Attempts >= job.MaxRetries {
		// Max retries reached, send to dead letter queue
		job.Status = StatusDeadLetter
		now := time.Now()
		job.ProcessedAt = &now

		w.taskForge.dlqMutex.Lock()
		w.taskForge.dlq[job.ID] = job
		w.taskForge.dlqMutex.Unlock()

		w.taskForge.metrics.incrementDLQ()
		log.Printf("Worker %d: Job %s moved to dead letter queue after %d attempts", w.id, job.ID, job.Attempts)
	} else {
		// Schedule for retry with exponential backoff
		job.Status = StatusRetrying
		w.taskForge.metrics.incrementRetried()

		retryDelay := w.calculateRetryDelay(job.Attempts)
		log.Printf("Worker %d: Job %s will retry in %v (attempt %d/%d)", w.id, job.ID, retryDelay, job.Attempts, job.MaxRetries)

		go w.scheduleRetry(job, retryDelay)
	}
}

// calculateRetryDelay implements exponential backoff for retry delays
func (w *worker) calculateRetryDelay(attempt int) time.Duration {
	baseDelay := w.taskForge.config.RetryDelay
	maxDelay := w.taskForge.config.MaxRetryDelay

	// Exponential backoff: delay = baseDelay * 2^(attempt-1)
	multiplier := 1 << uint(attempt-1)
	delay := time.Duration(int64(baseDelay) * int64(multiplier))

	// Cap at maximum delay
	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

// scheduleRetry schedules a job for retry after the specified delay
func (w *worker) scheduleRetry(job *Job, delay time.Duration) {
	select {
	case <-time.After(delay):
		select {
		case w.taskForge.retryQueue <- job:
		case <-w.taskForge.ctx.Done():
		}
	case <-w.taskForge.ctx.Done():
	}
}

// Start initializes and starts all workers
func (tf *TaskForge) Start() error {
	tf.mu.Lock()
	defer tf.mu.Unlock()

	if tf.running {
		return fmt.Errorf("TaskForge is already running")
	}

	tf.running = true
	tf.workers = make([]*worker, tf.config.WorkerCount)

	// Start workers
	for i := 0; i < tf.config.WorkerCount; i++ {
		tf.workers[i] = newWorker(i, tf)
		tf.wg.Add(1)
		go tf.workers[i].start(tf.ctx, &tf.wg)
	}

	log.Printf("TaskForge started with %d workers", tf.config.WorkerCount)
	return nil
}

// Stop gracefully shuts down TaskForge
func (tf *TaskForge) Stop() error {
	tf.mu.Lock()
	if !tf.running {
		tf.mu.Unlock()
		return fmt.Errorf("TaskForge is not running")
	}
	tf.running = false
	tf.mu.Unlock()

	log.Printf("TaskForge shutting down...")

	// Cancel context to stop workers
	tf.cancel()

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		tf.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("TaskForge stopped gracefully")
	case <-time.After(tf.config.ShutdownTimeout):
		log.Printf("TaskForge shutdown timeout exceeded")
	}

	return nil
}

// GetActiveWorkers returns the number of currently active workers
func (tf *TaskForge) GetActiveWorkers() int {
	tf.mu.RLock()
	defer tf.mu.RUnlock()

	if !tf.running {
		return 0
	}

	activeCount := 0
	for _, worker := range tf.workers {
		if worker.isActive() {
			activeCount++
		}
	}

	return activeCount
}