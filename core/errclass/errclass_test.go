package errclass

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestNewNilPassthrough(t *testing.T) {
	if err := New(Temporary, nil); err != nil {
		t.Errorf("New(Temporary, nil) = %v, want nil", err)
	}
}

func TestErrorTextIsTheWrappedOnes(t *testing.T) {
	// The class rides the error chain, not the message: log lines and API
	// responses keep their historical text.
	inner := errors.New("unexpected status code: 429, body: slow down")
	err := New(RateLimited, inner)
	if err.Error() != inner.Error() {
		t.Errorf("Error() = %q, want the wrapped text %q", err.Error(), inner.Error())
	}
	if !errors.Is(err, inner) {
		t.Error("expected errors.Is to see through the classification")
	}
}

func TestOfSeesThroughWrapping(t *testing.T) {
	inner := errors.New("boom")
	err := fmt.Errorf("provider %s failed: %w", "telegram", New(Timeout, inner))
	got, ok := Of(err)
	if !ok {
		t.Fatal("expected the classification to be found through wrapping")
	}
	if got != Timeout {
		t.Errorf("class = %q, want %q", got, Timeout)
	}
	if _, ok := Of(errors.New("plain")); ok {
		t.Error("expected no classification on an unclassified error")
	}
}

func TestRetryableByClass(t *testing.T) {
	cases := []struct {
		class     Class
		retryable bool
	}{
		{Temporary, true},
		{RateLimited, true},
		{Timeout, true},
		{Permanent, false},
		{Authentication, false},
		{InvalidRequest, false},
		{"", false},
		{"unknown", false},
	}
	for _, tc := range cases {
		if got := Retryable(tc.class); got != tc.retryable {
			t.Errorf("Retryable(%q) = %v, want %v", tc.class, got, tc.retryable)
		}
	}
}

func TestNewWithRetryAfter(t *testing.T) {
	if err := NewWithRetryAfter(RateLimited, nil, time.Second); err != nil {
		t.Errorf("NewWithRetryAfter(RateLimited, nil, ...) = %v, want nil", err)
	}

	// Negative advice carries no information: stored as zero.
	err := NewWithRetryAfter(RateLimited, errors.New("429"), -time.Second)
	if d, ok := RetryAfterOf(err); ok {
		t.Errorf("negative hint: RetryAfterOf = %v/%v, want 0/false", d, ok)
	}

	inner := errors.New("429 slow down")
	err = NewWithRetryAfter(RateLimited, inner, 3*time.Second)
	if err.Error() != inner.Error() {
		t.Errorf("Error() = %q, want the wrapped text %q", err.Error(), inner.Error())
	}
}

func TestRetryAfterOf(t *testing.T) {
	if _, ok := RetryAfterOf(errors.New("plain")); ok {
		t.Error("expected no hint on an unclassified error")
	}
	if _, ok := RetryAfterOf(New(RateLimited, errors.New("429"))); ok {
		t.Error("expected no hint when the advice is zero")
	}

	err := fmt.Errorf("provider %s: %w", "sms", NewWithRetryAfter(RateLimited, errors.New("429"), 30*time.Second))
	d, ok := RetryAfterOf(err)
	if !ok {
		t.Fatal("expected the hint to be found through wrapping")
	}
	if d != 30*time.Second {
		t.Errorf("hint = %v, want %v", d, 30*time.Second)
	}
}