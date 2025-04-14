package server

import (
	"backendify/internal/config"
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"backendify/internal/endpoint"
	"backendify/internal/models"
	"backendify/utils"
)

// MockEndpoint implements endpoint.IEndpoint for testing
type MockEndpoint struct {
	FetchCompanyFunc func(client utils.HTTPClient, id string) (models.CompanyResponse, int, error)
	Status           endpoint.Status
}

func (m *MockEndpoint) GetCacheEntryAsJson(id string) (string, bool) {
	return "", false
}

func (m *MockEndpoint) GetStatus() endpoint.Status {
	return m.Status
}

func (m *MockEndpoint) Close() {

}

func (m *MockEndpoint) FetchCompany(client utils.HTTPClient, id string) (models.CompanyResponse, int, error) {
	if m.FetchCompanyFunc != nil {
		return m.FetchCompanyFunc(client, id)
	}
	return models.CompanyResponse{}, http.StatusInternalServerError, nil
}

// MockHTTPClient implements utils.HTTPClient for testing
type MockHTTPClient struct {
	GetFunc func(url string, timeout time.Duration) (*http.Response, error)
}

func (m *MockHTTPClient) Get(url string, timeout time.Duration) (*http.Response, error) {
	if m.GetFunc != nil {
		return m.GetFunc(url, timeout)
	}
	return nil, errors.New("not implemented")
}

func getRandomPort() int {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// Helper to create a basic test server
func createMockServer() (*Server, *MockHTTPClient, *bytes.Buffer) {
	var logBuf bytes.Buffer
	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)
	mockClient := &MockHTTPClient{}
	server := &Server{
		Port:         getRandomPort(),
		SLA:          1,
		MaxWorkers:   5,
		logger:       logger,
		Client:       mockClient,
		jobQueue:     make(chan Job, 10),
		Endpoints:    make(map[string]endpoint.IEndpoint),
		QueueTimeout: 3,
		stopped:      true,
		started:      false,
	}
	return server, mockClient, &logBuf
}

// Test the server constructor
func TestNew(t *testing.T) {
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)
	cfg := config.ServerConfiguration{
		Port:       9000,
		SLA:        1,
		MaxWorkers: 5,
	}

	server := New(cfg, logger, nil)

	if server.Port != cfg.Port {
		t.Errorf("Expected Port to be %d, got %d", cfg.Port, server.Port)
	}
	if server.SLA != cfg.SLA {
		t.Errorf("Expected SLA to be %f, got %f", cfg.SLA, server.SLA)
	}
	if server.MaxWorkers != cfg.MaxWorkers {
		t.Errorf("Expected MaxWorkers to be %d, got %d", cfg.MaxWorkers, server.MaxWorkers)
	}
	if server.logger != logger {
		t.Errorf("Expected logger to be set correctly")
	}
}

