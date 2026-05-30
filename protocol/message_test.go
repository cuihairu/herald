package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMessageTypeConstants(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"MessageTypeRegister", MessageTypeRegister},
		{"MessageTypeRegisterAck", MessageTypeRegisterAck},
		{"MessageTypeHeartbeat", MessageTypeHeartbeat},
		{"MessageTypeDispatch", MessageTypeDispatch},
		{"MessageTypeAck", MessageTypeAck},
		{"MessageTypeError", MessageTypeError},
		{"MessageTypeEvent", MessageTypeEvent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value == "" {
				t.Errorf("expected non-empty value for %s", tt.name)
			}
		})
	}
}

func TestRegisterMessage(t *testing.T) {
	t.Run("Type method", func(t *testing.T) {
		msg := &RegisterMessage{}
		if msg.Type() != MessageTypeRegister {
			t.Errorf("expected type %s, got %s", MessageTypeRegister, msg.Type())
		}
	})

	t.Run("JSON marshaling", func(t *testing.T) {
		msg := &RegisterMessage{
			WorkerID:     "worker-1",
			Mode:         "remote",
			Platform:     "linux",
			Version:      "1.0.0",
			Capabilities: []string{"telegram", "slack"},
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var decoded RegisterMessage
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if decoded.WorkerID != msg.WorkerID {
			t.Errorf("expected WorkerID %s, got %s", msg.WorkerID, decoded.WorkerID)
		}
		if decoded.Mode != msg.Mode {
			t.Errorf("expected Mode %s, got %s", msg.Mode, decoded.Mode)
		}
		if decoded.Platform != msg.Platform {
			t.Errorf("expected Platform %s, got %s", msg.Platform, decoded.Platform)
		}
		if decoded.Version != msg.Version {
			t.Errorf("expected Version %s, got %s", msg.Version, decoded.Version)
		}
		if len(decoded.Capabilities) != len(msg.Capabilities) {
			t.Errorf("expected %d capabilities, got %d", len(msg.Capabilities), len(decoded.Capabilities))
		}
	})

	t.Run("empty message", func(t *testing.T) {
		msg := &RegisterMessage{}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var decoded RegisterMessage
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}

func TestRegisterAckMessage(t *testing.T) {
	t.Run("Type method", func(t *testing.T) {
		msg := &RegisterAckMessage{}
		if msg.Type() != MessageTypeRegisterAck {
			t.Errorf("expected type %s, got %s", MessageTypeRegisterAck, msg.Type())
		}
	})

	t.Run("JSON marshaling", func(t *testing.T) {
		msg := &RegisterAckMessage{
			WorkerID:  "worker-1",
			Success:   true,
			Error:     "",
			ServerID:  "herald-core",
			Timestamp: time.Now().Unix(),
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var decoded RegisterAckMessage
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if decoded.WorkerID != msg.WorkerID {
			t.Errorf("expected WorkerID %s, got %s", msg.WorkerID, decoded.WorkerID)
		}
		if !decoded.Success {
			t.Error("expected Success to be true")
		}
		if decoded.ServerID != msg.ServerID {
			t.Errorf("expected ServerID %s, got %s", msg.ServerID, decoded.ServerID)
		}
	})

	t.Run("with error", func(t *testing.T) {
		msg := &RegisterAckMessage{
			WorkerID:  "worker-1",
			Success:   false,
			Error:     "authentication failed",
			ServerID:  "herald-core",
			Timestamp: time.Now().Unix(),
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var decoded RegisterAckMessage
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if decoded.Success {
			t.Error("expected Success to be false")
		}
		if decoded.Error != "authentication failed" {
			t.Errorf("expected Error 'authentication failed', got %s", decoded.Error)
		}
	})
}

func TestHeartbeatMessage(t *testing.T) {
	t.Run("Type method", func(t *testing.T) {
		msg := &HeartbeatMessage{}
		if msg.Type() != MessageTypeHeartbeat {
			t.Errorf("expected type %s, got %s", MessageTypeHeartbeat, msg.Type())
		}
	})

	t.Run("JSON marshaling", func(t *testing.T) {
		msg := &HeartbeatMessage{
			WorkerID:  "worker-1",
			Timestamp: time.Now().Unix(),
			Status: map[string]interface{}{
				"tasks_sent": 10,
				"tasks_done": 8,
			},
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var decoded HeartbeatMessage
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if decoded.WorkerID != msg.WorkerID {
			t.Errorf("expected WorkerID %s, got %s", msg.WorkerID, decoded.WorkerID)
		}
		if decoded.Status == nil {
			t.Error("expected Status to be non-nil")
		}
	})

	t.Run("without status", func(t *testing.T) {
		msg := &HeartbeatMessage{
			WorkerID:  "worker-1",
			Timestamp: time.Now().Unix(),
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var decoded HeartbeatMessage
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}

func TestDispatchMessage(t *testing.T) {
	t.Run("Type method", func(t *testing.T) {
		msg := &DispatchMessage{}
		if msg.Type() != MessageTypeDispatch {
			t.Errorf("expected type %s, got %s", MessageTypeDispatch, msg.Type())
		}
	})

	t.Run("JSON marshaling", func(t *testing.T) {
		msg := &DispatchMessage{
			TaskID:    "task-123",
			Provider:  "telegram",
			Title:     "Test Alert",
			Body:      "Test body",
			Level:     "warning",
			Target:    "channel-1",
			Timestamp: time.Now().Unix(),
			Data: map[string]interface{}{
				"key": "value",
			},
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var decoded DispatchMessage
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if decoded.TaskID != msg.TaskID {
			t.Errorf("expected TaskID %s, got %s", msg.TaskID, decoded.TaskID)
		}
		if decoded.Provider != msg.Provider {
			t.Errorf("expected Provider %s, got %s", msg.Provider, decoded.Provider)
		}
		if decoded.Title != msg.Title {
			t.Errorf("expected Title %s, got %s", msg.Title, decoded.Title)
		}
	})
}

func TestAckMessage(t *testing.T) {
	t.Run("Type method", func(t *testing.T) {
		msg := &AckMessage{}
		if msg.Type() != MessageTypeAck {
			t.Errorf("expected type %s, got %s", MessageTypeAck, msg.Type())
		}
	})

	t.Run("JSON marshaling - success", func(t *testing.T) {
		msg := &AckMessage{
			TaskID:    "task-123",
			Success:   true,
			Error:     "",
			Timestamp: time.Now().Unix(),
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var decoded AckMessage
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if !decoded.Success {
			t.Error("expected Success to be true")
		}
	})

	t.Run("JSON marshaling - failure", func(t *testing.T) {
		msg := &AckMessage{
			TaskID:    "task-123",
			Success:   false,
			Error:     "delivery failed",
			Timestamp: time.Now().Unix(),
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var decoded AckMessage
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if decoded.Success {
			t.Error("expected Success to be false")
		}
		if decoded.Error != "delivery failed" {
			t.Errorf("expected Error 'delivery failed', got %s", decoded.Error)
		}
	})
}

func TestErrorMessage(t *testing.T) {
	t.Run("Type method", func(t *testing.T) {
		msg := &ErrorMessage{}
		if msg.Type() != MessageTypeError {
			t.Errorf("expected type %s, got %s", MessageTypeError, msg.Type())
		}
	})

	t.Run("JSON marshaling", func(t *testing.T) {
		msg := &ErrorMessage{
			Code:    "ERR_001",
			Message: "An error occurred",
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var decoded ErrorMessage
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if decoded.Code != msg.Code {
			t.Errorf("expected Code %s, got %s", msg.Code, decoded.Code)
		}
		if decoded.Message != msg.Message {
			t.Errorf("expected Message %s, got %s", msg.Message, decoded.Message)
		}
	})
}

func TestEventMessage(t *testing.T) {
	t.Run("Type method", func(t *testing.T) {
		msg := &EventMessage{}
		if msg.Type() != MessageTypeEvent {
			t.Errorf("expected type %s, got %s", MessageTypeEvent, msg.Type())
		}
	})

	t.Run("JSON marshaling", func(t *testing.T) {
		msg := &EventMessage{
			WorkerID:  "worker-1",
			EventType: "online",
			Timestamp: time.Now().Unix(),
			Data: map[string]interface{}{
				"message": "worker is online",
			},
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var decoded EventMessage
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if decoded.WorkerID != msg.WorkerID {
			t.Errorf("expected WorkerID %s, got %s", msg.WorkerID, decoded.WorkerID)
		}
		if decoded.EventType != msg.EventType {
			t.Errorf("expected EventType %s, got %s", msg.EventType, decoded.EventType)
		}
	})
}

func TestConnectionState(t *testing.T) {
	t.Run("String method", func(t *testing.T) {
		tests := []struct {
			state    ConnectionState
			expected string
		}{
			{StateDisconnected, "disconnected"},
			{StateConnecting, "connecting"},
			{StateConnected, "connected"},
			{StateRegistered, "registered"},
			{StateReady, "ready"},
			{ConnectionState(999), "unknown"},
		}

		for _, tt := range tests {
			if tt.state.String() != tt.expected {
				t.Errorf("state %d: expected string %s, got %s", tt.state, tt.expected, tt.state.String())
			}
		}
	})

	t.Run("state values", func(t *testing.T) {
		if int(StateDisconnected) != 0 {
			t.Errorf("expected StateDisconnected to be 0, got %d", StateDisconnected)
		}
		if int(StateConnecting) != 1 {
			t.Errorf("expected StateConnecting to be 1, got %d", StateConnecting)
		}
		if int(StateConnected) != 2 {
			t.Errorf("expected StateConnected to be 2, got %d", StateConnected)
		}
		if int(StateRegistered) != 3 {
			t.Errorf("expected StateRegistered to be 3, got %d", StateRegistered)
		}
		if int(StateReady) != 4 {
			t.Errorf("expected StateReady to be 4, got %d", StateReady)
		}
	})
}

func TestWorkerConfig(t *testing.T) {
	t.Run("DefaultWorkerConfig", func(t *testing.T) {
		config := DefaultWorkerConfig()

		if config.ReconnectDelay != 5*time.Second {
			t.Errorf("expected ReconnectDelay 5s, got %v", config.ReconnectDelay)
		}
		if config.HeartbeatInterval != 30*time.Second {
			t.Errorf("expected HeartbeatInterval 30s, got %v", config.HeartbeatInterval)
		}
		if config.Capabilities == nil {
			t.Error("expected Capabilities to be initialized")
		}
		if len(config.Capabilities) != 0 {
			t.Errorf("expected empty Capabilities, got %v", config.Capabilities)
		}
	})

	t.Run("JSON marshaling", func(t *testing.T) {
		config := &WorkerConfig{
			WorkerID:          "worker-1",
			CoreURL:           "ws://localhost:8081/worker",
			ReconnectDelay:    10 * time.Second,
			HeartbeatInterval: 20 * time.Second,
			Capabilities:      []string{"telegram", "slack"},
		}

		data, err := json.Marshal(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var decoded WorkerConfig
		err = json.Unmarshal(data, &decoded)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if decoded.WorkerID != config.WorkerID {
			t.Errorf("expected WorkerID %s, got %s", config.WorkerID, decoded.WorkerID)
		}
		if decoded.CoreURL != config.CoreURL {
			t.Errorf("expected CoreURL %s, got %s", config.CoreURL, decoded.CoreURL)
		}
	})
}

func TestMarshalMessage(t *testing.T) {
	t.Run("RegisterMessage", func(t *testing.T) {
		msg := &RegisterMessage{
			WorkerID:     "worker-1",
			Platform:     "linux",
			Capabilities: []string{"telegram"},
		}

		data, err := MarshalMessage(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var result map[string]interface{}
		err = json.Unmarshal(data, &result)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if result["type"] != MessageTypeRegister {
			t.Errorf("expected type %s, got %v", MessageTypeRegister, result["type"])
		}
	})

	t.Run("HeartbeatMessage", func(t *testing.T) {
		msg := &HeartbeatMessage{
			WorkerID: "worker-1",
		}

		data, err := MarshalMessage(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var result map[string]interface{}
		err = json.Unmarshal(data, &result)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if result["type"] != MessageTypeHeartbeat {
			t.Errorf("expected type %s, got %v", MessageTypeHeartbeat, result["type"])
		}
	})

	t.Run("DispatchMessage", func(t *testing.T) {
		msg := &DispatchMessage{
			TaskID:   "task-123",
			Provider: "telegram",
			Title:    "Test",
		}

		data, err := MarshalMessage(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var result map[string]interface{}
		err = json.Unmarshal(data, &result)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if result["type"] != MessageTypeDispatch {
			t.Errorf("expected type %s, got %v", MessageTypeDispatch, result["type"])
		}
	})

	t.Run("AckMessage", func(t *testing.T) {
		msg := &AckMessage{
			TaskID:  "task-123",
			Success: true,
		}

		data, err := MarshalMessage(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		var result map[string]interface{}
		err = json.Unmarshal(data, &result)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if result["type"] != MessageTypeAck {
			t.Errorf("expected type %s, got %v", MessageTypeAck, result["type"])
		}
	})
}
