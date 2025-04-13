package utils

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const URL string = "http://example.org:9001"

// MockHTTPClient is a mock implementation of HTTPClient
type MockHTTPClient struct {
	mock.Mock
}

func (m *MockHTTPClient) Get(url string, timeout time.Duration) (*http.Response, error) {
	args := m.Called(url)
	if args.Get(0) != nil {
		return args.Get(0).(*http.Response), args.Error(1)
	}
	return nil, args.Error(1)
}

func TestPingHTTP_Success(t *testing.T) {
	v1Response := `{"cn": "","created_on": "","closed_on": ""}`
	mockClient := &MockHTTPClient{}
	mockClient.On("Get", URL).Return(
		&http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(v1Response)))},
		nil,
	)

	status, err := PingHTTP(mockClient, URL, 5*time.Second)
	assert.NoError(t, err)
	assert.Equal(t, 200, status)
	mockClient.AssertExpectations(t)
}

func TestPingHTTP_Error(t *testing.T) {
	mockClient := &MockHTTPClient{}
	mockClient.On("Get", URL).Return(nil, assert.AnError)

	status, err := PingHTTP(mockClient, URL, 5*time.Second)
	assert.Error(t, err)
	assert.Equal(t, 503, status)
}

func TestGetHTTP_Success(t *testing.T) {
	// Arrange: Mocked successful response
	v1Response := `{"cn": "Test Company","created_on": "2021-03-21Z","closed_on": ""}`

	mockClient := &MockHTTPClient{}
	mockResponse := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(v1Response)),
		Header:     make(http.Header),
	}
	mockResponse.Header.Add("Content-type", string(V1ContentType))
	mockClient.On("Get", URL).Return(mockResponse, nil)

	// Act: Call GetHTTP
	statusCode, contentType, body, err := GetHTTP(mockClient, URL, 5*time.Second)

	// Assert: Validate response
	assert.NoError(t, err)
	assert.Equal(t, 200, statusCode)
	assert.Equal(t, v1Response, string(body))
	assert.Equal(t, V1ContentType, contentType)

	// Verify that the mock was called as expected
	mockClient.AssertExpectations(t)
}

func TestGetHTTP_404NotFound(t *testing.T) {
	// Arrange: Mocked 404 response - it's unlikely we'll get one, but it's not impossible
	mockClient := &MockHTTPClient{}
	mockResponse := &http.Response{
		StatusCode: 404,
		Body:       io.NopCloser(bytes.NewBufferString("Not Found")),
	}
	mockClient.On("Get", URL).Return(mockResponse, nil)

	// Act: Call GetHTTP
	statusCode, _, body, err := GetHTTP(mockClient, URL, 5*time.Second)

	// Assert: Validate response
	assert.NoError(t, err)
	assert.Equal(t, 404, statusCode)
	assert.Equal(t, "Not Found", string(body))

	// Verify that the mock was called as expected
	mockClient.AssertExpectations(t)
}

func TestGetHTTP_Timeout(t *testing.T) {
	// Arrange: Simulate timeout error
	mockClient := &MockHTTPClient{}
	mockClient.On("Get", URL).Return(nil, errors.New("timeout"))

	// Act: Call GetHTTP
	statusCode, _, body, err := GetHTTP(mockClient, URL, 5*time.Second)

	// Assert: Validate error scenario
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	assert.Empty(t, body)
	assert.Equal(t, 503, statusCode)

	// Verify that the mock was called as expected
	mockClient.AssertExpectations(t)
}
