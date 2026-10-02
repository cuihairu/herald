package httpclient

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// deadServerAddr starts a listener and immediately closes it, yielding an
// address that reliably refuses connections.
func deadServerAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// abortingServer returns a server that sends a truncated response with an
// inflated Content-Length and then aborts the connection mid-body.
func abortingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("partial")); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush() // push headers + partial body onto the wire
		}
		panic(http.ErrAbortHandler) // forcibly close the connection mid-body
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPostJSONRequestCreateError(t *testing.T) {
	c := NewClient(nil)
	_, err := c.PostJSON(context.Background(), "http://127.0.0.1\x00/", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("PostJSON() error = %v, want request creation failure", err)
	}
}

func TestPostJSONSendError(t *testing.T) {
	c := NewClient(&Config{Timeout: 2 * time.Second})
	_, err := c.PostJSON(context.Background(), "http://"+deadServerAddr(t)+"/", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("PostJSON() error = %v, want send failure", err)
	}
}

func TestPostJSONResponseBodyReadError(t *testing.T) {
	srv := abortingServer(t)
	c := NewClient(&Config{Timeout: 5 * time.Second})
	_, err := c.PostJSON(context.Background(), srv.URL, map[string]string{"k": "v"})
	if err == nil || !strings.Contains(err.Error(), "failed to read response") {
		t.Errorf("PostJSON() error = %v, want body read failure", err)
	}
}

func TestPostJSONMarshalError(t *testing.T) {
	c := NewClient(nil)
	_, err := c.PostJSON(context.Background(), "http://127.0.0.1:1/", map[string]interface{}{"ch": make(chan int)})
	if err == nil || !strings.Contains(err.Error(), "failed to marshal body") {
		t.Errorf("PostJSON() error = %v, want marshal failure", err)
	}
}

func TestPostFormSendError(t *testing.T) {
	c := NewClient(&Config{Timeout: 2 * time.Second})
	_, err := c.PostForm(context.Background(), "http://"+deadServerAddr(t)+"/", url.Values{"a": {"b"}})
	if err == nil || !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("PostForm() error = %v, want send failure", err)
	}
}

func TestPostFormResponseBodyReadError(t *testing.T) {
	srv := abortingServer(t)
	c := NewClient(&Config{Timeout: 5 * time.Second})
	_, err := c.PostForm(context.Background(), srv.URL, url.Values{"a": {"b"}})
	if err == nil || !strings.Contains(err.Error(), "failed to read response") {
		t.Errorf("PostForm() error = %v, want body read failure", err)
	}
}

func TestGetSendError(t *testing.T) {
	c := NewClient(&Config{Timeout: 2 * time.Second})
	_, err := c.Get(context.Background(), "http://"+deadServerAddr(t)+"/")
	if err == nil || !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("Get() error = %v, want send failure", err)
	}
}

func TestGetResponseBodyReadError(t *testing.T) {
	srv := abortingServer(t)
	c := NewClient(&Config{Timeout: 5 * time.Second})
	_, err := c.Get(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "failed to read response") {
		t.Errorf("Get() error = %v, want body read failure", err)
	}
}

func TestLogResponse(t *testing.T) {
	// Both branches should be safe to call; they only emit log records.
	LogResponse("unit-test", nil, context.DeadlineExceeded)

	LogResponse("unit-test", &Response{
		StatusCode: http.StatusTeapot,
		Body:       []byte(`{"ok":false}`),
	}, nil)
}

func TestWithClientCert(t *testing.T) {
	orig := NewClient(&Config{Timeout: 7 * time.Second})
	with := orig.WithClientCert(tls.Certificate{})

	if with == orig {
		t.Fatal("WithClientCert returned the receiver, want a copy")
	}
	origTransport, ok := orig.client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("original transport is not *http.Transport")
	}
	withTransport, ok := with.client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("derived transport is not *http.Transport")
	}
	if withTransport == origTransport {
		t.Error("transport not cloned; the original would be mutated")
	}
	if withTransport.TLSClientConfig == origTransport.TLSClientConfig {
		t.Fatal("tls.Config not cloned; installing a certificate would leak into the original")
	}
	if n := len(origTransport.TLSClientConfig.Certificates); n != 0 {
		t.Errorf("original client certificates = %d, want 0 (receiver untouched)", n)
	}
	if withTransport.TLSClientConfig == nil || len(withTransport.TLSClientConfig.Certificates) != 1 {
		t.Fatalf("derived TLSClientConfig = %+v, want one client certificate", withTransport.TLSClientConfig)
	}
	// HTTP/2 must still be negotiable after cloning — APNs requires it.
	if !containsProto(withTransport.TLSClientConfig.NextProtos, "h2") {
		t.Errorf("derived NextProtos = %v, want h2 kept for HTTP/2", withTransport.TLSClientConfig.NextProtos)
	}
	if with.timeout != orig.timeout {
		t.Errorf("timeout = %v, want preserved %v", with.timeout, orig.timeout)
	}
}

// containsProto reports whether protos contains want.
func containsProto(protos []string, want string) bool {
	for _, p := range protos {
		if p == want {
			return true
		}
	}
	return false
}
