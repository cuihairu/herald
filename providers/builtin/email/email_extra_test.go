package email

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

// startSMTPStub starts a minimal in-process SMTP server on 127.0.0.1 that
// speaks just enough of the protocol for smtp.SendMail to deliver one
// message, keeping every test offline. It advertises AUTH PLAIN so the
// provider's credential path runs, and records the AUTH command line and the
// DATA payload for assertions. SMTP clients only authenticate to localhost
// over a plaintext connection, which is why the stub binds 127.0.0.1.
func startSMTPStub(t *testing.T) (addr string, dataCh <-chan string, authCh <-chan string) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	// Buffered so the handler never blocks before replying to the client.
	data := make(chan string, 1)
	auth := make(chan string, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		r := bufio.NewReader(conn)
		write := func(format string, args ...interface{}) {
			_, _ = fmt.Fprintf(conn, format+"\r\n", args...)
		}

		write("220 smtp.test.local ESMTP ready")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				write("250-smtp.test.local")
				write("250-AUTH PLAIN")
				write("250 8BITMIME")
			case strings.HasPrefix(cmd, "AUTH"):
				auth <- strings.TrimSpace(line)
				write("235 2.7.0 Authentication successful")
			case strings.HasPrefix(cmd, "MAIL FROM"), strings.HasPrefix(cmd, "RCPT TO"):
				write("250 2.1.0 Ok")
			case cmd == "DATA":
				write("354 End data with <CR><LF>.<CR><LF>")
				var buf bytes.Buffer
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" || l == ".\n" {
						break
					}
					buf.WriteString(l)
				}
				data <- buf.String()
				write("250 2.0.0 Ok: queued")
			case cmd == "RSET":
				write("250 2.0.0 Ok")
			case cmd == "NOOP":
				write("250 2.0.0 Ok")
			case cmd == "QUIT":
				write("221 2.7.0 Bye")
				return
			default:
				write("502 5.5.2 Command not implemented")
			}
		}
	}()

	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	return ln.Addr().String(), data, auth
}

func smtpHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("failed to split stub address %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("failed to parse stub port %q: %v", portStr, err)
	}
	return host, port
}

func TestDeliverSuccessWithAuth(t *testing.T) {
	addr, dataCh, authCh := startSMTPStub(t)
	host, port := smtpHostPort(t, addr)

	provider, err := NewProvider(map[string]any{
		"host":      host,
		"port":      port,
		"username":  "herald",
		"password":  "s3cret",
		"from":      "sender@example.com",
		"from_name": "Herald",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	task := &core.DeliveryTask{
		ID:      "task-1",
		Level:   "error",
		Targets: []string{"oncall@example.com", "ops@example.com"},
		Payload: core.DeliveryPayload{
			Kind: core.PayloadContent,
			Content: &core.RenderedContent{
				Title:  "Disk full",
				Body:   "93% used",
				Format: "html",
			},
		},
		CreatedAt: time.Now(),
	}

	if err := provider.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver() error = %v, want nil", err)
	}

	// The AUTH PLAIN initial response must carry the configured credentials.
	authLine := <-authCh
	const prefix = "AUTH PLAIN "
	if !strings.HasPrefix(authLine, prefix) {
		t.Fatalf("expected AUTH PLAIN command, got %q", authLine)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authLine, prefix))
	if err != nil {
		t.Fatalf("failed to decode AUTH PLAIN response: %v", err)
	}
	if got, want := string(decoded), "\x00herald\x00s3cret"; got != want {
		t.Errorf("credentials = %q, want %q", got, want)
	}

	msg := <-dataCh
	if !strings.Contains(msg, "Subject: [ERROR] Disk full\r\n") {
		t.Errorf("message missing level-prefixed subject: %q", msg)
	}
	if !strings.Contains(msg, "From: Herald <sender@example.com>\r\n") {
		t.Errorf("message missing From header: %q", msg)
	}
	if !strings.Contains(msg, "To: oncall@example.com, ops@example.com\r\n") {
		t.Errorf("message missing To header: %q", msg)
	}
	if !strings.Contains(msg, "MIME-Version: 1.0\r\n") {
		t.Errorf("message missing MIME-Version header: %q", msg)
	}
	if !strings.Contains(msg, "Content-Type: text/html; charset=UTF-8\r\n") {
		t.Errorf("message missing html content type: %q", msg)
	}
	if !strings.Contains(msg, "93% used") {
		t.Errorf("message missing body: %q", msg)
	}
}

func TestDeliverSuccessWithoutAuth(t *testing.T) {
	addr, dataCh, authCh := startSMTPStub(t)
	host, port := smtpHostPort(t, addr)

	// No username/password: the provider must send unauthenticated.
	provider, err := NewProvider(map[string]any{
		"host": host,
		"port": port,
		"from": "sender@example.com",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	task := &core.DeliveryTask{
		ID:      "task-2",
		Targets: []string{"dev@example.com"},
		Payload: core.DeliveryPayload{
			Kind: core.PayloadContent,
			Content: &core.RenderedContent{
				Title:  "Build finished",
				Body:   "all green",
				Format: "plain",
			},
		},
		CreatedAt: time.Now(),
	}

	if err := provider.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver() error = %v, want nil", err)
	}

	select {
	case line := <-authCh:
		t.Errorf("expected no AUTH exchange without credentials, got %q", line)
	default:
	}

	msg := <-dataCh
	// Empty level: subject carries no prefix.
	if !strings.Contains(msg, "Subject: Build finished\r\n") {
		t.Errorf("message missing unprefixed subject: %q", msg)
	}
	if !strings.Contains(msg, "From: sender@example.com\r\n") {
		t.Errorf("message missing plain From header: %q", msg)
	}
	if !strings.Contains(msg, "Content-Type: text/plain; charset=UTF-8\r\n") {
		t.Errorf("message missing plain content type: %q", msg)
	}
	if !strings.Contains(msg, "all green") {
		t.Errorf("message missing body: %q", msg)
	}
}
