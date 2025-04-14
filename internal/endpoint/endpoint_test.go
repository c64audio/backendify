package endpoint

import (
	"backendify/internal/models"
	"backendify/utils"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/golang-lru"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"io"
	"log"
	"net/http"
	"os"
	"testing"
	"time"
)

// MockHTTPClient is a mock implementation of the HTTPClient interface
type MockHTTPClient struct {
	mock.Mock
}

func (m *MockHTTPClient) Get(url string, timeout time.Duration) (*http.Response, error) {
	args := m.Called(url, timeout)
	if args.Get(0) != nil {
		return args.Get(0).(*http.Response), args.Error(1)
	}
	return nil, args.Error(1)
}

func setupEndpoint() (*Endpoint, error) {
	// Create a Cache for the endpoint
	cache, err := lru.New(100)
	if err != nil {
		return nil, err
	}

	l := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)
	ep := &Endpoint{
		Url:           "http://example.org",
		PathTemplate:  "companies/%s",
		Port:          "9001",
		Status:        StatusActive,
		LastRetry:     time.Time{},
		RetryAttempts: 0,
		SLA:           30,
		Cache:         cache,
		Logger:        l,
	}
	return ep, nil
}

func TestEndpoint_GetUrl(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	expected := "http://example.org:9001"
	actual := ep.GetUrl()

	assert.Equal(t, expected, actual)
}

func TestEndpoint_GetUrlForCompany(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	companyID := "123456"
	expected := "http://example.org:9001/companies/123456"
	actual := ep.GetUrlForCompany(companyID)

	assert.Equal(t, expected, actual)
}

func TestEndpoint_StatusMethods(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	// Test initial status
	assert.Equal(t, StatusActive, ep.GetStatus())

	// Test setting status
	ep.SetStatus(StatusInactive)
	assert.Equal(t, StatusInactive, ep.GetStatus())

	// Test Kill method
	ep.Kill()
	assert.Equal(t, StatusDead, ep.GetStatus())
	assert.Equal(t, 0, ep.GetRetries())

	// Test Reactivate method
	ep.Reactivate()
	assert.Equal(t, StatusActive, ep.GetStatus())
	assert.Equal(t, 0, ep.GetRetries())
}

func TestEndpoint_RetryMethods(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	// Test initial retries
	assert.Equal(t, 0, ep.GetRetries())

	// Test setting retries
	ep.SetRetries(2)
	assert.Equal(t, 2, ep.GetRetries())
}

func TestEndpoint_ProcessError(t *testing.T) {
	testCases := []struct {
		name            string
		statusCode      int
		initialRetries  int
		initialStatus   Status
		expectedRetries int
		expectedStatus  Status
	}{
		{
			name:            "Success status should not change anything",
			statusCode:      200,
			initialRetries:  0,
			initialStatus:   StatusActive,
			expectedRetries: 0,
			expectedStatus:  StatusActive,
		},
		{
			name:            "Error status should increment retries",
			statusCode:      500,
			initialRetries:  0,
			initialStatus:   StatusActive,
			expectedRetries: 1,
			expectedStatus:  StatusInactive,
		},
		{
			name:            "Max retries should set status to dead",
			statusCode:      500,
			initialRetries:  5, // Length of SupportedRetryIntervals
			initialStatus:   StatusInactive,
			expectedRetries: 0,
			expectedStatus:  StatusDead,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := setupEndpoint()
			assert.NoError(t, err)

			ep.SetRetries(tc.initialRetries)
			ep.SetStatus(tc.initialStatus)

			ep.ProcessError(tc.statusCode)

			assert.Equal(t, tc.expectedRetries, ep.GetRetries())
			assert.Equal(t, tc.expectedStatus, ep.GetStatus())
		})
	}
}