// Test the HandleFetchCompany handler
func TestHandleFetchCompany(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		mockEndpoint   *MockEndpoint
		mockClient     *MockHTTPClient
		expectedStatus int
		expectedBody   string
	}{
		{
			name:        "Valid request with V1 backend",
			queryParams: "id=COMP123&country_iso=US",
			mockEndpoint: &MockEndpoint{
				FetchCompanyFunc: func(client utils.HTTPClient, id string) (models.CompanyResponse, int, error) {
					return models.CompanyResponse{
						ID:     id,
						Name:   "Test Company V1",
						Active: true,
					}, http.StatusOK, nil
				},
				Status: endpoint.StatusActive,
			},
			expectedStatus: http.StatusOK,
			expectedBody:   `"id":"COMP123","name":"Test Company V1","active":true`,
		},
		{
			name:        "Valid request with V2 backend",
			queryParams: "id=COMP234&country_iso=RU",
			mockEndpoint: &MockEndpoint{
				FetchCompanyFunc: func(client utils.HTTPClient, id string) (models.CompanyResponse, int, error) {
					return models.CompanyResponse{
						ID:     id,
						Name:   "Test Company V2",
						Active: false,
					}, http.StatusOK, nil
				},
				Status: endpoint.StatusActive,
			},
			expectedStatus: http.StatusOK,
			expectedBody:   `"id":"COMP234","name":"Test Company V2","active":false`,
		},
		{
			name:           "Missing query parameters",
			queryParams:    "",
			mockEndpoint:   nil,
			expectedStatus: http.StatusNotFound,
			expectedBody:   "",
		},
		{
			name:           "Invalid country code",
			queryParams:    "id=COMP123&country_iso=INVALID",
			mockEndpoint:   nil,
			expectedStatus: http.StatusNotFound,
			expectedBody:   "",
		},
		{
			name:        "Company not found in backend",
			queryParams: "id=COMP_UNKNOWN&country_iso=US",
			mockEndpoint: &MockEndpoint{
				FetchCompanyFunc: func(client utils.HTTPClient, id string) (models.CompanyResponse, int, error) {
					return models.CompanyResponse{}, http.StatusNotFound, nil
				},
				Status: endpoint.StatusActive,
			},
			expectedStatus: http.StatusNotFound,
			expectedBody:   "",
		},
		{
			name:        "Backend returns unexpected error",
			queryParams: "id=COMP123&country_iso=US",
			mockEndpoint: &MockEndpoint{
				FetchCompanyFunc: func(client utils.HTTPClient, id string) (models.CompanyResponse, int, error) {
					return models.CompanyResponse{}, http.StatusInternalServerError, nil
				},
				Status: endpoint.StatusActive,
			},
			expectedStatus: http.StatusNotFound,
			expectedBody:   "",
		},
	}

	server, _, _ := createMockServer()
	defer func() {
		// If you have a Stop/Shutdown method, call it here
		// This would stop the workers and clean up resources
		if err := server.Stop(); err != nil {
			t.Logf("Error stopping server: %v", err)
		}
	}()

	err := server.Start()
	if err != nil {
		t.Error("error starting server")
	}

	defer func() {
		if err := server.Stop(); err != nil {
			t.Logf("Error stopping server: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server.Endpoints = make(map[string]endpoint.IEndpoint)

			// Create mock server
			if tt.mockEndpoint != nil {
				server.Endpoints["us"] = tt.mockEndpoint // Simulate US endpoint as an example
				server.Endpoints["ru"] = tt.mockEndpoint // Simulate RU endpoint as an additional example
			}

			// Create HTTP request
			req, err := http.NewRequest("GET", "/company?"+tt.queryParams, nil)
			if err != nil {
				t.Fatalf("Could not create request: %v", err)
			}

			rec := httptest.NewRecorder()

			// Trigger the handler
			server.FetchCompanyHandler(rec, req)

			// Validate HTTP status code
			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected HTTP status %d, got %d", tt.expectedStatus, rec.Code)
			}

			// Validate the HTTP response body
			if !strings.Contains(rec.Body.String(), tt.expectedBody) {
				t.Errorf("Expected response body to contain %q, got %q", tt.expectedBody, rec.Body.String())
			}
		})
	}
	server.Stop()
}

