package aliyunsms

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/httpclient"
)

// This file intentionally complements aliyunsms_test.go, which owns the shared
// test helpers (validConfig, newTestProvider, templateTask, the shared TLS
// stub server and its routing helpers). Only additional edge-path tests live
// here so that the two files never redeclare symbols.

func TestDeliverCreateRequestError(t *testing.T) {
	// A non-numeric port makes the request URL unparseable before any
	// connection is attempted.
	p := newTestProvider(t, "127.0.0.1:not-a-port")

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "SMS_12345678", nil))
	if err == nil || !strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("expected create request error, got %v", err)
	}
	if !httpclient.IsRetryable(err) {
		t.Error("expected retryable error")
	}
}

func TestDeliverContextCanceled(t *testing.T) {
	p := newTestProvider(t, stubEndpoint())

	setStub(t, func(w http.ResponseWriter, r *http.Request) {
		// Drain the (unread) form body so the server can notice the dropped
		// connection, then hold until the client goes away. The timer bounds
		// the wait so the shared stub server can never block on close.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := p.Deliver(ctx, templateTask([]string{"13800138000"}, "SMS_12345678", nil))
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
	if !strings.Contains(err.Error(), "canceled") && !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context error, got %v", err)
	}
}

func TestSignSignatureOnlyParams(t *testing.T) {
	// A params map containing only the reserved Signature key signs the same
	// minimal string to sign as an empty map.
	p := &Provider{accessKeySecret: "testsecret"}
	if got := p.sign(map[string]string{"Signature": "IGNORED"}, "POST"); got != "0TS6mljAaR1otoyy5oJ3S3FnDhw=" {
		t.Errorf("sign(Signature only) = %q, expected %q", got, "0TS6mljAaR1otoyy5oJ3S3FnDhw=")
	}
}
