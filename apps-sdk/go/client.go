// Package sdk is the §13.5 Go SDK for integration apps (集成者 API 的
// 客户端首发): one Client per app token — an app holds several tokens,
// each carrying its own scope set (config/trigger/query), so the client
// is deliberately scope-less and the credential decides what the calls
// may do. The full flow it serves: 配品类 → 绑受众（操作侧入口，见
// docs/guide/integration.md）→ 触发 → 查状态.
package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client talks to one app namespace with one bearer token.
type Client struct {
	baseURL string
	app     string
	token   string
	http    *http.Client
}

// New builds a client against a herald base URL (e.g.
// https://herald.example.com) for one app namespace with one token.
func New(baseURL, app, token string) *Client {
	return &Client{
		baseURL: baseURL,
		app:     app,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Error is one refused call: the HTTP status plus the wire message.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("herald: %d: %s", e.Status, e.Message)
}

// IsTransport reports whether an error is anything but a herald
// answer: transport failures and unreadable responses land here alike.
// Retrying those is the caller's call; retrying an *Error is pointless.
func IsTransport(err error) bool {
	_, ok := err.(*Error)
	return err != nil && !ok
}

// envelope is the api wire format.
type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// call runs one API round trip: non-2xx and non-zero envelopes come back
// as *Error, success decodes data into out (nil skips the decode).
func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("herald: encode request: %w", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("herald: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("herald: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("herald: %s %s: decode response: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 || env.Code != 0 {
		return &Error{Status: resp.StatusCode, Message: env.Message}
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("herald: %s %s: decode data: %w", method, path, err)
		}
	}
	return nil
}
