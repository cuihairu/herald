package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cuihairu/herald/core/errclass"
	"github.com/cuihairu/herald/internal/logger"
)

// Client is a HTTP client for providers
type Client struct {
	client  *http.Client
	timeout time.Duration
}

// Config is the client configuration
type Config struct {
	Timeout         time.Duration
	MaxIdleConns    int
	MaxConnsPerHost int
}

// NewClient creates a new HTTP client
func NewClient(config *Config) *Client {
	if config == nil {
		config = &Config{}
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	maxIdleConns := config.MaxIdleConns
	if maxIdleConns == 0 {
		maxIdleConns = 100
	}

	maxConnsPerHost := config.MaxConnsPerHost
	if maxConnsPerHost == 0 {
		maxConnsPerHost = 10
	}

	return &Client{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        maxIdleConns,
				MaxIdleConnsPerHost: maxConnsPerHost,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		timeout: timeout,
	}
}

// classifyStatus maps an HTTP status onto the shared error vocabulary
// (§15): request timeout, rate limiting and server-side errors are
// transient classes; other 4xx codes are deterministic client errors
// (bad payload, bad credentials) — retrying them cannot succeed, so they
// stay unclassified.
func classifyStatus(code int) (errclass.Class, bool) {
	switch {
	case code == http.StatusRequestTimeout:
		return errclass.Timeout, true
	case code == http.StatusTooManyRequests:
		return errclass.RateLimited, true
	case code >= 500:
		return errclass.Temporary, true
	default:
		return "", false
	}
}

// statusError builds the error for a non-2xx response, classifying it
// when the status is transient. The response itself is still returned so
// callers can inspect the body for diagnostics.
func statusError(code int, body []byte) error {
	err := fmt.Errorf("unexpected status code: %d, body: %s", code, string(body))
	if class, ok := classifyStatus(code); ok {
		return errclass.New(class, err)
	}
	return err
}

// sendError classifies a request-transport failure: network timeouts get
// their own class (§15), everything else on the wire is temporary.
func sendError(err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return errclass.New(errclass.Timeout, fmt.Errorf("failed to send request: %w", err))
	}
	return errclass.New(errclass.Temporary, fmt.Errorf("failed to send request: %w", err))
}

// WithClientCert returns a copy of the client that presents the given
// TLS client certificate on every connection (e.g. APNs certificate
// authentication). The receiver's transport is untouched: Clone gives
// the copy its own tls.Config (go1.26's Transport.Clone materializes one
// with the h2 ALPN protocols on both sides, so only Certificates is set
// on the clone — replacing the config would drop HTTP/2 negotiation).
func (c *Client) WithClientCert(cert tls.Certificate) *Client {
	transport := c.client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
	httpClient := *c.client
	httpClient.Transport = transport
	return &Client{client: &httpClient, timeout: c.timeout}
}

// PostJSON sends a JSON POST request
func (c *Client) PostJSON(ctx context.Context, url string, body interface{}) (*Response, error) {
	return c.PostJSONWithHeaders(ctx, url, body, nil)
}

// PostJSONWithHeaders sends a JSON POST request with extra request headers
// (e.g. Authorization for token-authenticated APIs). Values overwrite the
// defaults on collision.
func (c *Client) PostJSONWithHeaders(ctx context.Context, url string, body interface{}, headers map[string]string) (*Response, error) {
	// Marshal body
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal body: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, sendError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Response{
			StatusCode: resp.StatusCode,
			Body:       respBody,
		}, statusError(resp.StatusCode, respBody)
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Body:       respBody,
	}, nil
}

// PostForm sends a form-encoded POST request
func (c *Client) PostForm(ctx context.Context, reqURL string, data url.Values) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, sendError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Response{
			StatusCode: resp.StatusCode,
			Body:       respBody,
		}, statusError(resp.StatusCode, respBody)
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Body:       respBody,
	}, nil
}

// Get sends a GET request
func (c *Client) Get(ctx context.Context, url string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, sendError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Response{
			StatusCode: resp.StatusCode,
			Body:       respBody,
		}, statusError(resp.StatusCode, respBody)
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Body:       respBody,
	}, nil
}

// Response is an HTTP response
type Response struct {
	StatusCode int
	Body       []byte
}

// JSON unmarshals the response body into v
func (r *Response) JSON(v interface{}) error {
	return json.Unmarshal(r.Body, v)
}

// String returns the response body as string
func (r *Response) String() string {
	return string(r.Body)
}

// Default client
var defaultClient = NewClient(nil)

// PostJSON is a convenience function using the default client
func PostJSON(ctx context.Context, url string, body interface{}) (*Response, error) {
	return defaultClient.PostJSON(ctx, url, body)
}

// Get is a convenience function using the default client
func Get(ctx context.Context, url string) (*Response, error) {
	return defaultClient.Get(ctx, url)
}

// RetryableError wraps an error to indicate it can be retried
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// IsRetryable returns true if the error is retryable. A classified error
// (core/errclass) decides by its class; the legacy WithRetry marker stays
// accepted for compatibility.
func IsRetryable(err error) bool {
	if class, ok := errclass.Of(err); ok {
		return errclass.Retryable(class)
	}
	if _, ok := err.(*RetryableError); ok {
		return true
	}
	return false
}

// WithRetry wraps an error as retryable
func WithRetry(err error) error {
	if err == nil {
		return nil
	}
	return &RetryableError{Err: err}
}

// LogResponse logs the response for debugging
func LogResponse(provider string, resp *Response, err error) {
	if err != nil {
		logger.Error("http request failed",
			"provider", provider,
			"error", err,
		)
		return
	}

	logger.Debug("http request success",
		"provider", provider,
		"status", resp.StatusCode,
		"body", string(resp.Body),
	)
}