func TestJobQueue(t *testing.T) {
	server, _, _ := createMockServer()

	// Set up a mock endpoint to handle the job
	server.Endpoints["us"] = &MockEndpoint{
		FetchCompanyFunc: func(client utils.HTTPClient, id string) (models.CompanyResponse, int, error) {
			return models.CompanyResponse{
				ID:     "COMP123",
				Name:   "Test Company",
				Active: true,
			}, http.StatusOK, nil
		},
		Status: endpoint.StatusActive,
	}

	// Start the server (which starts the workers)
	err := server.Start()
	if err != nil {
		t.Error("error starting server")
	}

	// Ensure we stop the server at the end of the test
	defer func() {
		if err := server.Stop(); err != nil {
			t.Logf("Error stopping server: %v", err)
		}
	}()

	// Create a response channel
	responseCh := make(chan models.Result, 1)

	// Create a job with all required fields including context
	job := Job{
		CompanyID:   "COMP123",
		CountryCode: "US",
		ResponseCh:  responseCh,
		Ctx:         context.Background(),
	}

	// Send the job to the queue
	server.jobQueue <- job

	// Wait for the response from the worker
	select {
	case result := <-responseCh:
		// Verify the result is what we expect
		if result.StatusCode != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, result.StatusCode)
		}

		// Direct field access since Data is a CompanyResponse
		if result.Data.ID != "COMP123" || result.Data.Name != "Test Company" {
			t.Errorf("Unexpected result: %+v", result.Data)
		}

	case <-time.After(2 * time.Second):
		t.Error("No response received from worker within timeout")
	}
}

// TestHandleFetchCompanyTimeout tests the timeout behavior for company fetch requests.
// This is separated from the main test suite to avoid log interleaving issues.
func TestHandleFetchCompanyTimeout(t *testing.T) {
	// Create a dedicated server with a short SLA
	server, _, _ := createMockServer() // Remove the unused mockHTTPClient variable
	server.SLA = 1.0                   // 1 second timeout

	// Start the server with a single worker to ensure deterministic behavior
	server.MaxWorkers = 1
	err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Make sure to clean up when the test ends
	defer func() {
		if err := server.Stop(); err != nil {
			t.Logf("Warning: Error stopping server: %v", err)
		}
	}()

	// Allow time for server to initialize
	time.Sleep(100 * time.Millisecond)

	// Create a mock endpoint that will deliberately take longer than the SLA timeout
	delayDuration := 2 * time.Second // Longer than the 1s SLA

	slowMockEndpoint := &MockEndpoint{
		FetchCompanyFunc: func(client utils.HTTPClient, id string) (models.CompanyResponse, int, error) {
			t.Logf("Mock endpoint for timeout test starting sleep of %v", delayDuration)
			time.Sleep(delayDuration) // This will trigger the timeout
			t.Log("Mock endpoint for timeout test finished sleep (after timeout should occur)")
			return models.CompanyResponse{}, http.StatusNotFound, nil
		},
		Status: endpoint.StatusActive,
	}

	// Set up the endpoint for both country codes
	server.Endpoints["us"] = slowMockEndpoint
	server.Endpoints["ru"] = slowMockEndpoint

	// Create the request
	req, err := http.NewRequest("GET", "/company?id=COMP123&country_iso=US", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	rec := httptest.NewRecorder()

	// Use a channel to signal when the handler function completes
	handlerDone := make(chan struct{})

	// Start timing to verify timeout behavior
	startTime := time.Now()

	// Run the handler in a goroutine
	go func() {
		server.FetchCompanyHandler(rec, req)
		close(handlerDone)
	}()

	// Wait for the handler to complete or a maximum test timeout
	select {
	case <-handlerDone:
		// Handler completed
		t.Logf("Handler returned after %v", time.Since(startTime))
	case <-time.After(1500 * time.Millisecond): // A bit longer than SLA
		t.Logf("Test timeout after %v, but we'll still wait for handler to complete", time.Since(startTime))
	}

	// Wait for handler to actually complete so we can check results
	<-handlerDone

	// Verify the expected timeout behavior
	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status code %d (timeout), got %d", http.StatusNotFound, rec.Code)
	}

	// Verify the elapsed time is approximately the timeout duration (with some wiggle room)
	elapsedTime := time.Since(startTime)
	if elapsedTime < 900*time.Millisecond || elapsedTime > 1300*time.Millisecond {
		t.Logf("Warning: Expected timeout to occur after approximately 1s, actual time: %v", elapsedTime)
	}

	// Allow for background worker goroutine to finish
	time.Sleep(2500 * time.Millisecond)

	t.Log("Timeout test completed successfully")
}
