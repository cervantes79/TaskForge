package taskforge

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkTaskForgeEnqueue(b *testing.B) {
	tf := New(nil)
	defer tf.Stop()

	err := tf.RegisterHandlerFunc("bench_job", func(ctx context.Context, job *Job) error {
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}

	err = tf.Start()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := tf.Enqueue("bench_job", map[string]interface{}{"data": "test"})
			if err != nil {
				b.Error(err)
			}
		}
	})
}

func BenchmarkTaskForgeProcessing(b *testing.B) {
	tf := New(nil)
	defer tf.Stop()

	var processed int64

	err := tf.RegisterHandlerFunc("bench_processing", func(ctx context.Context, job *Job) error {
		// Simulate minimal work
		atomic.AddInt64(&processed, 1)
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}

	err = tf.Start()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	// Enqueue jobs
	for i := 0; i < b.N; i++ {
		_, err := tf.Enqueue("bench_processing", map[string]interface{}{"id": i})
		if err != nil {
			b.Error(err)
		}
	}

	// Wait for all jobs to be processed
	for atomic.LoadInt64(&processed) < int64(b.N) {
		runtime.Gosched()
	}
}

func BenchmarkTaskForgeConcurrency(b *testing.B) {
	benchmarks := []struct {
		name    string
		workers int
	}{
		{"1Worker", 1},
		{"2Workers", 2},
		{"4Workers", 4},
		{"8Workers", 8},
		{"16Workers", 16},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			config := DefaultConfig()
			config.WorkerCount = bm.workers

			tf := New(config)
			defer tf.Stop()

			var processed int64

			err := tf.RegisterHandlerFunc("concurrent_bench", func(ctx context.Context, job *Job) error {
				// Simulate some CPU work
				for i := 0; i < 1000; i++ {
					_ = i * i
				}
				atomic.AddInt64(&processed, 1)
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}

			err = tf.Start()
			if err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()

			start := time.Now()
			// Enqueue jobs
			for i := 0; i < b.N; i++ {
				_, err := tf.Enqueue("concurrent_bench", map[string]interface{}{"id": i})
				if err != nil {
					b.Error(err)
				}
			}

			// Wait for completion
			for atomic.LoadInt64(&processed) < int64(b.N) {
				runtime.Gosched()
			}

			b.ReportMetric(float64(time.Since(start).Nanoseconds()/int64(b.N)), "ns/job")
		})
	}
}

func BenchmarkTaskForgeMemoryUsage(b *testing.B) {
	tf := New(nil)
	defer tf.Stop()

	err := tf.RegisterHandlerFunc("memory_bench", func(ctx context.Context, job *Job) error {
		// Simulate memory allocation
		data := make([]byte, 1024)
		_ = data
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}

	err = tf.Start()
	if err != nil {
		b.Fatal(err)
	}

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := tf.Enqueue("memory_bench", map[string]interface{}{
			"data":  fmt.Sprintf("test data %d", i),
			"index": i,
		})
		if err != nil {
			b.Error(err)
		}
	}

	// Wait a bit for processing
	time.Sleep(100 * time.Millisecond)

	runtime.GC()
	runtime.ReadMemStats(&m2)

	b.ReportMetric(float64(m2.Alloc-m1.Alloc)/float64(b.N), "bytes/job")
}

func BenchmarkTaskForgeRetryMechanism(b *testing.B) {
	config := DefaultConfig()
	config.RetryDelay = 1 * time.Millisecond
	config.MaxRetries = 2

	tf := New(config)
	defer tf.Stop()

	var attempts int64

	err := tf.RegisterHandlerFunc("retry_bench", func(ctx context.Context, job *Job) error {
		count := atomic.AddInt64(&attempts, 1)
		if count%3 != 0 { // Fail 2 out of 3 times
			return fmt.Errorf("retry test failure")
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}

	err = tf.Start()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := tf.Enqueue("retry_bench", map[string]interface{}{"id": i})
		if err != nil {
			b.Error(err)
		}
	}

	// Wait for processing (including retries)
	time.Sleep(time.Duration(b.N) * time.Millisecond)
}

func BenchmarkTaskForgeQueueOperations(b *testing.B) {
	tf := New(nil)
	defer tf.Stop()

	err := tf.RegisterHandlerFunc("queue_ops", func(ctx context.Context, job *Job) error {
		time.Sleep(10 * time.Millisecond) // Simulate work
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}

	err = tf.Start()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	b.Run("Enqueue", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := tf.Enqueue("queue_ops", map[string]interface{}{"id": i})
			if err != nil {
				b.Error(err)
			}
		}
	})

	b.Run("GetQueueLength", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = tf.GetQueueLength()
		}
	})

	b.Run("GetMetrics", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = tf.metrics.GetMetrics()
		}
	})
}

// Comparative benchmark to show performance vs. a simple channel-based approach
func BenchmarkSimpleChannelProcessing(b *testing.B) {
	jobs := make(chan map[string]interface{}, 1000)
	done := make(chan bool)

	// Simple worker
	go func() {
		for job := range jobs {
			_ = job // Simulate processing
		}
		done <- true
	}()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		jobs <- map[string]interface{}{"id": i}
	}

	close(jobs)
	<-done
}