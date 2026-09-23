package tencentsms

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestDeliverMarshalRequestError covers the defensive json.Marshal failure
// branch in sendRequest. SendSmsRequest only holds string fields, so the real
// encoder can never fail on it: the package-level jsonMarshal seam is
// overridden to inject the failure instead. The package variable must be
// restored for other tests, so t.Parallel is intentionally not used.
func TestDeliverMarshalRequestError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("injected marshal failure") }
	defer func() { jsonMarshal = orig }()

	p := newTestProvider(t, "sms.tencentcloudapi.com")

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "10001", nil))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to marshal request") {
		t.Errorf("expected 'failed to marshal request' error, got %v", err)
	}
	if !strings.Contains(err.Error(), "injected marshal failure") {
		t.Errorf("expected wrapped cause in error, got %v", err)
	}
}

// TestSendRequestMarshalError exercises the same branch directly on
// sendRequest, without going through Deliver and the retry wrapper.
func TestSendRequestMarshalError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("injected marshal failure") }
	defer func() { jsonMarshal = orig }()

	p := newTestProvider(t, "sms.tencentcloudapi.com")

	err := p.sendRequest(context.Background(), SendSmsRequest{
		SmsSdkAppId:    "1400000000",
		PhoneNumberSet: []string{"+8613800138000"},
		TemplateID:     "10001",
	})
	if err == nil || !strings.Contains(err.Error(), "failed to marshal request") {
		t.Errorf("expected 'failed to marshal request' error, got %v", err)
	}
}
