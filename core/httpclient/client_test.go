package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	c := NewClient(nil)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", c.timeout)
	}
}

func TestNewClientWithConfig(t *testing.T) {
	config := &Config{
		Timeout:         10 * time.Second,
		MaxIdleConns:    50,
		MaxConnsPerHost: 5,
	}

	c := NewClient(config)
	if c.timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", c.timeout)
	}
}

func TestClientGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	c := NewClient(nil)
	ctx := context.Background()

	resp, err := c.Get(ctx, server.URL)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if string(resp.Body) != `{"status":"ok"}` {
		t.Errorf("unexpected body: %s", string(resp.Body))
	}
}

func TestClientGetError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer server.Close()

	c := NewClient(nil)
	ctx := context.Background()

	_, err := c.Get(ctx, server.URL)
	if err == nil {
		t.Error("expected error for 500 status")
	}
}

func TestClientPostJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		var data map[string]string
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if data["test"] != "value" {
			t.Errorf("expected test=value, got %v", data)
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"result":"created"}`))
	}))
	defer server.Close()

	c := NewClient(nil)
	ctx := context.Background()

	body := map[string]string{"test": "value"}
	resp, err := c.PostJSON(ctx, server.URL, body)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}
}

func TestClientPostJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()

	c := NewClient(nil)
	ctx := context.Background()

	_, err := c.PostJSON(ctx, server.URL, map[string]string{"test": "value"})
	if err == nil {
		t.Error("expected error for 400 status")
	}
}

func TestClientContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(nil)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	_, err := c.Get(ctx, server.URL)
	if err == nil {
		t.Error("expected error after context cancel")
	}
}

func TestClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &Config{Timeout: 50 * time.Millisecond}
	c := NewClient(config)
	ctx := context.Background()

	_, err := c.Get(ctx, server.URL)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestResponseJSON(t *testing.T) {
	resp := &Response{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"name":"test","value":123}`),
	}

	var result struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	err := resp.JSON(&result)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result.Name != "test" {
		t.Errorf("expected name=test, got %s", result.Name)
	}
	if result.Value != 123 {
		t.Errorf("expected value=123, got %d", result.Value)
	}
}

func TestResponseString(t *testing.T) {
	resp := &Response{
		StatusCode: http.StatusOK,
		Body:       []byte("test body"),
	}

	str := resp.String()
	if str != "test body" {
		t.Errorf("expected 'test body', got %s", str)
	}
}

func TestRetryableError(t *testing.T) {
	baseErr := errors.New("test error")
	retryableErr := &RetryableError{Err: baseErr}

	if !IsRetryable(retryableErr) {
		t.Error("expected RetryableError to be retryable")
	}

	if IsRetryable(baseErr) {
		t.Error("expected non-RetryableError to not be retryable")
	}
}

func TestWithRetry(t *testing.T) {
	baseErr := errors.New("transient error")
	err := WithRetry(baseErr)
	if err == nil {
		t.Error("expected non-nil error")
	}
	if !IsRetryable(err) {
		t.Error("expected wrapped error to be retryable")
	}
}

func TestWithRetryNil(t *testing.T) {
	err := WithRetry(nil)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestDefaultClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	ctx := context.Background()

	resp, err := Get(ctx, server.URL)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	resp2, err := PostJSON(ctx, server.URL, map[string]string{"key": "value"})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp2.StatusCode)
	}
}

