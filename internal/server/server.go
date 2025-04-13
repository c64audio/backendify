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
	"net/http"
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

	mu      sync.Mutex
	stopped bool         // Flag to track if Stop has been called
	started bool         // Flag to track if Start has been called
	server  *http.Server // Store the HTTP server instance
}

func (s *Server) IsHealthy() bool {
	if len(s.Endpoints) == 0 {
		return false
	}
	if len(s.jobQueue) >= cap(s.jobQueue) {
		return false
	}
	return true
}

// Start Starts the server in a thread-safe way
func (s *Server) Start() error {
	// Lock to prevent concurrent access
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already started
	if s.started {
		return errors.New("server already started")
	}

	// Check if already stopped
	if s.stopped {
		return errors.New("cannot start a stopped server")
	}

	s.logger.Println("INFO: Server starting...")

	// Log all configured endpoints
	s.logger.Println("INFO: Configured endpoints:")
	if len(s.Endpoints) == 0 {
		s.logger.Println("WARNING: No endpoints configured!")
	} else {
		for country := range s.Endpoints {
			s.logger.Printf("INFO: Endpoint for country %s is configured", country)
		}
	}

	// Initialize job queue if not already set
	if s.jobQueue == nil {
		s.jobQueue = make(chan Job, s.MaxWorkers*10) // Buffer size can be adjusted
	}

	// Start the worker pool
	s.startWorkerPool()

	// Create and start the HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handler) // This will route all requests to your handler function

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.Port),
		Handler: mux,
	}

	s.logger.Printf("INFO: HTTP server listening on port %d", s.Port)

	// Mark as started
	s.started = true

	// Start server in a goroutine so it doesn't block
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Fatalf("ERROR: Server failed to start: %v", err)
		}
	}()

	return nil
}

// Stop Stops the server in a thread-safe way
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		s.logger.Println("INFO: Server already shut down")
		return nil
	}

	s.logger.Println("INFO: Server shutting down...")

	// Gracefully shutdown the HTTP server if it exists
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.Printf("ERROR: Server shutdown error: %v", err)
			// Continue with cleanup even if shutdown fails
		}
	}

	// Close job queue safely
	if s.jobQueue != nil {
		close(s.jobQueue)
	}

	// Close all endpoints
	for _, epi := range s.Endpoints {
		epi.Close()
	}

	// Mark as stopped
	s.stopped = true
	s.started = false

	s.logger.Println("INFO: All workers stopped and endpoints closed.")
	return nil
}

func New(c config.ServerConfiguration, l *log.Logger, client utils.HTTPClient) *Server {
	port := c.Port
	sla := c.SLA
	queueTimeout := c.QueueTimeout
	maxWorkers := c.MaxWorkers
	srv := &Server{Port: port, SLA: sla, MaxWorkers: maxWorkers, logger: l, QueueTimeout: queueTimeout, Client: client}
	srv.logger.Printf("INFO: Server created: port %d, SLA %f seconds, max workers %d", port, sla, maxWorkers)
	return srv
}

func (s *Server) FetchCompany(id string, countryCode string) (models.CompanyResponse, int) {
	// right: next job - get the Endpoints done in another part of the code, and then get a company from it, then process result
	s.logger.Printf("DEBUG: Endpoints map contents: %v", s.Endpoints)
	lowerCountryCode := strings.ToLower(countryCode)

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

// You should also make startWorkerPool thread-safe if needed
func (s *Server) startWorkerPool() {

	for w := 1; w <= s.MaxWorkers; w++ {
		s.logger.Printf("INFO: Starting worker %d", w)
		go s.worker(s.jobQueue)
	}
}

func (s *Server) HealthHandler(w http.ResponseWriter) {
	if s.IsHealthy() {
		s.logger.Println("INFO: HealthCheck: Server is healthy")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "healthy", // here we might want to list crapped out endpoints
			"version": "1.0.0",   // You might want to use a version variable
		})
	} else {
		s.logger.Println("WARNING: HealthCheck: Server is overloaded or unhealthy")
		w.WriteHeader(http.StatusTooManyRequests)
		// OK, if the server isn't ready yet then _any_ request is too many :)
	}
}

func (s *Server) FetchCompanyHandler(w http.ResponseWriter, r *http.Request) {
	handlerStart := time.Now()

	id := r.URL.Query().Get("id")
	countryCode := r.URL.Query().Get("country_iso")
	s.logger.Printf("INFO: Received /company request with id: %s, country_iso: %s", id, countryCode)

	if id == "" || countryCode == "" {
		s.logger.Printf("WARNING: Missing parameters in /company request. id: %s, country_iso: %s", id, countryCode)
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
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
		s.logger.Printf("TIMING: Time to queue job: %v", queueTime)
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
		s.logger.Printf("TIMING: Total processing time: %v", processTime)

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
			s.logger.Printf("DETAIL: Timeout was due to deadline exceeded (SLA: %.2f seconds)", timeout.Seconds())
		case context.Canceled:
			s.logger.Printf("DETAIL: Timeout was due to request being canceled")
		default:
			s.logger.Printf("DETAIL: Timeout occurred for unknown reason: %v", ctx.Err())
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
			s.logger.Printf("DETAIL: No endpoint configured for country code: %s", countryCode)
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