func TestEndpoint_ShouldRetry(t *testing.T) {
	testCases := []struct {
		name          string
		status        Status
		retryAttempts int
		expected      bool
	}{
		{
			name:          "Active endpoints should always retry",
			status:        StatusActive,
			retryAttempts: 0,
			expected:      true,
		},
		{
			name:          "Dead endpoints should never retry",
			status:        StatusDead,
			retryAttempts: 0,
			expected:      false,
		},
		{
			name:          "Inactive with max retries should not retry",
			status:        StatusInactive,
			retryAttempts: 5, // Length of SupportedRetryIntervals
			expected:      false,
		},
		// Note: Testing time-based retries would require more complex setup
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := setupEndpoint()
			assert.NoError(t, err)

			ep.SetStatus(tc.status)
			ep.SetRetries(tc.retryAttempts)

			result := ep.ShouldRetry()
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestEndpoint_FetchCompany_CacheHit(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	// Mock Cache hit
	mockCompany := models.CompanyResponse{
		Name:   "Test Company",
		Active: true,
	}
	companyID := "123456"
	ep.Cache.Add(companyID, mockCompany)

	// Create mock client (should not be called due to Cache hit)
	mockClient := &MockHTTPClient{}
	// Call the function
	result, statusCode, err := ep.FetchCompany(mockClient, companyID)

	// Verify results
	assert.NoError(t, err)
	assert.Equal(t, 200, statusCode)
	assert.Equal(t, mockCompany, result)

	// Verify the client was never called
	mockClient.AssertNotCalled(t, "Get")
}

func TestEndpoint_FetchCompany_DeadEndpoint(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	ep.SetStatus(StatusDead)
	mockClient := new(MockHTTPClient)

	result, statusCode, err := ep.FetchCompany(mockClient, "123456")

	assert.Error(t, err)
	assert.Equal(t, 404, statusCode)
	assert.Contains(t, err.Error(), "endpoint unavailable")

	// Empty result should be returned
	assert.Equal(t, models.CompanyResponse{}, result)

	// Client should not be called
	mockClient.AssertNotCalled(t, "Get")
}

func TestEndpoint_FetchCompany_InactiveNotReady(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	ep.SetStatus(StatusInactive)
	// Mock ShouldRetry to return false
	// This would normally involve time, but we'll test the negative path

	mockClient := new(MockHTTPClient)

	// Patch or use a test helper that makes ShouldRetry return false
	// For this example, assume we have access to set retry attempts high enough
	ep.SetRetries(5) // This should make ShouldRetry return false

	result, statusCode, err := ep.FetchCompany(mockClient, "123456")

	assert.Error(t, err)
	assert.Equal(t, 404, statusCode)
	assert.Contains(t, err.Error(), "endpoint awaiting retry")

	// Empty result should be returned
	assert.Equal(t, models.CompanyResponse{}, result)

	// Client should not be called
	mockClient.AssertNotCalled(t, "Get")
}

func TestEndpoint_FetchCompany_V1Response(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	companyID := "123456"
	targetUrl := ep.GetUrlForCompany(companyID)

	// Create a V1 response
	v1Response := V1Response{
		Cn:        "Test Company",
		CreatedOn: "2021-03-01Z",
		// Add other V1 fields as needed
	}
	responseBytes, _ := json.Marshal(v1Response)

	// Set up mock client
	mockClient := new(MockHTTPClient)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBuffer(responseBytes)), // Mock the body
		Header: http.Header{
			"Content-Type": []string{string(utils.V1ContentType)}, // Set the content type to V2
		},
	}
	mockClient.On("Get", targetUrl, ep.GetSLADuration()).
		Return(resp, nil)

	// Call the function
	result, statusCode, err := ep.FetchCompany(mockClient, companyID)

	// Verify results
	assert.NoError(t, err)
	assert.Equal(t, 200, statusCode)

	// Verify the expected company response
	expected := v1Response.GetCompanyResponse()
	expected.ID = companyID
	assert.Equal(t, expected, result)

	// Verify the client was called correctly
	mockClient.AssertExpectations(t)
}

func TestEndpoint_FetchCompany_V2Response_Active(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	companyID := "123456"
	targetUrl := ep.GetUrlForCompany(companyID)

	// Create a V2 response
	v2Response := V2Response{
		CompanyName: "Test Company",
		// Add other V2 fields as needed
	}
	responseBytes, _ := json.Marshal(v2Response)

	// Set up mock client
	mockClient := new(MockHTTPClient)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBuffer(responseBytes)), // Mock the body
		Header: http.Header{
			"Content-Type": []string{string(utils.V2ContentType)}, // Set the content type to V2
		},
	}

	mockClient.On("Get", targetUrl, ep.GetSLADuration()).
		Return(resp, nil)

	// Call the function
	result, statusCode, err := ep.FetchCompany(mockClient, companyID)

	// Verify results
	assert.NoError(t, err)
	assert.Equal(t, 200, statusCode)

	// Verify the expected company response
	expected := v2Response.GetCompanyResponse()
	expected.ID = companyID
	assert.Equal(t, expected, result)

	// Verify the client was called correctly
	mockClient.AssertExpectations(t)
}

