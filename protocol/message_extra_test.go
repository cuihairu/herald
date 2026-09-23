package protocol

import (
	"errors"
	"strings"
	"testing"
)

// arrayMessage marshals to a JSON array, which cannot unmarshal into the
// map[string]interface{} that MarshalMessage decodes into.
type arrayMessage struct{}

func (arrayMessage) Type() string                 { return "array" }
func (arrayMessage) MarshalJSON() ([]byte, error) { return []byte("[1,2,3]"), nil }

// brokenMessage fails at json.Marshal time.
type brokenMessage struct{}

func (brokenMessage) Type() string                 { return "broken" }
func (brokenMessage) MarshalJSON() ([]byte, error) { return nil, errors.New("marshal boom") }

func TestMarshalMessageUnmarshalError(t *testing.T) {
	_, err := MarshalMessage(arrayMessage{})
	if err == nil {
		t.Error("MarshalMessage(array payload) should fail to decode into object map")
	}
}

func TestMarshalMessageMarshalError(t *testing.T) {
	_, err := MarshalMessage(brokenMessage{})
	if err == nil || !strings.Contains(err.Error(), "marshal boom") {
		t.Errorf("MarshalMessage() error = %v, want wrapped marshal boom", err)
	}
}
