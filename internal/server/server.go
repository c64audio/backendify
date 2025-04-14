package server

import (
	"backendify/internal/config"
	"backendify/internal/endpoint"
	"backendify/internal/models"
	"backendify/utils"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Job struct {
	CompanyID   string
	CountryCode string
	ResponseCh  chan models.Result
	Ctx         context.Context
}

// Server We will be using this for the mocks as well
type Server struct {
	Client       utils.HTTPClient
	SLA          float64 // SLA in seconds
	Port         int
	MaxWorkers   int
	logger       *log.Logger
	jobQueue     chan Job
	Endpoints    map[string]endpoint.IEndpoint
	QueueTimeout int

	mu      sync.RWMutex
	stopped bool         // Flag to track if Stop has been called
	started bool         // Flag to track if Start has been called
	server  *http.Server // Store the HTTP server instance
}

type ServerHealth struct {
	LiveEndpoints     string `json:"live_endpoints"`
	InactiveEndpoints string `json:"inactive_endpoints"`
	DeadEndpoints     string `json:"dead_endpoints"`
	QueueLength       int    `json:"queue_length"`
}

func (s *Server) GetHealth() ServerHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	var liveEndpoints []string
	var inactiveEndpoints []string
	var deadEndpoints []string

	for cc, ep := range s.Endpoints {
		switch ep.GetStatus() {
		case endpoint.StatusActive:
			liveEndpoints = append(liveEndpoints, cc)
		case endpoint.StatusInactive:
			inactiveEndpoints = append(inactiveEndpoints, cc)
		case endpoint.StatusDead:
			deadEndpoints = append(deadEndpoints, cc)
		}
	}
	return ServerHealth{
		LiveEndpoints:     strings.Join(liveEndpoints, ","),
		InactiveEndpoints: strings.Join(inactiveEndpoints, ","),
		DeadEndpoints:     strings.Join(deadEndpoints, ","),
		QueueLength:       len(s.jobQueue),
	}
}

func (s *Server) IsHealthy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.started && !s.stopped {
		return true
	}
	return false

	// Not sure if this logic is pissing off production
	/*if len(s.Endpoints) == 0 {
		return false
	}
	if len(s.jobQueue) >= cap(s.jobQueue) {
		return false
	}
	return true*/
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return errors.New("server already started")
	}

	// Log endpoint configuration
	s.logEndpointConfiguration()

	// Initialize job queue
	if s.jobQueue == nil {
		s.jobQueue = make(chan Job, s.MaxWorkers*10)
	}

	// Start worker pool
	s.startWorkerPool()

	// Prepare server
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.recoveryMiddleware(s.handler))

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.Port),
		Handler: mux,
	}

	serverReady := make(chan error, 1)
	go func() {
		listener, err := net.Listen("tcp", s.server.Addr)
		if err != nil {
			serverReady <- fmt.Errorf("failed to create listener: %v", err)
			return
		}
		defer listener.Close()

		s.logger.Printf("INFO: Listening on %s", s.server.Addr)
		serverReady <- nil

		// Block and serve
		if serveErr := s.server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			s.logger.Printf("ERROR: Server failed: %v", serveErr)
		}
	}()

	// Wait for startup or failure
	select {
	case err := <-serverReady:
		if err != nil {
			s.logger.Printf("ERROR: Server startup failed: %v", err)
			return err
		}
		s.started = true
		s.stopped = false
		s.logger.Printf("INFO: Server successfully started on port %d", s.Port)
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("server startup timed out")
	}
}

func (s *Server) logEndpointConfiguration() {
	s.logger.Println("INFO: Configured endpoints:")
	if len(s.Endpoints) == 0 {
		s.logger.Println("WARNING: No endpoints configured!")
	} else {
		for country := range s.Endpoints {
			s.logger.Printf("INFO: Endpoint for country %s is configured", country)
		}
	}
}

func (s *Server) recoveryMiddleware(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				stackTrace := string(buf[:n])

				s.logger.Printf(
					"PANIC in HTTP handler: %v\n"+
						"Request: %s %s\n"+
						"Stack Trace:\n%s",
					rec, r.Method, r.URL.Path, stackTrace,
				)

				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		handler(w, r)
	}
}

// Stop Stops the server in a thread-safe way - kind of!
func (s *Server) Stop() error {
	// Use a write lock with a context for more controlled shutdown
	s.logger.Println("INFO: Stopping server")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stopCh := make(chan struct{})
	go func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		defer close(stopCh)

		if s.stopped {
			return
		}

		// Drain job queue with timeout
		s.drainJobQueue(ctx)

		// Graceful server shutdown
		if s.server != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 5*time.Second)
			defer shutdownCancel()

			if err := s.server.Shutdown(shutdownCtx); err != nil {
				s.logger.Printf("Server shutdown error: %v", err)
			}
		}

		s.logger.Println("INFO: Stopping worker pool")
		// Close endpoints
		for _, ep := range s.Endpoints {
			ep.Close()
		}

		s.stopped = true
		s.started = false
	}()

	// Wait for stop to complete or timeout
	select {
	case <-stopCh:
		s.logger.Println("INFO: Server stopped")
		return nil
	case <-ctx.Done():
		return errors.New("server stop timed out")
	}
}

