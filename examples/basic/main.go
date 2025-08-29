package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/cervantes79/TaskForge"
)

func main() {
	// Create configuration
	config := taskforge.DefaultConfig()
	
	// Override with environment variables if set
	if workers := os.Getenv("TASKFORGE_WORKERS"); workers != "" {
		if w, err := strconv.Atoi(workers); err == nil {
			config.WorkerCount = w
		}
	}
	
	if queueSize := os.Getenv("TASKFORGE_QUEUE_SIZE"); queueSize != "" {
		if qs, err := strconv.Atoi(queueSize); err == nil {
			config.QueueSize = qs
		}
	}

	// Create TaskForge instance
	tf := taskforge.New(config)

	// Register job handlers
	setupJobHandlers(tf)

	// Start TaskForge
	if err := tf.Start(); err != nil {
		log.Fatalf("Failed to start TaskForge: %v", err)
	}

	log.Printf("TaskForge started with %d workers", config.WorkerCount)

	// Start HTTP server for job submission and monitoring
	go startHTTPServer(tf)

	// Simulate some initial jobs
	go simulateJobs(tf)

	// Wait for shutdown signal
	waitForShutdown(tf)
}

func setupJobHandlers(tf *taskforge.TaskForge) {
	// Email sending job
	tf.RegisterHandlerFunc("send_email", func(ctx context.Context, job *taskforge.Job) error {
		recipient := job.Payload["recipient"].(string)
		subject := job.Payload["subject"].(string)
		
		log.Printf("Sending email to %s with subject: %s", recipient, subject)
		
		// Simulate email sending delay
		time.Sleep(time.Duration(rand.Intn(500)+100) * time.Millisecond)
		
		// Randomly fail 10% of the time to demonstrate retry logic
		if rand.Float32() < 0.1 {
			return fmt.Errorf("SMTP server temporarily unavailable")
		}
		
		log.Printf("Email sent successfully to %s", recipient)
		return nil
	})

	// Report generation job
	tf.RegisterHandlerFunc("generate_report", func(ctx context.Context, job *taskforge.Job) error {
		reportType := job.Payload["type"].(string)
		userID := job.Payload["user_id"].(string)
		
		log.Printf("Generating %s report for user %s", reportType, userID)
		
		// Simulate report generation (longer process)
		time.Sleep(time.Duration(rand.Intn(2000)+1000) * time.Millisecond)
		
		log.Printf("Report %s generated for user %s", reportType, userID)
		return nil
	})

	// Data synchronization job
	tf.RegisterHandlerFunc("sync_data", func(ctx context.Context, job *taskforge.Job) error {
		source := job.Payload["source"].(string)
		destination := job.Payload["destination"].(string)
		
		log.Printf("Syncing data from %s to %s", source, destination)
		
		// Simulate data sync
		time.Sleep(time.Duration(rand.Intn(1000)+200) * time.Millisecond)
		
		// Fail occasionally to test retry mechanism
		if rand.Float32() < 0.05 {
			return fmt.Errorf("network timeout during sync")
		}
		
		log.Printf("Data sync completed: %s -> %s", source, destination)
		return nil
	})

	// Image processing job
	tf.RegisterHandlerFunc("process_image", func(ctx context.Context, job *taskforge.Job) error {
		imageURL := job.Payload["image_url"].(string)
		operation := job.Payload["operation"].(string)
		
		log.Printf("Processing image %s with operation: %s", imageURL, operation)
		
		// Simulate image processing
		time.Sleep(time.Duration(rand.Intn(3000)+500) * time.Millisecond)
		
		log.Printf("Image processed: %s", imageURL)
		return nil
	})
}

