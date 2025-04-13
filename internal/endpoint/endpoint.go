package endpoint

import (
	"backendify/internal/config"
	"backendify/internal/mocks"
	"backendify/internal/models"
	"backendify/utils"
	"encoding/json"
	"fmt"
	lru "github.com/hashicorp/golang-lru"
	"log"
	"strings"
	"sync"
	"time"

	"net/url"
)

// SupportedRetryIntervals Defines the retry intervals (in seconds) for Endpoints. This slice should be treated as immutable.
var SupportedRetryIntervals = [5]int{10, 30, 90, 180, 720}

type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusInactive Status = "INACTIVE"
	StatusDead     Status = "DEAD"
)

type Endpoint struct {
	Url           string
	PathTemplate  string
	Port          string
	Status        Status
	LastRetry     time.Time
	RetryAttempts int
	SLA           float64
	Cache         *lru.Cache
	Logger        *log.Logger
	mu            sync.Mutex
	mockServer    *mocks.MockServer
}

func (ep *Endpoint) Close() {
	// Ensure the cache is purged and any other resources are released
	ep.mu.Lock()
	defer ep.mu.Unlock()
	if ep.Cache != nil {
		ep.Cache.Purge()
	}
	if ep.mockServer != nil {
		err := ep.mockServer.Shutdown()
		if err != nil {
			log.Printf("Error shutting down mock server: %v", err)
		}
	}
	ep.Status = StatusDead // just in case
}

func (ep *Endpoint) GetSLADuration() time.Duration {
	return time.Duration(ep.SLA) * time.Second
}

func (ep *Endpoint) SetRetries(num int) {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	ep.RetryAttempts = num
}

func (ep *Endpoint) GetRetries() int {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	return ep.RetryAttempts
}

// SetStatus Thread-safe method to update status
func (ep *Endpoint) SetStatus(status Status) {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	ep.Status = status
}

// GetStatus Thread-safe method to get status
func (ep *Endpoint) GetStatus() Status {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	return ep.Status
}

func (ep *Endpoint) GetUrl() string {
	// Ensure the URL does not have a trailing slash
	baseUrl := strings.TrimRight(ep.Url, "/")
	return fmt.Sprintf("%s:%s", baseUrl, ep.Port) // Properly concatenate base URL and port
}

func (ep *Endpoint) GetUrlForCompany(id string) string {
	endpointUrl := ep.GetUrl()
	// Ensure path doesn't introduce extra slashes
	path := strings.TrimLeft(fmt.Sprintf(ep.PathTemplate, id), "/")
	return fmt.Sprintf("%s/%s", endpointUrl, path) // Concatenate URL and path cleanly
}

func (ep *Endpoint) ProcessError(statusCode int) {
	if !utils.TryLock(&ep.mu, 100*time.Millisecond) {
		// Log the failure to acquire lock but don't block
		ep.Logger.Printf("WARNING: Could not acquire lock to process error")
		return
	}
	defer ep.mu.Unlock()

	if statusCode >= 200 && statusCode < 300 {
		return
	}

	if ep.RetryAttempts >= len(SupportedRetryIntervals) {
		// maximum retry, kill it
		ep.Status = StatusDead
	} else {
		ep.RetryAttempts = ep.RetryAttempts + 1
	}
}

func (ep *Endpoint) Kill() {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	ep.RetryAttempts = 0
	ep.Status = StatusDead
}

func (ep *Endpoint) Reactivate() {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	ep.RetryAttempts = 0
	ep.Status = StatusActive
}

// ShouldRetry Check if the endpoint should retry: this is dependent on date and status.
// If this looks overly complex, it's to minimise unnecessary locking
func (ep *Endpoint) ShouldRetry() bool {
	// Create a channel for communicating the result
	resultChan := make(chan bool, 1)

	// Create a timeout for lock acquisition
	lockTimeout := 50 * time.Millisecond

	// Try to acquire the lock in a separate goroutine
	go func() {
		// Attempt to acquire the lock
		ep.mu.Lock()
		defer ep.mu.Unlock()

		// Default answer if we're active
		if ep.Status == StatusActive {
			resultChan <- true
			return
		}

		// Quick return if we're dead
		if ep.Status == StatusDead {
			resultChan <- false
			return
		}

		// Check retry attempt count
		if ep.RetryAttempts >= len(SupportedRetryIntervals) {
			resultChan <- false
			return
		}

		// Calculate retry duration based on the current retry attempt
		applicableRetryInterval := SupportedRetryIntervals[ep.RetryAttempts]
		retryDuration := time.Duration(applicableRetryInterval) * time.Second

		// Check if enough time has passed for retry
		// Note: The original code has a logical issue - it checks if now is after (now + duration)
		// which will always be false. Let's fix that by comparing with LastRetry + duration
		shouldRetry := time.Now().After(ep.LastRetry.Add(retryDuration))
		resultChan <- shouldRetry
	}()

	// Wait for either the result or a timeout
	select {
	case result := <-resultChan:
		return result
	case <-time.After(lockTimeout):
		// If we couldn't acquire the lock in time, log the issue and return a safe default
		if ep.Logger != nil {
			ep.Logger.Printf("WARNING: Lock acquisition timeout in ShouldRetry()")
		}
		// Conservative default - don't retry if we can't determine status
		return false
	}
}

