package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cuihairu/herald/internal/logger"
)

// Client is a HTTP client for providers
type Client struct {
	client  *http.Client
	timeout time.Duration
}

// Config is the client configuration
type Config struct {
	Timeout   time.Duration
	MaxIdleConns int
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

// PostJSON sends a JSON POST request
func (c *Client) PostJSON(ctx context.Context, url string, body interface{}) (*Response, error) {
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

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

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
		}, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
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
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Response{
			StatusCode: resp.StatusCode,
			Body:       respBody,
		}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
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

// IsRetryable returns true if the error is retryable
func IsRetryable(err error) bool {
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
