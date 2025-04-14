package utils

import (
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	statsd "github.com/smira/go-statsd"
)

// StatsClient provides a thread-safe client for sending metrics to StatsD
type StatsClient struct {
	client *statsd.Client
	logger *log.Logger
	// Atomic metrics storage
	metric1 int64
	metric2 int64
	metric3 int64
	metric4 int64

	// Previous values for calculating deltas
	prevMetric1 int64
	prevMetric2 int64
	prevMetric3 int64
	prevMetric4 int64

	// Control channel for graceful shutdown
	done chan struct{}
	wg   sync.WaitGroup
}

// NewStatsClient creates a new StatsD client configured from environment variables
func NewStatsClient(l *log.Logger) (*StatsClient, error) {
	// Get StatsD server address from environment
	serverAddr := os.Getenv("STATSD_SERVER")
	if serverAddr == "" {
		serverAddr = "localhost:8125" // Default fallback
	}

	// Split host:port if needed
	host := serverAddr
	port := "8125"
	if strings.Contains(serverAddr, ":") {
		parts := strings.Split(serverAddr, ":")
		host, port = parts[0], parts[1]
	}

	// Log the configuration
	l.Printf("Initializing StatsD client to %s:%s", host, port)

	// Create the statsd client
	client := statsd.NewClient(host+":"+port,
		statsd.MaxPacketSize(1400),
		statsd.MetricPrefix(""),
	)

	statsClient := &StatsClient{
		client: client,
		logger: l,
		done:   make(chan struct{}),
	}

	// Start background goroutine for periodic sending
	statsClient.wg.Add(1)
	go func() {
		defer statsClient.wg.Done()
		statsClient.periodicSend()
	}()

	return statsClient, nil
}

// periodicSend sends the metrics to StatsD every second using counters
func (s *StatsClient) periodicSend() {
	s.logger.Println("Starting periodic sending goroutine (counter mode)...")

	// Recover from panics
	defer func() {
		if r := recover(); r != nil {
			s.logger.Printf("PANIC in periodicSend: %v", r)
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Initial log to indicate the goroutine has started
	s.logger.Println("Periodic send loop is running")

	for {
		select {
		case <-ticker.C:
			// Get current metric values atomically
			currentM1 := atomic.LoadInt64(&s.metric1)
			currentM2 := atomic.LoadInt64(&s.metric2)
			currentM3 := atomic.LoadInt64(&s.metric3)
			currentM4 := atomic.LoadInt64(&s.metric4)

			// Calculate deltas (changes since last send)
			deltaM1 := currentM1 - s.prevMetric1
			deltaM2 := currentM2 - s.prevMetric2
			deltaM3 := currentM3 - s.prevMetric3
			deltaM4 := currentM4 - s.prevMetric4

			// Update previous values for next calculation
			s.prevMetric1 = currentM1
			s.prevMetric2 = currentM2
			s.prevMetric3 = currentM3
			s.prevMetric4 = currentM4

			// Debug log before sending
			s.logger.Printf("Current values: m1=%d, m2=%d, m3=%d, m4=%d",
				currentM1, currentM2, currentM3, currentM4)

			// Only send non-zero deltas
			func() {
				defer func() {
					if r := recover(); r != nil {
						s.logger.Printf("PANIC in Counter calls: %v", r)
					}
				}()

				// Use IncrementBy for counter metrics instead of Gauge
				if deltaM1 != 0 {
					s.client.Incr("metric.1", deltaM1)
				}
				if deltaM2 != 0 {
					s.client.Incr("metric.2", deltaM2)
				}
				if deltaM3 != 0 {
					s.client.Incr("metric.3", deltaM3)
				}
				if deltaM4 != 0 {
					s.client.Incr("metric.4", deltaM4)
				}

				s.logger.Println("Counter deltas sent successfully")
			}()

		case <-s.done:
			s.logger.Println("Shutdown signal received, sending final metrics...")

			// Final send of deltas before shutting down
			currentM1 := atomic.LoadInt64(&s.metric1)
			currentM2 := atomic.LoadInt64(&s.metric2)
			currentM3 := atomic.LoadInt64(&s.metric3)
			currentM4 := atomic.LoadInt64(&s.metric4)

			// Calculate final deltas
			deltaM1 := currentM1 - s.prevMetric1
			deltaM2 := currentM2 - s.prevMetric2
			deltaM3 := currentM3 - s.prevMetric3
			deltaM4 := currentM4 - s.prevMetric4

			// Send final deltas
			if deltaM1 != 0 {
				s.client.Incr("metric.1", deltaM1)
			}
			if deltaM2 != 0 {
				s.client.Incr("metric.2", deltaM2)
			}
			if deltaM3 != 0 {
				s.client.Incr("metric.3", deltaM3)
			}
			if deltaM4 != 0 {
				s.client.Incr("metric.4", deltaM4)
			}

			s.logger.Println("Final counter deltas sent, goroutine shutting down")
			return
		}
	}
}

// SetMetric1 sets the value for metric1
func (s *StatsClient) SetMetric1(value int64) {
	atomic.StoreInt64(&s.metric1, value)
}

// SetMetric2 sets the value for metric2
func (s *StatsClient) SetMetric2(value int64) {
	atomic.StoreInt64(&s.metric2, value)
}

// SetMetric3 sets the value for metric3
func (s *StatsClient) SetMetric3(value int64) {
	atomic.StoreInt64(&s.metric3, value)
}

// SetMetric4 sets the value for metric4
func (s *StatsClient) SetMetric4(value int64) {
	atomic.StoreInt64(&s.metric4, value)
}

// IncrementMetric1 increments metric1 by 1
func (s *StatsClient) IncrementMetric1() {
	s.logger.Println("Incrementing metric1 by 1")
	atomic.AddInt64(&s.metric1, 1)
}

// IncrementMetric2 increments metric2 by 1
func (s *StatsClient) IncrementMetric2() {
	s.logger.Println("Incrementing metric2 by 1")
	atomic.AddInt64(&s.metric2, 1)
}

// IncrementMetric3 increments metric3 by 1
func (s *StatsClient) IncrementMetric3() {
	atomic.AddInt64(&s.metric3, 1)
}

// IncrementMetric4 increments metric4 by 1
func (s *StatsClient) IncrementMetric4() {
	atomic.AddInt64(&s.metric4, 1)
}

// Shutdown stops the periodic sending goroutine and flushes metrics
func (s *StatsClient) Shutdown() {
	s.logger.Println("Shutting down StatsClient...")
	close(s.done)

	// Wait for the goroutine to finish with a timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Println("StatsClient shutdown complete")
	case <-time.After(3 * time.Second):
		s.logger.Println("StatsClient shutdown timed out after 3 seconds")
	}
}