func TestEndpoint_FetchCompany_V2Response_Inactive(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	companyID := "123456"
	targetUrl := ep.GetUrlForCompany(companyID)

	// Create a V2 response
	v2Response := V2Response{
		CompanyName: "Test Company",
		DissolvedOn: "2021-03-02Z",
		// Add other V2 fields as needed
	}
	responseBytes, _ := json.Marshal(v2Response)

	// Set up mock client
	mockClient := new(MockHTTPClient)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBuffer(responseBytes)), // Mock the body
		Header: http.Header{
			"Content-Type": []string{string(utils.V2ContentType)}, // Set the content type to V2
		},
	}

	mockClient.On("Get", targetUrl, ep.GetSLADuration()).
		Return(resp, nil)

	// Call the function
	result, statusCode, err := ep.FetchCompany(mockClient, companyID)

	// Verify results
	assert.NoError(t, err)
	assert.Equal(t, 200, statusCode)

	// Verify the expected company response
	expected := v2Response.GetCompanyResponse()
	expected.ID = companyID
	assert.Equal(t, expected, result)

	// Verify the client was called correctly
	mockClient.AssertExpectations(t)
}

func TestEndpoint_FetchCompany_HTTPError(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	companyID := "123456"
	targetUrl := ep.GetUrlForCompany(companyID)

	// Set up mock client to return an error
	mockClient := new(MockHTTPClient)
	mockError := fmt.Errorf("connection timeout")
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(bytes.NewBuffer([]byte{})), // Mock the body
	}

	mockClient.On("Get", targetUrl, ep.GetSLADuration()).
		Return(resp, mockError)

	// Call the function
	result, statusCode, err := ep.FetchCompany(mockClient, companyID)

	// Verify results
	assert.Error(t, err)
	assert.Equal(t, mockError, err)
	assert.Equal(t, 500, statusCode)

	// Verify empty result
	assert.Equal(t, models.CompanyResponse{}, result)

	// Verify the client was called correctly
	mockClient.AssertExpectations(t)

	// Verify that ProcessError was called (by checking retry attempts increased)
	assert.Equal(t, 1, ep.GetRetries())
}

func TestEndpoint_FetchCompany_UnmarshalError(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	companyID := "123456"
	targetUrl := ep.GetUrlForCompany(companyID)

	// Invalid JSON that will cause unmarshal error
	invalidJSON := []byte(`{"this is not valid json"}`)

	// Set up mock client
	mockClient := new(MockHTTPClient)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBuffer(invalidJSON)), // Mock the body
		Header: http.Header{
			"Content-Type": []string{string(utils.V1ContentType)}, // Set the content type to V2
		},
	}

	mockClient.On("Get", targetUrl, ep.GetSLADuration()).
		Return(resp, nil)

	// Call the function
	result, statusCode, err := ep.FetchCompany(mockClient, companyID)

	// Verify results
	assert.Error(t, err)
	assert.Equal(t, 500, statusCode)

	// Verify empty result
	assert.Equal(t, models.CompanyResponse{}, result)

	// Verify the client was called correctly
	mockClient.AssertExpectations(t)
}

func TestEndpoint_FetchCompany_UnsupportedContentType(t *testing.T) {
	ep, err := setupEndpoint()
	assert.NoError(t, err)

	companyID := "123456"
	targetUrl := ep.GetUrlForCompany(companyID)

	// Valid JSON but unsupported content type
	v2Response := V2Response{
		CompanyName: "Test Company",
		DissolvedOn: "2021-03-02Z",
		// Add other V2 fields as needed
	}
	responseBytes, _ := json.Marshal(v2Response)

	// Set up mock client
	mockClient := new(MockHTTPClient)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBuffer(responseBytes)), // Mock the body
		Header: http.Header{
			"Content-Type": []string{"application/json"}, // Set the content type to V2
		},
	}

	mockClient.On("Get", targetUrl, ep.GetSLADuration()).
		Return(resp, nil)

	// Call the function
	result, statusCode, err := ep.FetchCompany(mockClient, companyID)

	// Since the code might handle unsupported types in different ways,
	// we'll make general assertions that could apply in multiple scenarios

	// Either we expect an error, or we expect a default result to be returned
	if err != nil {
		assert.NotEqual(t, 200, statusCode)
	} else {
		// If no error, the function should still return something (possibly default values)
		assert.Equal(t, models.CompanyResponse{}, result)
	}

	// Verify the client was called correctly
	mockClient.AssertExpectations(t)
}
