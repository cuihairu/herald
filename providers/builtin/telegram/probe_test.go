package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewProbeConfig(t *testing.T) {
	if _, err := NewProbe(map[string]interface{}{}); err == nil {
		t.Error("probe accepted a config without token")
	}
	p, err := NewProbe(map[string]interface{}{"token": "tok-1"})
	if err != nil {
		t.Fatalf("NewProbe: %v", err)
	}
	if p.apiURL != defaultAPIBase {
		t.Errorf("apiURL = %q, want default", p.apiURL)
	}
	if p.Channel() != "telegram" {
		t.Errorf("Channel() = %q", p.Channel())
	}
	p, err = NewProbe(map[string]interface{}{"token": "tok-1", "api_url": "https://mirror.example"})
	if err != nil || p.apiURL != "https://mirror.example" {
		t.Errorf("api_url override = (%q, %v)", p.apiURL, err)
	}
}

func TestProbeValidAnswers(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
		wantEr bool
	}{
		{name: "known chat", status: 200, body: `{"ok":true,"result":{}}`, want: true},
		{name: "unknown chat is the platform's no", status: 400, body: `{"ok":false,"description":"chat not found"}`, want: false},
		{name: "blocked bot", status: 403, body: `{"ok":false,"description":"Forbidden: bot was blocked by the user"}`, want: false},
		{name: "200 with ok false", status: 200, body: `{"ok":false}`, want: false},
		{name: "server error", status: 500, body: `<html>bad gateway</html>`, wantEr: true},
		{name: "unparseable body", status: 200, body: `not json`, wantEr: true},
		{name: "400 without envelope", status: 400, body: `garbage`, wantEr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer ts.Close()
			p, err := NewProbe(map[string]interface{}{"token": "tok", "api_url": ts.URL})
			if err != nil {
				t.Fatal(err)
			}
			got, err := p.Valid(context.Background(), "42")
			if tc.wantEr {
				if err == nil {
					t.Fatalf("Valid = %v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Valid: %v", err)
			}
			if got != tc.want {
				t.Errorf("Valid = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProbeValidTransportErrorAndEmptyChat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	p, err := NewProbe(map[string]interface{}{"token": "tok", "api_url": ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	ts.Close() // connection refused from here on
	if _, err := p.Valid(context.Background(), "42"); err == nil {
		t.Error("transport failure answered as an opinion")
	}
	if _, err := p.Valid(context.Background(), ""); err == nil {
		t.Error("empty chat id accepted")
	}
}
