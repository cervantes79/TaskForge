# TaskForge

[![Go Report Card](https://goreportcard.com/badge/github.com/cervantes79/TaskForge)](https://goreportcard.com/report/github.com/cervantes79/TaskForge)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A high-performance background job processing system for Go applications. TaskForge provides concurrent processing, automatic retry logic with exponential backoff, and dead letter queue functionality - delivering significantly better performance than Python-based solutions like Celery.

## Features

- **High Performance**: Built with Go's concurrency primitives for maximum throughput
- **Concurrent Processing**: Configurable worker pools for parallel job execution
- **Automatic Retries**: Exponential backoff retry mechanism with configurable limits
- **Dead Letter Queue**: Failed jobs management with requeue capabilities
- **Built-in Metrics**: Real-time monitoring of job processing statistics
- **Docker Ready**: Container support with docker-compose configuration  
- **Production Ready**: Comprehensive testing and benchmarks
- **Graceful Shutdown**: Ensures jobs complete before termination

## Quick Start

### Installation

```bash
go get github.com/cervantes79/TaskForge
```

### Basic Usage

```go
package main

import (
    "context"
    "log"
    "time"
    
    "github.com/cervantes79/TaskForge"
)

func main() {
    // Create TaskForge with default configuration
    tf := taskforge.New(nil)
    defer tf.Stop()

    // Register a job handler
    err := tf.RegisterHandlerFunc("send_email", func(ctx context.Context, job *taskforge.Job) error {
        recipient := job.Payload["recipient"].(string)
        log.Printf("Sending email to: %s", recipient)
        
        // Simulate email sending
        time.Sleep(100 * time.Millisecond)
        
        log.Printf("Email sent to: %s", recipient)
        return nil
    })
    if err != nil {
        log.Fatal(err)
    }

    // Start the job processor
    if err := tf.Start(); err != nil {
        log.Fatal(err)
    }

    // Enqueue a job
    job, err := tf.Enqueue("send_email", map[string]interface{}{
        "recipient": "user@example.com",
        "subject":   "Welcome!",
    })
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Job enqueued: %s", job.ID)
    
    // Wait for job completion
    time.Sleep(1 * time.Second)
}
```

## Configuration

TaskForge can be configured using the `Config` struct:

```go
config := &taskforge.Config{
    WorkerCount:        8,                    // Number of concurrent workers
    QueueSize:          2000,                 // Job queue buffer size
    MaxRetries:         5,                    // Default retry attempts
    RetryDelay:         time.Second * 2,      // Initial retry delay
    MaxRetryDelay:      time.Minute * 5,      // Maximum retry delay
    DeadLetterQueueTTL: time.Hour * 24,       // DLQ retention period
    ShutdownTimeout:    time.Second * 30,     // Graceful shutdown timeout
}

tf := taskforge.New(config)
```

## Advanced Features

### Job Options

Customize individual jobs with `EnqueueOptions`:

```go
job, err := tf.EnqueueWithOptions("priority_job", payload, &taskforge.EnqueueOptions{
    MaxRetries: intPtr(10),                    // Custom retry limit
    Priority:   intPtr(5),                     // Higher priority
    Delay:      durationPtr(time.Minute * 5),  // Delayed execution
})
```

### Dead Letter Queue Management

```go
// Get all DLQ jobs
dlqJobs := tf.GetDeadLetterQueue()

// Requeue a specific job
err := tf.RequeueFromDLQ("job-id-123")

// Get DLQ statistics
stats := tf.GetDLQStats()

// Clear entire DLQ
clearedCount := tf.ClearDeadLetterQueue()
```

### Monitoring and Metrics

```go
metrics := tf.GetMetrics()
fmt.Printf("Processed: %d, Failed: %d, In DLQ: %d\n", 
    metrics.JobsProcessed, metrics.JobsFailed, metrics.JobsInDLQ)

// Real-time status
fmt.Printf("Active Workers: %d, Queue Length: %d\n", 
    tf.GetActiveWorkers(), tf.GetQueueLength())
```

## Use Cases

TaskForge is ideal for:

- Email Processing: Bulk email campaigns, transactional emails
- Report Generation: PDF reports, data exports, analytics
- Data Synchronization: ETL processes, database migrations  
- Image/Media Processing: Thumbnail generation, video encoding
- Webhook Processing: API integrations, third-party notifications
- Scheduled Tasks: Cleanup jobs, maintenance operations

## Performance

TaskForge delivers exceptional performance:

- 10,000+ jobs/second throughput on modern hardware
- Sub-millisecond job enqueue latency  
- Minimal memory footprint with efficient job queuing
- Linear scaling with worker count increase

### Benchmark Results

```
BenchmarkTaskForgeEnqueue-8           500000      3521 ns/op      256 B/op       4 allocs/op
BenchmarkTaskForgeProcessing-8         50000     42847 ns/op      512 B/op       8 allocs/op
BenchmarkTaskForgeConcurrency/4Workers-8  20000     85234 ns/op    1024 B/op      12 allocs/op
```

## Docker Support

### Using Docker Compose

```bash
# Start TaskForge with monitoring stack
docker-compose up -d

# View logs  
docker-compose logs -f taskforge

# Scale workers
docker-compose up -d --scale taskforge=3
```

### Environment Variables

- `TASKFORGE_WORKERS`: Number of worker processes
- `TASKFORGE_QUEUE_SIZE`: Queue buffer size
- `TASKFORGE_MAX_RETRIES`: Default retry limit

## Testing

```bash
# Run tests
go test -v ./...

# Run benchmarks
go test -bench=. -benchmem ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Examples

Check the `examples/` directory for complete implementations:

- Basic Usage: Simple job processing setup
- HTTP API: REST endpoints for job management
- Advanced Configuration: Custom retry policies and monitoring
- Production Setup: Docker deployment with monitoring

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Support

- Documentation: [GoDoc](https://godoc.org/github.com/cervantes79/TaskForge)
- Issues: [GitHub Issues](https://github.com/cervantes79/TaskForge/issues)
- Discussions: [GitHub Discussions](https://github.com/cervantes79/TaskForge/discussions)