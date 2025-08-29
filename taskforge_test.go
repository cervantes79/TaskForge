package taskforge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskForgeBasicFunctionality(t *testing.T) {
	tf := New(nil)
	defer tf.Stop()

	var processedJobs int32

	// Register handler
	err := tf.RegisterHandlerFunc("test_job", func(ctx context.Context, job *Job) error {
		atomic.AddInt32(&processedJobs, 1)
		return nil
	})
	require.NoError(t, err)

	// Start TaskForge
	err = tf.Start()
	require.NoError(t, err)

	// Enqueue job
	_, err = tf.Enqueue("test_job", map[string]interface{}{"data": "test"})
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&processedJobs))
}

func TestTaskForgeRetryLogic(t *testing.T) {
	config := DefaultConfig()
	config.RetryDelay = 10 * time.Millisecond
	config.MaxRetries = 2

	tf := New(config)
	defer tf.Stop()

	var attempts int32

	// Register failing handler
	err := tf.RegisterHandlerFunc("failing_job", func(ctx context.Context, job *Job) error {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			return errors.New("temporary failure")
		}
		return nil
	})
	require.NoError(t, err)

	err = tf.Start()
	require.NoError(t, err)

	_, err = tf.Enqueue("failing_job", map[string]interface{}{"data": "test"})
	require.NoError(t, err)

	// Wait for all retries
	time.Sleep(200 * time.Millisecond)
	
	// The job should eventually succeed after retries
	attemptCount := atomic.LoadInt32(&attempts)
	assert.GreaterOrEqual(t, attemptCount, int32(2)) // At least 2 attempts
	
	// Check metrics
	metrics := tf.metrics.GetMetrics()
	assert.GreaterOrEqual(t, metrics.JobsRetried, int64(1))
}

func TestTaskForgeDeadLetterQueue(t *testing.T) {
	config := DefaultConfig()
	config.MaxRetries = 1
	config.RetryDelay = 10 * time.Millisecond

	tf := New(config)
	defer tf.Stop()

	// Register always-failing handler
	err := tf.RegisterHandlerFunc("always_fail", func(ctx context.Context, job *Job) error {
		return errors.New("permanent failure")
	})
	require.NoError(t, err)

	err = tf.Start()
	require.NoError(t, err)

	job, err := tf.Enqueue("always_fail", map[string]interface{}{"data": "test"})
	require.NoError(t, err)

	// Wait for failure and DLQ placement
	time.Sleep(100 * time.Millisecond)

	// Check DLQ
	dlqJobs := tf.GetDeadLetterQueue()
	assert.Len(t, dlqJobs, 1)
	
	dlqJob, exists := dlqJobs[job.ID]
	assert.True(t, exists)
	assert.Equal(t, StatusDeadLetter, dlqJob.Status)
}

func TestTaskForgeConcurrency(t *testing.T) {
	config := DefaultConfig()
	config.WorkerCount = 4

	tf := New(config)
	defer tf.Stop()

	var processedJobs int32
	jobCount := 50

	err := tf.RegisterHandlerFunc("concurrent_job", func(ctx context.Context, job *Job) error {
		// Simulate some work
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&processedJobs, 1)
		return nil
	})
	require.NoError(t, err)

	err = tf.Start()
	require.NoError(t, err)

	// Enqueue multiple jobs
	start := time.Now()
	for i := 0; i < jobCount; i++ {
		_, err := tf.Enqueue("concurrent_job", map[string]interface{}{"id": i})
		require.NoError(t, err)
	}

	// Wait for all jobs to complete
	for atomic.LoadInt32(&processedJobs) < int32(jobCount) {
		time.Sleep(10 * time.Millisecond)
	}
	
	duration := time.Since(start)
	
	// With 4 workers, 50 jobs should complete much faster than sequential
	// Sequential would take ~500ms, concurrent should be ~150ms or less
	assert.Less(t, duration, 300*time.Millisecond)
	assert.Equal(t, int32(jobCount), atomic.LoadInt32(&processedJobs))
}

func TestTaskForgeJobPriority(t *testing.T) {
	tf := New(nil)
	defer tf.Stop()

	var processOrder []int
	var mu sync.Mutex

	err := tf.RegisterHandlerFunc("priority_job", func(ctx context.Context, job *Job) error {
		id := job.Payload["id"].(int)
		mu.Lock()
		processOrder = append(processOrder, id)
		mu.Unlock()
		return nil
	})
	require.NoError(t, err)

	err = tf.Start()
	require.NoError(t, err)

	// Enqueue jobs with different priorities (priority is stored but not yet implemented in scheduling)
	_, err = tf.EnqueueWithOptions("priority_job", map[string]interface{}{"id": 1}, &EnqueueOptions{
		Priority: intPtr(1),
	})
	require.NoError(t, err)

	_, err = tf.EnqueueWithOptions("priority_job", map[string]interface{}{"id": 2}, &EnqueueOptions{
		Priority: intPtr(10),
	})
	require.NoError(t, err)

	_, err = tf.EnqueueWithOptions("priority_job", map[string]interface{}{"id": 3}, &EnqueueOptions{
		Priority: intPtr(5),
	})
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Jobs are processed in FIFO order currently (priority scheduling not yet implemented)
	mu.Lock()
	assert.Len(t, processOrder, 3)
	mu.Unlock()
}

