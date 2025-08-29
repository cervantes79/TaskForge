package taskforge

import (
	"log"
	"time"
)

// DLQCleanupManager handles automatic cleanup of expired jobs in the dead letter queue
type DLQCleanupManager struct {
	taskForge *TaskForge
	ticker    *time.Ticker
	done      chan bool
}

// NewDLQCleanupManager creates a new cleanup manager for the dead letter queue
func NewDLQCleanupManager(tf *TaskForge) *DLQCleanupManager {
	return &DLQCleanupManager{
		taskForge: tf,
		done:      make(chan bool),
	}
}

// Start begins the automatic cleanup process
func (dlq *DLQCleanupManager) Start() {
	// Run cleanup every hour
	dlq.ticker = time.NewTicker(time.Hour)
	
	go func() {
		for {
			select {
			case <-dlq.ticker.C:
				dlq.cleanup()
			case <-dlq.done:
				dlq.ticker.Stop()
				return
			}
		}
	}()
}

// Stop halts the cleanup process
func (dlq *DLQCleanupManager) Stop() {
	if dlq.ticker != nil {
		dlq.done <- true
	}
}

// cleanup removes expired jobs from the dead letter queue
func (dlq *DLQCleanupManager) cleanup() {
	dlq.taskForge.dlqMutex.Lock()
	defer dlq.taskForge.dlqMutex.Unlock()

	now := time.Now()
	ttl := dlq.taskForge.config.DeadLetterQueueTTL
	removed := 0

	for jobID, job := range dlq.taskForge.dlq {
		var jobTime time.Time
		if job.ProcessedAt != nil {
			jobTime = *job.ProcessedAt
		} else {
			jobTime = job.CreatedAt
		}

		if now.Sub(jobTime) > ttl {
			delete(dlq.taskForge.dlq, jobID)
			removed++
		}
	}

	if removed > 0 {
		dlq.taskForge.metrics.mutex.Lock()
		dlq.taskForge.metrics.JobsInDLQ -= int64(removed)
		if dlq.taskForge.metrics.JobsInDLQ < 0 {
			dlq.taskForge.metrics.JobsInDLQ = 0
		}
		dlq.taskForge.metrics.mutex.Unlock()
		
		log.Printf("DLQ Cleanup: Removed %d expired jobs", removed)
	}
}

// ForceCleanup immediately removes all expired jobs
func (dlq *DLQCleanupManager) ForceCleanup() int {
	dlq.cleanup()
	
	dlq.taskForge.dlqMutex.RLock()
	count := len(dlq.taskForge.dlq)
	dlq.taskForge.dlqMutex.RUnlock()
	
	return count
}

// GetDLQJob retrieves a specific job from the dead letter queue
func (tf *TaskForge) GetDLQJob(jobID string) (*Job, error) {
	tf.dlqMutex.RLock()
	defer tf.dlqMutex.RUnlock()

	job, exists := tf.dlq[jobID]
	if !exists {
		return nil, ErrJobNotFound
	}

	// Return a copy to prevent external modification
	jobCopy := *job
	return &jobCopy, nil
}

// RemoveFromDLQ permanently removes a job from the dead letter queue
func (tf *TaskForge) RemoveFromDLQ(jobID string) error {
	tf.dlqMutex.Lock()
	defer tf.dlqMutex.Unlock()

	_, exists := tf.dlq[jobID]
	if !exists {
		return ErrJobNotFound
	}

	delete(tf.dlq, jobID)
	tf.metrics.mutex.Lock()
	tf.metrics.JobsInDLQ--
	tf.metrics.mutex.Unlock()

	return nil
}

// GetDLQStats returns statistics about the dead letter queue
func (tf *TaskForge) GetDLQStats() map[string]interface{} {
	tf.dlqMutex.RLock()
	defer tf.dlqMutex.RUnlock()

	stats := make(map[string]interface{})
	stats["total_jobs"] = len(tf.dlq)
	
	// Count jobs by type
	jobTypes := make(map[string]int)
	oldestJob := time.Now()
	newestJob := time.Time{}
	
	for _, job := range tf.dlq {
		jobTypes[job.Type]++
		
		var jobTime time.Time
		if job.ProcessedAt != nil {
			jobTime = *job.ProcessedAt
		} else {
			jobTime = job.CreatedAt
		}
		
		if jobTime.Before(oldestJob) {
			oldestJob = jobTime
		}
		if jobTime.After(newestJob) {
			newestJob = jobTime
		}
	}
	
	stats["jobs_by_type"] = jobTypes
	if len(tf.dlq) > 0 {
		stats["oldest_job"] = oldestJob
		stats["newest_job"] = newestJob
	}
	
	return stats
}

// BulkRequeueFromDLQ requeues multiple jobs from the dead letter queue
func (tf *TaskForge) BulkRequeueFromDLQ(jobIDs []string) (int, []string) {
	successCount := 0
	var failedIDs []string

	for _, jobID := range jobIDs {
		err := tf.RequeueFromDLQ(jobID)
		if err != nil {
			failedIDs = append(failedIDs, jobID)
		} else {
			successCount++
		}
	}

	return successCount, failedIDs
}

// RequeueDLQByType requeues all jobs of a specific type from the dead letter queue
func (tf *TaskForge) RequeueDLQByType(jobType string) (int, []string) {
	tf.dlqMutex.RLock()
	var jobsToRequeue []string
	for id, job := range tf.dlq {
		if job.Type == jobType {
			jobsToRequeue = append(jobsToRequeue, id)
		}
	}
	tf.dlqMutex.RUnlock()

	return tf.BulkRequeueFromDLQ(jobsToRequeue)
}