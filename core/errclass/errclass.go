// Package errclass classifies provider errors (plan §15): providers turn
// their vendor-specific failures into one shared error vocabulary, and
// the retry layer decides from that vocabulary alone — retry policy never
// lives in the provider.
package errclass

import "errors"

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