package wechatmp

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// errNetworkDown is the transport failure the stub returns; httpclient
// classifies it as a send error the probe must propagate.
var errNetworkDown = errors.New("dial tcp: connection refused")

func TestProbeNewProbeValidatesConfig(t *testing.T) {
	if _, err := NewProbe(map[string]interface{}{}); err == nil {
		t.Error("probe accepted a config without credentials")
	}
	if _, err := NewProbe(map[string]interface{}{"app_id": "only-id"}); err == nil {
		t.Error("probe accepted a config without app_secret")
	}
	p, err := NewProbe(map[string]interface{}{"app_id": "id", "app_secret": "secret", "template_id": "t"})
	if err != nil {
		t.Fatalf("NewProbe: %v", err)
	}
	if p.Channel() != "wechat_mp" {
		t.Errorf("Channel() = %q", p.Channel())
	}
}

// probeFixture builds a probe whose every outbound request (token fetch
// first, user/info second) lands on the stub handler.
func probeFixture(t *testing.T, handler stubHandler) *Probe {
	t.Helper()
	p, err := NewProbe(map[string]interface{}{"app_id": "id", "app_secret": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	st := &stubTransport{handler: handler}
	replaceTransport(t, p.client, st)
	return p
}

func TestProbeValidFollowStates(t *testing.T) {
	cases := []struct {
		name    string
		handler stubHandler
		want    bool
		wantEr  bool
	}{
		{
			name: "follower still subscribed",
			handler: func(seq int, _ recordedRequest) (int, string, error) {
				if seq == 1 {
					return tokenStub("TOKEN", 7200)
				}
				return jsonStub(http.StatusOK, `{"subscribe":1,"openid":"o1"}`)
			},
			want: true,
		},
		{
			name: "unfollowed silently",
			handler: func(seq int, _ recordedRequest) (int, string, error) {
				if seq == 1 {
					return tokenStub("TOKEN", 7200)
				}
				return jsonStub(http.StatusOK, `{"subscribe":0,"openid":"o1"}`)
			},
			want: false,
		},
		{
			name: "api error is not an opinion",
			handler: func(seq int, _ recordedRequest) (int, string, error) {
				if seq == 1 {
					return tokenStub("TOKEN", 7200)
				}
				return jsonStub(http.StatusOK, `{"errcode":40003,"errmsg":"invalid openid"}`)
			},
			wantEr: true,
		},
		{
			name: "http failure",
			handler: func(seq int, _ recordedRequest) (int, string, error) {
				return jsonStub(http.StatusInternalServerError, "{}")
			},
			wantEr: true,
		},
		{
			name: "unparseable body",
			handler: func(seq int, _ recordedRequest) (int, string, error) {
				return jsonStub(http.StatusOK, `not json`)
			},
			wantEr: true,
		},
		{
			// The token round succeeded; the user/info round is what
			// fails on the wire — a probe that cannot ask must error.
			name: "user/info transport failure",
			handler: func(seq int, _ recordedRequest) (int, string, error) {
				if seq == 1 {
					return tokenStub("TOKEN", 7200)
				}
				return 0, "", errNetworkDown
			},
			wantEr: true,
		},
		{
			// A 200 whose body is not JSON: the token worked, the answer
			// is garbage — error, never a guess.
			name: "unparseable user/info body",
			handler: func(seq int, _ recordedRequest) (int, string, error) {
				if seq == 1 {
					return tokenStub("TOKEN", 7200)
				}
				return jsonStub(http.StatusOK, `not json`)
			},
			wantEr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := probeFixture(t, tc.handler)
			got, err := p.Valid(context.Background(), "o1")
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

func TestProbeValidTransportErrorAndEmptyOpenID(t *testing.T) {
	p := probeFixture(t, func(seq int, _ recordedRequest) (int, string, error) {
		return 0, "", errNetworkDown
	})
	if _, err := p.Valid(context.Background(), "o1"); err == nil {
		t.Error("transport failure answered as an opinion")
	}
	if _, err := p.Valid(context.Background(), ""); err == nil {
		t.Error("empty openid accepted")
	}
}
