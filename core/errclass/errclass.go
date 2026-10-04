// Package errclass classifies provider errors (plan §15): providers turn
// their vendor-specific failures into one shared error vocabulary, and
// the retry layer decides from that vocabulary alone — retry policy never
// lives in the provider.
package errclass

import (
	"errors"
	"time"
)

// Class is the failure category of a provider error.
type Class string

const (
	Temporary      Class = "temporary"       // transient failure: a later attempt may succeed
	Permanent      Class = "permanent"       // deterministic failure: retrying cannot succeed
	RateLimited    Class = "rate_limited"    // throttled: retry with backoff
	Authentication Class = "authentication"  // bad credentials: retrying cannot succeed
	InvalidRequest Class = "invalid_request" // rejected payload: retrying cannot succeed
	Timeout        Class = "timeout"         // timed out: a later attempt may succeed
)

// Error carries a classification plus the underlying failure. Its message
// is exactly the wrapped error's — the class rides the error chain, not
// the text, so task-log lines and API responses keep their historical
// shape.
type Error struct {
	Class Class
	Err   error
	// RetryAfter is a server-advised wait (RFC 7231 Retry-After), set on
	// rate-limited failures when the upstream states one. The retry layer
	// prefers it over its backoff curve; 0 means no advice.
	RetryAfter time.Duration
}

func (e *Error) Error() string { return e.Err.Error() }

func (e *Error) Unwrap() error { return e.Err }

// New wraps err under class. A nil err stays nil, so helpers can pass the
// no-error path through unchanged.
func New(class Class, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Class: class, Err: err}
}

// Of reports the classification carried by err, if any.
func Of(err error) (Class, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Class, true
	}
	return "", false
}

// NewWithRetryAfter wraps err under class and attaches a server-advised
// wait (the same semantics as New otherwise). Non-positive hints carry no
// information, so they are stored as zero — RetryAfterOf then reports "no
// advice" for them. A nil err stays nil.
func NewWithRetryAfter(class Class, err error, retryAfter time.Duration) error {
	if err == nil {
		return nil
	}
	if retryAfter < 0 {
		retryAfter = 0
	}
	return &Error{Class: class, Err: err, RetryAfter: retryAfter}
}

// RetryAfterOf reports the server-advised wait carried by err, if any.
// Zero hints and unclassified errors both report false, so callers can
// treat the boolean as "the upstream stated a wait".
func RetryAfterOf(err error) (time.Duration, bool) {
	var e *Error
	if errors.As(err, &e) && e.RetryAfter > 0 {
		return e.RetryAfter, true
	}
	return 0, false
}

// Retryable says whether a classified failure is worth a later attempt:
// temporary, rate-limited and timeout failures may succeed on retry;
// permanent, authentication and invalid-request ones fail again
// deterministically.
func Retryable(c Class) bool {
	switch c {
	case Temporary, RateLimited, Timeout:
		return true
	default:
		return false
	}
}