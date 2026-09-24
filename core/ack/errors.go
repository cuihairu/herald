package ack

import "errors"

// ErrEmptyID is returned when an ack is recorded without an alert id.
var ErrEmptyID = errors.New("ack: alert id is required")