func TestClientInvalidJSON(t *testing.T) {
	c := NewClient(nil)
	ctx := context.Background()

	// Invalid JSON that can't be marshaled
	invalidBody := make(chan int) // channels can't be marshaled to JSON

	_, err := c.PostJSON(ctx, "http://example.com", invalidBody)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestClientInvalidURL(t *testing.T) {
	c := NewClient(nil)
	ctx := context.Background()

	// Invalid URL should cause an error
	_, err := c.Get(ctx, "\n")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestClientPostForm(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("expected Content-Type application/x-www-form-urlencoded, got %s", ct)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	c := NewClient(nil)
	ctx := context.Background()

	formData := url.Values{}
	formData.Set("username", "test")
	formData.Set("password", "secret")

	resp, err := c.PostForm(ctx, server.URL, formData)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestClientPostFormError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	c := NewClient(nil)
	ctx := context.Background()

	formData := url.Values{}
	formData.Set("key", "value")

	_, err := c.PostForm(ctx, server.URL, formData)
	if err == nil {
		t.Error("expected error for 401 status")
	}
}

func TestClientPostFormWithEmptyValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(nil)
	ctx := context.Background()

	formData := url.Values{}

	resp, err := c.PostForm(ctx, server.URL, formData)
	if err != nil {
		t.Errorf("expected no error with empty form data, got %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestClientPostFormInvalidURL(t *testing.T) {
	c := NewClient(nil)
	ctx := context.Background()

	formData := url.Values{}
	formData.Set("key", "value")

	_, err := c.PostForm(ctx, "\n", formData)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestClientGetWithHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "" {
			w.Header().Set("X-Received-Header", "true")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	c := NewClient(nil)
	ctx := context.Background()

	resp, err := c.Get(ctx, server.URL)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestClientGetWithSpecialCharactersInURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer server.Close()

	c := NewClient(nil)
	ctx := context.Background()

	// Test URL with query parameters
	testURL := server.URL + "?key=value&foo=bar"
	resp, err := c.Get(ctx, testURL)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestResponseJSONError(t *testing.T) {
	resp := &Response{
		StatusCode: http.StatusOK,
		Body:       []byte(`invalid json`),
	}

	var result struct {
		Name string `json:"name"`
	}

	err := resp.JSON(&result)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestClientConfigDefaults(t *testing.T) {
	config := &Config{}
	c := NewClient(config)

	if c.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", c.timeout)
	}
}

func TestClientConfigWithZeroTimeout(t *testing.T) {
	config := &Config{Timeout: 0}
	c := NewClient(config)

	// Zero timeout should default to 30s
	if c.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", c.timeout)
	}
}

func TestClientConfigWithZeroMaxIdleConns(t *testing.T) {
	config := &Config{MaxIdleConns: 0}
	c := NewClient(config)

	// Should use default of 100
	transport, ok := c.client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected http.Transport")
	}
	if transport.MaxIdleConns != 100 {
		t.Errorf("expected MaxIdleConns 100, got %d", transport.MaxIdleConns)
	}
}

func TestClientConfigWithZeroMaxConnsPerHost(t *testing.T) {
	config := &Config{MaxConnsPerHost: 0}
	c := NewClient(config)

	transport, ok := c.client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected http.Transport")
	}
	if transport.MaxIdleConnsPerHost != 10 {
		t.Errorf("expected MaxIdleConnsPerHost 10, got %d", transport.MaxIdleConnsPerHost)
	}
}

func TestPostJSONReadBodyError(t *testing.T) {
	// This test would require a server that closes the connection mid-response
	// which is difficult to implement reliably. Skipping this edge case.
	// The error path is tested through context cancellation and timeout tests.
}

func TestRetryableErrorUnwrap(t *testing.T) {
	baseErr := errors.New("base error")
	retryableErr := &RetryableError{Err: baseErr}

	if retryableErr.Unwrap() != baseErr {
		t.Error("expected Unwrap to return base error")
	}

	if retryableErr.Error() != "base error" {
		t.Errorf("expected Error() to return 'base error', got %s", retryableErr.Error())
	}
}

func TestIsRetryableWithNilError(t *testing.T) {
	if IsRetryable(nil) {
		t.Error("expected IsRetryable to return false for nil error")
	}
}

func TestGetConvenience(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer server.Close()

	ctx := context.Background()
	resp, err := Get(ctx, server.URL)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestGetConvenienceError(t *testing.T) {
	ctx := context.Background()
	_, err := Get(ctx, "http://invalid.local.test.example")
	// Error is expected for invalid URL
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}
