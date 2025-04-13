package mocks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type BackendV1Response struct {
	CompanyName string     `json:"cn"`
	CreatedOn   time.Time  `json:"created_on"`
	ClosedOn    *time.Time `json:"closed_on,omitempty"`
}

type BackendV2Response struct {
	CompanyName string     `json:"company_name"`
	TIN         string     `json:"tin"`
	DissolvedOn *time.Time `json:"dissolved_on,omitempty"`
}

// MockServer represents a mock backend server
type MockServer struct {
	server  *http.Server
	Version int
	Port    string
	Delay   int
	wg      sync.WaitGroup
	logger  *log.Logger
}

// New creates and starts a new mock backend server
func New(port string) *MockServer {

	// Determine version based on port (odd -> 1, even -> 2)
	portInt, err := strconv.Atoi(port)
	if err != nil {
		log.Fatalf("Invalid port: %s", port)
	}
	version := 2
	if portInt%2 != 0 {
		version = 1
	}

	// Set delay to random between 5 and 1100
	delay := 5 + int(time.Now().UnixNano()%1096)
	l := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)
	mock := &MockServer{
		Version: version,
		Port:    port,
		Delay:   delay,
		logger:  l,
	}

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/companies/", mock.companiesHandler)

	// Create server
	mock.server = &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: mux,
	}

	// Start server in a goroutine
	mock.wg.Add(1)
	go func() {
		defer mock.wg.Done()
		log.Printf("Starting mock backend v%d on port %s...\n", mock.Version, mock.Port)
		if err := mock.server.ListenAndServe(); err != nil {
			if err != http.ErrServerClosed {
				log.Printf("Server error: %v", err)
			}
		}
	}()

	return mock
}

// companiesHandler handles requests to the /companies/ endpoint
// companiesHandler handles requests to the /companies/ endpoint
// companiesHandler handles requests to the /companies/ endpoint
func (m *MockServer) companiesHandler(w http.ResponseWriter, r *http.Request) {
	// Simulate delay if specified
	if m.Delay > 0 {
		time.Sleep(time.Duration(m.Delay) * time.Millisecond)
	}

	// Extract company ID from URL
	paths := strings.Split(r.URL.Path, "/")
	if len(paths) < 3 || paths[1] != "companies" {
		m.logger.Printf("ERROR: Invalid path structure: %v", paths)
		http.NotFound(w, r)
		return
	}
	id := paths[2]

	// For test purposes, return 404 for company ID "notfound"
	if id == "notfound" {
		m.logger.Printf("Returning 404 for 'notfound' ID")
		http.NotFound(w, r)
		return
	}

	m.logger.Printf("INFO: Request received for id %s on endpoint port %s", id, m.Port)

	if m.Version == 1 {
		m.logger.Println("Version 1")

		// Set Content-Type header before writing any response
		w.Header().Set("Content-Type", "application/x-company-v1") // Changed to standard JSON for testing
		// Set status code explicitly to 200 OK
		w.WriteHeader(http.StatusOK)
		// For demo purposes, create a closed company for IDs ending with "closed"
		var closedOn *time.Time
		if strings.HasSuffix(id, "closed") {
			closedTime := time.Now().Add(-24 * time.Hour) // Closed yesterday
			closedOn = &closedTime
		}

		resp := BackendV1Response{
			CompanyName: fmt.Sprintf("V1 Company %s", id),
			CreatedOn:   time.Now().Add(-365 * 24 * time.Hour), // Created a year ago
			ClosedOn:    closedOn,
		}

		// Log the response being sent
		respBytes, _ := json.Marshal(resp)
		m.logger.Printf("Sending response: %s", string(respBytes))

		// Write response directly instead of using Encoder
		respBytes, err := json.Marshal(resp)
		if err != nil {
			m.logger.Printf("ERROR: Failed to marshal response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		_, err = w.Write(respBytes)
		if err != nil {
			m.logger.Printf("ERROR: Failed to write response: %v", err)
			return
		}

	} else {
		m.logger.Println("Version 2")

		// Set Content-Type header before writing any response
		w.Header().Set("Content-Type", "application/x-company-v2") // Changed to standard JSON for testing
		// Set status code explicitly to 200 OK
		w.WriteHeader(http.StatusOK)

		// For demo purposes, create a dissolved company for IDs ending with "dissolved"
		var dissolvedOn *time.Time
		if strings.HasSuffix(id, "dissolved") {
			dissolvedTime := time.Now().Add(-24 * time.Hour) // Dissolved yesterday
			dissolvedOn = &dissolvedTime
		}

		resp := BackendV2Response{
			CompanyName: fmt.Sprintf("V2 Company %s", id),
			TIN:         fmt.Sprintf("TIN-%s", id),
			DissolvedOn: dissolvedOn,
		}

		// Log the response being sent
		respBytes, _ := json.Marshal(resp)
		m.logger.Printf("Sending response: %s", string(respBytes))

		// Write response directly instead of using Encoder
		respBytes, err := json.Marshal(resp)
		if err != nil {
			m.logger.Printf("ERROR: Failed to marshal response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		_, err = w.Write(respBytes)
		if err != nil {
			m.logger.Printf("ERROR: Failed to write response: %v", err)
			return
		}
	}

	m.logger.Printf("Successfully handled request for ID: %s", id)
}

// Shutdown gracefully shuts down the mock server
func (m *MockServer) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := m.server.Shutdown(ctx)
	m.wg.Wait() // Wait for the server goroutine to finish
	return err
}
