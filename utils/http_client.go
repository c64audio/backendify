package utils

import (
	"io"
	"log"
	"math/rand"
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
			MaxIdleConnsPerHost: 200,
			MaxConnsPerHost:     300,
			IdleConnTimeout:     30 * time.Second,
			DisableKeepAlives:   false,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,  // Connect timeout: short, because we know that the endpoints are really near
				KeepAlive: 60 * time.Second, // TCP keepalive interval
			}).DialContext,
			ResponseHeaderTimeout: 60 * time.Second, // This will never be called, as the jittered client timeout will strike first.
		},
	}
}

// Get performs an HTTP GET request with the specified timeout
// Note that all of these timeouts are below the SLA
func (r *RealHTTPClient) Get(url string, timeout time.Duration) (*http.Response, error) {
	// Create a client with the shared transport but a per-request timeout
	// This avoids race conditions when multiple goroutines use the same client
	// Jitter of 20% is added to avoid request floods when everything times out
	jitter := time.Duration(float64(timeout) * (0.8 + 0.4*rand.Float64()))
	client := &http.Client{
		Transport: r.transport,
		Timeout:   jitter,
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
