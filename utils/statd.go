package utils

import (
	"log"
	"os"
	"strings"
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

	// Control channel for graceful shutdown
	done chan struct{}
}

// NewStatsClient creates a new StatsD client configured from environment variables
func NewStatsClient(l *log.Logger) (*StatsClient, error) {
	// Get StatsD server address from environment
	log.Println("Creating StatsD client...")
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
	log.Println("Starting periodic sending goroutine...")
	go statsClient.periodicSend()

	return statsClient, nil
}

// periodicSend sends the metrics to StatsD every second
func (s *StatsClient) periodicSend() {
	s.logger.Println("Starting periodic sending goroutine...")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.logger.Println("sending metrics...")
			// Get current metric values atomically
			m1 := atomic.LoadInt64(&s.metric1)
			m2 := atomic.LoadInt64(&s.metric2)
			m3 := atomic.LoadInt64(&s.metric3)
			m4 := atomic.LoadInt64(&s.metric4)

			// Send to StatsD
			s.client.Gauge("metric.1", m1)
			s.client.Gauge("metric.2", m2)
			s.client.Gauge("metric.3", m3)
			s.client.Gauge("metric.4", m4)
		case <-s.done:
			// Final flush of metrics before shutting down
			m1 := atomic.LoadInt64(&s.metric1)
			m2 := atomic.LoadInt64(&s.metric2)
			m3 := atomic.LoadInt64(&s.metric3)
			m4 := atomic.LoadInt64(&s.metric4)

			s.client.Gauge("metric.1", m1)
			s.client.Gauge("metric.2", m2)
			s.client.Gauge("metric.3", m3)
			s.client.Gauge("metric.4", m4)
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

// SetMetric3 sets the value for metric3
func (s *StatsClient) SetMetric4(value int64) {
	atomic.StoreInt64(&s.metric4, value)
}

// IncrementMetric1 increments metric1 by 1
func (s *StatsClient) IncrementMetric1() {
	atomic.AddInt64(&s.metric1, 1)
}

// IncrementMetric2 increments metric2 by 1
func (s *StatsClient) IncrementMetric2() {
	atomic.AddInt64(&s.metric2, 1)
}

// IncrementMetric3 increments metric3 by 1
func (s *StatsClient) IncrementMetric3() {
	atomic.AddInt64(&s.metric3, 1)
}

// IncrementMetric4 increments metric3 by 1
func (s *StatsClient) IncrementMetric4() {
	atomic.AddInt64(&s.metric4, 1)
}

// Shutdown stops the periodic sending goroutine and flushes metrics
func (s *StatsClient) Shutdown() {
	close(s.done)
}