func (s *Server) drainJobQueue(ctx context.Context) {
	s.logger.Println("INFO: Draining job queue")

	// Count drained jobs for logging
	drained := 0

	for {
		select {
		case job, ok := <-s.jobQueue:
			if !ok {
				// Queue was closed
				s.logger.Printf("INFO: Job queue drained successfully, processed %d jobs", drained)
				return
			}

			// Send response with non-blocking operation
			select {
			case job.ResponseCh <- models.Result{StatusCode: http.StatusServiceUnavailable}:
				// Successfully sent
			default:
				// Channel was full or closed - log but don't block
				s.logger.Printf("WARNING: Could not send result for job - channel full or closed")
			}

			drained++

		case <-ctx.Done():
			s.logger.Printf("INFO: Job queue drain timed out after processing %d jobs", drained)
			return
		}
	}
}

func New(c config.ServerConfiguration, l *log.Logger, client utils.HTTPClient) *Server {
	port := c.Port
	sla := c.SLA
	queueTimeout := c.QueueTimeout
	maxWorkers := c.MaxWorkers
	srv := &Server{Port: port, SLA: sla, MaxWorkers: maxWorkers, logger: l, QueueTimeout: queueTimeout, Client: client, stopped: true, started: false}
	srv.logger.Printf("INFO: Server created: port %d, SLA %f seconds, max workers %d", port, sla, maxWorkers)
	return srv
}

func (s *Server) FetchCompany(id string, countryCode string) (models.CompanyResponse, int) {
	// right: next job - get the Endpoints done in another part of the code, and then get a company from it, then process result
	lowerCountryCode := strings.ToLower(countryCode)

	s.mu.RLock()
	defer s.mu.RUnlock()
	ep, ok := s.Endpoints[lowerCountryCode]

	if !ok {
		s.logger.Printf("ERROR: No endpoint configured for country code: %s", countryCode)
		return models.CompanyResponse{}, http.StatusNotFound
	}

	resp, status, err := ep.FetchCompany(s.Client, id)
	resp.ID = id
	if err != nil {
		s.logger.Printf("ERROR: Endpoint returned error for %s: %v", countryCode, err)
		return models.CompanyResponse{}, http.StatusNotFound
	}
	if status != http.StatusOK {
		s.logger.Printf("ERROR: Endpoint returned status %d for %s", status, countryCode)
		return models.CompanyResponse{}, http.StatusNotFound
	}
	return resp, status
}

func (s *Server) worker(jobs <-chan Job) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("RECOVERED from panic in worker: %v\n", r)
		}
	}()

	for job := range jobs {
		// Check for cancellation before starting work
		if job.Ctx == nil {
			s.logger.Printf("WARN: Context is nil for job %s", job.CompanyID)
			continue
		}
		select {
		case <-job.Ctx.Done():
			s.logger.Printf("INFO: Skipping cancelled job for CompanyID: %s", job.CompanyID)
			continue
		default:
			// Job not cancelled, proceed
		}

		start := time.Now()
		s.logger.Printf("INFO: Worker processing job for CompanyID: %s, CountryCode: %s", job.CompanyID, job.CountryCode)
		result, code := s.FetchCompany(job.CompanyID, job.CountryCode)

		duration := time.Since(start)
		s.logger.Printf("INFO: Worker finished job for CompanyID: %s, CountryCode: %s, Status: %d, Duration: %s", job.CompanyID, job.CountryCode, code, duration)

		// If an error occurs, log it here:
		if code != http.StatusOK {
			s.logger.Printf("ERROR: FetchCompany failed for CompanyID: %s, CountryCode: %s, Status: %d", job.CompanyID, job.CountryCode, code)
		}

		// Send result back to the channel with non-blocking operation and check for cancellation
		select {
		case <-job.Ctx.Done():
			s.logger.Printf("INFO: Job was cancelled during processing for CompanyID: %s", job.CompanyID)
		case job.ResponseCh <- models.Result{Data: result, StatusCode: code}:
			// Successfully sent
		default:
			// Channel was full or no one listening
			s.logger.Printf("WARNING: Could not send result for %s - channel full or closed", job.CompanyID)
		}

	}
}

func (s *Server) startWorkerPool() {
	var wg sync.WaitGroup
	for w := 1; w <= s.MaxWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			s.logger.Printf("INFO: Worker %d initialized", id)
			wg.Done()            // Signal that worker has been initialized
			s.worker(s.jobQueue) // This will run indefinitely
		}(w)
	}
	s.logger.Printf("INFO: Waiting for %d workers to initialize", s.MaxWorkers)
	wg.Wait() // Wait for worker initialization only
	s.logger.Printf("INFO: All %d workers successfully initialized", s.MaxWorkers)
}

