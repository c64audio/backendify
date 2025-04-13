package utils

import (
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

const (
	V1ContentType ContentType = "application/x-company-v1"
	V2ContentType ContentType = "application/x-company-v2"
)

type ContentType string

// HTTPClient defines the behavior for HTTP requests
type HTTPClient interface {
	Get(url string, timeout time.Duration) (*http.Response, error)
}

// RealHTTPClient implements HTTPClient using the standard http.Client
type RealHTTPClient struct {
	// Base transport configuration with connection pooling
	transport *http.Transport
}

// NewRealHTTPClient creates a new HTTP client with proper connection pool settings
func NewRealHTTPClient() *RealHTTPClient {
	return &RealHTTPClient{
		transport: &http.Transport{
			MaxIdleConns:        500,
			MaxIdleConnsPerHost: 100,
			MaxConnsPerHost:     200,
			IdleConnTimeout:     90 * time.Second,
			DisableKeepAlives:   false,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,  // Connect timeout
				KeepAlive: 30 * time.Second, // TCP keepalive interval
			}).DialContext,
			ResponseHeaderTimeout: 10 * time.Second, // Added timeout for receiving response headers
		},
	}
}

// Get performs an HTTP GET request with the specified timeout
func (r *RealHTTPClient) Get(url string, timeout time.Duration) (*http.Response, error) {
	// Create a client with the shared transport but a per-request timeout
	// This avoids race conditions when multiple goroutines use the same client
	client := &http.Client{
		Transport: r.transport,
		Timeout:   timeout,
	}

	return client.Get(url)
}

// Close should be called when the client is no longer needed to clean up idle connections
func (r *RealHTTPClient) Close() {
	// Close idle connections when the client is no longer needed
	r.transport.CloseIdleConnections()
}

// PingHTTP sends a GET request to the given URL and returns the status code
func PingHTTP(client HTTPClient, url string, sla time.Duration) (int, error) {
	resp, err := client.Get(url, sla)
	if err != nil {
		// If we have a response despite the error
		if resp != nil {
			defer func() {
				// Discard the body to enable connection reuse
				_, _ = io.Copy(io.Discard, resp.Body)
				if err := resp.Body.Close(); err != nil {
					log.Printf("Error closing response body: %v", err)
				}
			}()
			return resp.StatusCode, err
		}

		// No response available, return a meaningful status code
		// For connection errors, 503 Service Unavailable is appropriate
		return http.StatusServiceUnavailable, err
	}
	defer func() {
		// Discard any unread portion of the body
		_, _ = io.Copy(io.Discard, resp.Body)
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()
	return resp.StatusCode, nil
}

func GetHTTP(client HTTPClient, targetUrl string, timeout time.Duration) (int, ContentType, []byte, error) {
	resp, err := client.Get(targetUrl, timeout)

	// First, check if err is non-nil
	if err != nil {
		// Check if resp exists despite the error
		if resp != nil {
			// We have a response despite an error (unusual but possible)
			defer func() {
				// Discard the body to enable connection reuse
				_, _ = io.Copy(io.Discard, resp.Body)
				if err := resp.Body.Close(); err != nil {
					log.Printf("Error closing response body: %v", err)
				}
			}()
			ct := resp.Header.Get("Content-Type")
			return resp.StatusCode, ContentType(ct), []byte{}, err
		}

		// No response object, so no status code or content type
		return http.StatusServiceUnavailable, "", []byte{}, err
	}

	// We have a valid response with no error
	defer func() {
		if err := resp.Body.Close(); err != nil {
			_, _ = io.Copy(io.Discard, resp.Body) // just in case io.ReadAll failed to get the whole body. Stupid io.ReadAll!
			log.Printf("Error closing response body: %v", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	ct := resp.Header.Get("Content-Type")
	if err != nil {
		return resp.StatusCode, ContentType(ct), []byte{}, err
	}

	return resp.StatusCode, ContentType(ct), body, nil
}