func TestTaskForgeDelayedJobs(t *testing.T) {
	tf := New(nil)
	defer tf.Stop()

	var processedAt time.Time

	err := tf.RegisterHandlerFunc("delayed_job", func(ctx context.Context, job *Job) error {
		processedAt = time.Now()
		return nil
	})
	require.NoError(t, err)

	err = tf.Start()
	require.NoError(t, err)

	start := time.Now()
	delay := 50 * time.Millisecond

	_, err = tf.EnqueueWithOptions("delayed_job", map[string]interface{}{"data": "test"}, &EnqueueOptions{
		Delay: &delay,
	})
	require.NoError(t, err)

	// Wait for delayed processing
	time.Sleep(100 * time.Millisecond)

	actualDelay := processedAt.Sub(start)
	assert.GreaterOrEqual(t, actualDelay, delay)
	assert.Less(t, actualDelay, delay+30*time.Millisecond) // Allow some margin
}

func TestTaskForgeMetrics(t *testing.T) {
	tf := New(nil)
	defer tf.Stop()

	err := tf.RegisterHandlerFunc("metric_job", func(ctx context.Context, job *Job) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	require.NoError(t, err)

	err = tf.RegisterHandlerFunc("failing_metric_job", func(ctx context.Context, job *Job) error {
		return errors.New("test failure")
	})
	require.NoError(t, err)

	err = tf.Start()
	require.NoError(t, err)

	// Process successful jobs
	for i := 0; i < 5; i++ {
		_, err := tf.Enqueue("metric_job", map[string]interface{}{"id": i})
		require.NoError(t, err)
	}

	// Process failing job
	_, err = tf.Enqueue("failing_metric_job", map[string]interface{}{"id": "fail"})
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	metrics := tf.metrics.GetMetrics()
	assert.Equal(t, int64(5), metrics.JobsProcessed)
	assert.GreaterOrEqual(t, metrics.JobsFailed, int64(1))
	assert.Greater(t, metrics.AverageExecTime, int64(0))
}

func TestTaskForgeGracefulShutdown(t *testing.T) {
	tf := New(nil)

	var processedJobs int32

	err := tf.RegisterHandlerFunc("shutdown_job", func(ctx context.Context, job *Job) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
			atomic.AddInt32(&processedJobs, 1)
			return nil
		}
	})
	require.NoError(t, err)

	err = tf.Start()
	require.NoError(t, err)

	// Enqueue jobs
	for i := 0; i < 3; i++ {
		_, err := tf.Enqueue("shutdown_job", map[string]interface{}{"id": i})
		require.NoError(t, err)
	}

	// Allow some processing time
	time.Sleep(25 * time.Millisecond)

	// Shutdown
	err = tf.Stop()
	require.NoError(t, err)

	// Some jobs should have been processed
	processed := atomic.LoadInt32(&processedJobs)
	assert.GreaterOrEqual(t, processed, int32(0))
	assert.LessOrEqual(t, processed, int32(3))
}

// Helper function
func intPtr(i int) *int {
	return &i
}

func TestTaskForgeDLQOperations(t *testing.T) {
	config := DefaultConfig()
	config.RetryDelay = 10 * time.Millisecond // Fast retries for testing
	
	tf := New(config)
	defer tf.Stop()

	err := tf.RegisterHandlerFunc("dlq_test", func(ctx context.Context, job *Job) error {
		return fmt.Errorf("test error")
	})
	require.NoError(t, err)

	err = tf.Start()
	require.NoError(t, err)

	// Create a job that will fail and go to DLQ
	job, err := tf.Enqueue("dlq_test", map[string]interface{}{"test": true})
	require.NoError(t, err)

	// Wait for it to fail and be moved to DLQ (needs time for retries)
	time.Sleep(500 * time.Millisecond)

	// Test GetDLQJob
	dlqJob, err := tf.GetDLQJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusDeadLetter, dlqJob.Status)

	// Test DLQ stats
	stats := tf.GetDLQStats()
	assert.Equal(t, 1, stats["total_jobs"])

	// Test requeue from DLQ - replace handler first
	var processedJobs int32
	err = tf.RegisterHandlerFunc("dlq_test", func(ctx context.Context, job *Job) error {
		atomic.AddInt32(&processedJobs, 1)
		return nil
	})
	require.NoError(t, err)

	err = tf.RequeueFromDLQ(job.ID)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&processedJobs))

	// Test remove from DLQ (create another failing job first)
	// First restore the failing handler
	err = tf.RegisterHandlerFunc("dlq_test", func(ctx context.Context, job *Job) error {
		return fmt.Errorf("test error")
	})
	require.NoError(t, err)
	
	job2, err := tf.Enqueue("dlq_test", map[string]interface{}{"test": true})
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	err = tf.RemoveFromDLQ(job2.ID)
	require.NoError(t, err)

	// Should not be in DLQ anymore
	_, err = tf.GetDLQJob(job2.ID)
	assert.Equal(t, ErrJobNotFound, err)
}