// FetchCompany this is the main function
func (ep *Endpoint) FetchCompany(client utils.HTTPClient, id string) (models.CompanyResponse, int, error) {
	if ep.Logger != nil {
		ep.Logger.Printf("INFO: FetchCompany initiated for id: %s, endpoint: %s", id, ep.Url)
	}
	if client == nil {
		ep.Logger.Printf("WARN: Client is nil")
	}
	// Try getting from Cache first
	cacheKey := id
	if cachedValue, found := ep.Cache.Get(cacheKey); found {
		return cachedValue.(models.CompanyResponse), 200, nil
	}

	var result models.CompanyResponse

	// Cache miss, fetch from remote endpoint
	switch ep.Status {
	case StatusDead:
		return result, 404, fmt.Errorf("endpoint unavailable")
	case StatusInactive:
		if !ep.ShouldRetry() {
			return result, 404, fmt.Errorf("endpoint awaiting retry")
		}
		fallthrough
	case StatusActive:
		targetUrl := ep.GetUrlForCompany(id)
		// Process response and create result

		code, contentType, body, err := utils.GetHTTP(client, targetUrl, ep.GetSLADuration())
		if err != nil {
			ep.ProcessError(code)
			return result, code, err
		}

		if contentType == utils.V1ContentType {
			var v1 V1Response
			err = json.Unmarshal(body, &v1)
			if err != nil {
				return result, 500, err
			}
			result = v1.GetCompanyResponse()
		} else if contentType == utils.V2ContentType {
			var v2 V2Response
			err = json.Unmarshal(body, &v2)
			if err != nil {
				return result, 500, err
			}
			result = v2.GetCompanyResponse()
		} else {
			return result, 500, fmt.Errorf("invalid content type version %s", contentType)
		}

		ep.Cache.Add(cacheKey, result)
	}

	// Cache the result if successful
	return result, 200, nil
}

func New(httpClient utils.HTTPClient, l *log.Logger, endpointURL string, sla float64, endpointPathTemplate string, cacheSize int, spawnLocalhostMock bool) (IEndpoint, error) {
	l.Printf("INFO: Creating new endpoint for URL: %s", endpointURL)
	endpoint := Endpoint{Port: "80", Status: StatusActive, PathTemplate: endpointPathTemplate, SLA: sla, Logger: l}
	endpoint.Cache, _ = lru.New(cacheSize) // Or appropriate size

	// parsing the URL for well-formedness before actually pinging it
	parsedURL, err := url.Parse(endpointURL)
	if err != nil {
		// if there's an invalid host that doesn't parse, it's a fatal error
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	host := parsedURL.Hostname()
	port := parsedURL.Port()
	if port == "" {
		// Default the port if not explicitly provided in the URL
		if parsedURL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	endpoint.Port = port

	// Ensure base URL is clean
	urlWithoutPort := fmt.Sprintf("%s://%s", parsedURL.Scheme, host)
	endpoint.Url = strings.TrimRight(urlWithoutPort, "/")

	endpoint.LastRetry = time.Now()
	// Either create a mock endpoint, or ping a real one
	if spawnLocalhostMock && host == "localhost" {
		endpoint.mockServer = mocks.New(endpoint.Port)
	} else {
		code, err := utils.PingHTTP(httpClient, endpointURL, time.Duration(sla)*time.Second)
		if err != nil {
			if code >= 300 && code <= 500 {
				endpoint.Status = StatusDead // note: at this point there aren't multiple threads to worry about so could have set directly
			} else if code >= 500 {
				endpoint.Status = StatusInactive // note: at this point there aren't multiple threads to worry about so could have set directly
			}
		}
	}
	return &endpoint, err
}

func GetEndpoints(client utils.HTTPClient, l *log.Logger, appConfig config.Config) (map[string]IEndpoint, error) {
	args := utils.ParseArgs()
	endpoints := make(map[string]IEndpoint)

	for countryCode, endpointURL := range args {
		if !utils.ValidateCountryCode(countryCode) {
			// we can't have an illegal country code in the setup
			l.Fatalf("Illegal country code: %s", countryCode)
		}

		endpoint, err := New(client, l, endpointURL, appConfig.EndpointTimeout, appConfig.EndpointPathTemplate, appConfig.DefaultCacheSize, appConfig.SpawnLocalhostMocks)
		if err == nil {
			lowerCountryCode := strings.ToLower(countryCode)
			endpoints[lowerCountryCode] = endpoint
		} else {
			// Failure might be a ping failure.
			l.Printf("WARN: Endpoint construction failed for country code: %s, endpoint: %s", countryCode, endpointURL)
		}
	}
	return endpoints, nil
}