func startHTTPServer(tf *taskforge.TaskForge) {
	http.HandleFunc("/enqueue", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		jobType := r.FormValue("type")
		if jobType == "" {
			http.Error(w, "Job type required", http.StatusBadRequest)
			return
		}

		var payload map[string]interface{}
		
		switch jobType {
		case "send_email":
			payload = map[string]interface{}{
				"recipient": r.FormValue("recipient"),
				"subject":   r.FormValue("subject"),
			}
		case "generate_report":
			payload = map[string]interface{}{
				"type":    r.FormValue("report_type"),
				"user_id": r.FormValue("user_id"),
			}
		case "sync_data":
			payload = map[string]interface{}{
				"source":      r.FormValue("source"),
				"destination": r.FormValue("destination"),
			}
		case "process_image":
			payload = map[string]interface{}{
				"image_url": r.FormValue("image_url"),
				"operation": r.FormValue("operation"),
			}
		default:
			http.Error(w, "Unknown job type", http.StatusBadRequest)
			return
		}

		job, err := tf.Enqueue(jobType, payload)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, "Job enqueued: %s\n", job.ID)
	})

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		metrics := tf.GetMetrics()
		
		fmt.Fprintf(w, "Jobs Processed: %d\n", metrics.JobsProcessed)
		fmt.Fprintf(w, "Jobs Failed: %d\n", metrics.JobsFailed)
		fmt.Fprintf(w, "Jobs Retried: %d\n", metrics.JobsRetried)
		fmt.Fprintf(w, "Jobs in DLQ: %d\n", metrics.JobsInDLQ)
		fmt.Fprintf(w, "Average Execution Time: %dms\n", metrics.AverageExecTime)
		fmt.Fprintf(w, "Active Workers: %d\n", tf.GetActiveWorkers())
		fmt.Fprintf(w, "Queue Length: %d\n", tf.GetQueueLength())
		fmt.Fprintf(w, "Retry Queue Length: %d\n", tf.GetRetryQueueLength())
	})

	http.HandleFunc("/dlq", func(w http.ResponseWriter, r *http.Request) {
		dlqJobs := tf.GetDeadLetterQueue()
		
		fmt.Fprintf(w, "Dead Letter Queue (%d jobs):\n", len(dlqJobs))
		for id, job := range dlqJobs {
			fmt.Fprintf(w, "- Job ID: %s, Type: %s, Attempts: %d, Error: %s\n", 
				id, job.Type, job.Attempts, job.LastError)
		}
	})

	http.HandleFunc("/dlq/requeue", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		jobID := r.FormValue("job_id")
		if jobID == "" {
			http.Error(w, "Job ID required", http.StatusBadRequest)
			return
		}

		err := tf.RequeueFromDLQ(jobID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, "Job requeued: %s\n", jobID)
	})

	log.Println("HTTP server starting on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Printf("HTTP server error: %v", err)
	}
}

func simulateJobs(tf *taskforge.TaskForge) {
	time.Sleep(2 * time.Second) // Wait for startup

	// Simulate periodic job creation
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Create some sample jobs
			jobs := []struct {
				jobType string
				payload map[string]interface{}
			}{
				{
					"send_email",
					map[string]interface{}{
						"recipient": "user@example.com",
						"subject":   "Weekly Newsletter",
					},
				},
				{
					"generate_report",
					map[string]interface{}{
						"type":    "sales",
						"user_id": fmt.Sprintf("user_%d", rand.Intn(100)),
					},
				},
				{
					"sync_data",
					map[string]interface{}{
						"source":      "database_primary",
						"destination": "data_warehouse",
					},
				},
			}

			for _, job := range jobs {
				_, err := tf.Enqueue(job.jobType, job.payload)
				if err != nil {
					log.Printf("Failed to enqueue job: %v", err)
				}
			}
		}
	}
}

func waitForShutdown(tf *taskforge.TaskForge) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.Println("Shutdown signal received, stopping TaskForge...")

	if err := tf.Stop(); err != nil {
		log.Printf("Error stopping TaskForge: %v", err)
	}

	log.Println("TaskForge stopped. Goodbye!")
}