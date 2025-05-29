// shared/api/client.go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log" // For logging in client.go, consider using a structured logger later
	"net"
	"net/http"
	"time"
)

// HTTPError is a custom error type for HTTP responses with non-OK status codes.
type HTTPError struct {
	StatusCode int
	Message    string
	URL        string
	Method     string
	// Optional: add RequestID, Timestamp, etc. for tracing
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("HTTP error %d %s from %s %s: %s", e.StatusCode, http.StatusText(e.StatusCode), e.Method, e.URL, e.Message)
	}
	return fmt.Sprintf("HTTP error %d %s from %s %s", e.StatusCode, http.StatusText(e.StatusCode), e.Method, e.URL)
}

// Common errors for client usage. Use errors.Is for checking.
var (
	ErrNotFound      = fmt.Errorf("resource not found")
	ErrConflict      = fmt.Errorf("resource conflict")
	ErrBadRequest    = fmt.Errorf("bad request")
	ErrUnauthorized  = fmt.Errorf("unauthorized")
	ErrForbidden     = fmt.Errorf("forbidden")
	ErrInternalError = fmt.Errorf("internal server error")
)

// NewDefaultHTTPClient creates a robust http.Client with common timeouts and transport settings.
// This can be used by all API clients.
func NewDefaultHTTPClient() *http.Client {
	return &http.Client{
		// Total request timeout, including connection, handshake, writing, and reading.
		// This should be the primary timeout you rely on.
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment, // Respect HTTP_PROXY, HTTPS_PROXY env vars
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,  // Connection establishment timeout
				KeepAlive: 30 * time.Second, // Keep-alive for idle connections
			}).DialContext,
			MaxIdleConns:          100,              // Maximum idle (keep-alive) connections across all hosts
			IdleConnTimeout:       90 * time.Second, // How long idle connections are kept in the pool
			TLSHandshakeTimeout:   5 * time.Second,  // TLS handshake timeout
			ExpectContinueTimeout: 1 * time.Second,  // Timeout for the client to wait for a server's "100-continue" response
			// Disable HTTP/2 if you face issues or don't need it. Go's http.Transport enables it by default.
			// ForceAttemptHTTP2: false,
		},
	}
}

// Client is a generic HTTP client for interacting with RESTful APIs.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new API Client.
// It's recommended to pass a pre-configured http.Client (e.g., from NewDefaultHTTPClient).
func NewClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		log.Println("WARNING: NewClient called with nil httpClient. Using NewDefaultHTTPClient.")
		httpClient = NewDefaultHTTPClient()
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

// doRequest is a helper for common request logic
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	url := fmt.Sprintf("%s%s", c.baseURL, path)

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body for %s %s: %w", method, url, err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create %s request for %s: %w", method, url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Add other common headers if needed (e.g., Authorization tokens)
	// req.Header.Set("Authorization", "Bearer <token>")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Differentiate between context cancellation and other network errors
		if errors.Is(ctx.Err(), context.Canceled) {
			return fmt.Errorf("%s request to %s cancelled: %w", method, url, ctx.Err())
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%s request to %s timed out: %w", method, url, ctx.Err())
		}
		return fmt.Errorf("failed to send %s request to %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errorResponse struct {
			Message string `json:"message"`
		}
		// Try to read error message from body
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr == nil && len(bodyBytes) > 0 {
			if jsonErr := json.Unmarshal(bodyBytes, &errorResponse); jsonErr == nil && errorResponse.Message != "" {
				return createHTTPError(resp.StatusCode, errorResponse.Message, url, method)
			}
			// Fallback: If JSON decoding fails or message is empty, just include the raw body if it's small
			if len(bodyBytes) < 500 { // Limit size to avoid logging huge bodies
				return createHTTPError(resp.StatusCode, string(bodyBytes), url, method)
			}
		}
		return createHTTPError(resp.StatusCode, "", url, method) // No readable message
	}

	if result != nil {
		if resp.StatusCode == http.StatusNoContent { // Handle 204 No Content
			return nil
		}
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode %s response from %s: %w", method, url, err)
		}
	}
	return nil
}

// createHTTPError maps common status codes to predefined errors.
func createHTTPError(statusCode int, message, url, method string) error {
	httpErr := &HTTPError{StatusCode: statusCode, Message: message, URL: url, Method: method}
	switch statusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, httpErr.Error())
	case http.StatusConflict:
		return fmt.Errorf("%w: %s", ErrConflict, httpErr.Error())
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrBadRequest, httpErr.Error())
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, httpErr.Error())
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrForbidden, httpErr.Error())
	case http.StatusInternalServerError:
		fallthrough // Fall through for 5xx errors
	case http.StatusBadGateway:
		fallthrough
	case http.StatusServiceUnavailable:
		fallthrough
	case http.StatusGatewayTimeout:
		return fmt.Errorf("%w: %s", ErrInternalError, httpErr.Error())
	default:
		return httpErr // Return the generic HTTPError for others
	}
}

func (c *Client) Get(ctx context.Context, path string, result interface{}) error {
	return c.doRequest(ctx, http.MethodGet, path, nil, result)
}

func (c *Client) Post(ctx context.Context, path string, body interface{}, result interface{}) error {
	return c.doRequest(ctx, http.MethodPost, path, body, result)
}

func (c *Client) Put(ctx context.Context, path string, body interface{}, result interface{}) error {
	return c.doRequest(ctx, http.MethodPut, path, body, result)
}

// Delete performs a DELETE request.
func (c *Client) Delete(ctx context.Context, path string) error {
	return c.doRequest(ctx, http.MethodDelete, path, nil, nil) // No body, no result expected
}

// IsHTTPError checks if an error is an HTTPError and optionally matches status code.
func IsHTTPError(err error, status int) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return status == 0 || httpErr.StatusCode == status
	}
	return false
}

// GetHTTPStatusCode extracts the status code from an HTTPError if present.
func GetHTTPStatusCode(err error) int {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode
	}
	return 0
}