func (s *Server) HealthHandler(w http.ResponseWriter) {
	if !s.started {
		s.logger.Println("WARNING: HealthCheck: Server is not started")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	sh := s.GetHealth()
	if s.IsHealthy() {
		s.logger.Println("INFO: HealthCheck: Server is healthy")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		hsb, _ := json.Marshal(sh)
		_, _ = w.Write(hsb)
		s.logger.Printf("INFO: Health info: %v", string(hsb))
	} else {
		s.logger.Println("WARNING: HealthCheck: Server is overloaded or unhealthy")
		w.WriteHeader(http.StatusTooManyRequests)
		// OK, if the server isn't ready yet then _any_ request is too many :)
	}
}

func (s *Server) GetCachedResponse(id string, countryCode string) (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ep, ok := s.Endpoints[strings.ToLower(countryCode)]
	if !ok {
		return false, ""
	}
	result, ok := ep.GetCacheEntryAsJson(id)
	if !ok {
		return false, ""
	}
	return true, result
}

func (s *Server) IsStopped() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stopped
}

func (s *Server) FetchCompanyHandler(w http.ResponseWriter, r *http.Request) {
	if s.IsStopped() {
		return
	}
	handlerStart := time.Now()

	id := r.URL.Query().Get("id")
	countryCode := r.URL.Query().Get("country_iso")
	s.logger.Printf("INFO: Received /company request with id: %s, country_iso: %s", id, countryCode)

	if id == "" || countryCode == "" {
		s.logger.Printf("WARNING: Missing parameters in /company request. id: %s, country_iso: %s", id, countryCode)
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// performance improvement: if we can emergency serve cache hits from here, we greatly reduce the load
	ok, cachedResponse := s.GetCachedResponse(id, countryCode)
	if ok {
		s.logger.Printf("INFO: Serving super-cached response for id: %s, country_iso: %s", id, countryCode)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cachedResponse))
		return
	}

	responseCh := make(chan models.Result, 1) // buffered channel to ensure that incomplete jobs don't block
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.SLA*float64(time.Second)))
	defer cancel()

	// Create a separate context for queue timeout
	queueCtx, queueCancel := context.WithTimeout(context.Background(), time.Duration(s.QueueTimeout)*time.Second)
	defer queueCancel()

	job := Job{
		CompanyID:   id,
		CountryCode: countryCode,
		ResponseCh:  responseCh,
		Ctx:         ctx, // This context controls job execution timeout
	}

	// Use queue context for queue timeout
	select {
	case s.jobQueue <- job:
		// Job successfully queued
		queueTime := time.Since(handlerStart)
		s.logger.Printf("TIMING: Time to queue job %s: %v", id, queueTime)
		s.logger.Printf("INFO: Job successfully queued for id: %s, country_iso: %s", id, countryCode)
	case <-queueCtx.Done():
		// Job queue full or queue timeout
		s.logger.Printf("ERROR: Job queue full or timeout. Dropping request for id: %s, country_iso: %s", id, countryCode)
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// Use the main context for overall SLA timeout
	select {
	case res := <-responseCh:
		processTime := time.Since(handlerStart)
		s.logger.Printf("TIMING: Total processing time for id:%s: %v", id, processTime)

		cancel()
		if res.StatusCode != 200 {
			s.logger.Printf("WARNING: Backend returned non-OK status for id: %s, country_iso: %s, StatusCode: %d", id, countryCode, res.StatusCode)
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		s.logger.Printf("INFO: Successfully fetched company for id: %s, country_iso: %s", id, countryCode)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(res.Data)
	case <-ctx.Done():
		// Add more detailed timeout information
		timeout := time.Duration(s.SLA * float64(time.Second))
		s.logger.Printf("ERROR: Request timed out after %.2f seconds for id: %s, country_iso: %s", timeout.Seconds(), id, countryCode)

		// Log the reason for the timeout
		switch ctx.Err() {
		case context.DeadlineExceeded:
			s.logger.Printf("DETAIL: Timeout for id: %s was due to deadline exceeded (SLA: %.2f seconds)", id, timeout.Seconds())
		case context.Canceled:
			s.logger.Printf("DETAIL: Timeout was due to id: %s being canceled", id)
		default:
			s.logger.Printf("DETAIL: Timeout on id: %s occurred for unknown reason: %v", id, ctx.Err())
		}

		// Check if the job is still in queue
		select {
		case s.jobQueue <- Job{}:
			// If we can immediately put something in the queue and take it out,
			// that suggests the queue isn't full
			<-s.jobQueue
			s.logger.Printf("DETAIL: Job queue is not full, suggesting worker pool might be overloaded")
		default:
			s.logger.Printf("DETAIL: Job queue appears to be full: %d/%d", len(s.jobQueue), cap(s.jobQueue))
		}

		// Log endpoint information
		_, ok := s.Endpoints[countryCode]
		if !ok {
			s.logger.Printf("DETAIL: No endpoint configured for id: %s for country code: %s", id, countryCode)
		} else {
			// If your endpoint has a method to get its status/health, log it here
			s.logger.Printf("DETAIL: Endpoint for %s is configured", countryCode)
		}

		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

func (s *Server) handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/status":
		// Healthcheck endpoint
		s.HealthHandler(w)
	case "/company":
		// Fetch Company endpoint
		s.FetchCompanyHandler(w, r)
	}
}
