package protocol

import (
	"time"
)

// Message types
const (
	MessageTypeRegister    = "register"
	MessageTypeRegisterAck = "register_ack"
	MessageTypeHeartbeat   = "heartbeat"
	MessageTypeDispatch    = "dispatch"
	MessageTypeAck         = "ack"
	MessageTypeError       = "error"
	MessageTypeEvent       = "event"
)

// Message is the base interface for all protocol messages
type Message interface {
	Type() string
}

// RegisterMessage is sent by worker to register with the core
type RegisterMessage struct {
	WorkerID     string   `json:"worker_id"`
	Platform     string   `json:"platform"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

func (m *RegisterMessage) Type() string { return MessageTypeRegister }

// RegisterAckMessage is sent by core to acknowledge registration
type RegisterAckMessage struct {
	WorkerID  string `json:"worker_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	ServerID  string `json:"server_id"`
	Timestamp int64  `json:"timestamp"`
}

func (m *RegisterAckMessage) Type() string { return MessageTypeRegisterAck }

// HeartbeatMessage is sent periodically by worker
type HeartbeatMessage struct {
	WorkerID  string                 `json:"worker_id"`
	Timestamp int64                  `json:"timestamp"`
	Status    map[string]interface{} `json:"status,omitempty"`
}

func (m *HeartbeatMessage) Type() string { return MessageTypeHeartbeat }

// DispatchMessage is sent by core to dispatch a task
type DispatchMessage struct {
	TaskID    string                 `json:"task_id"`
	Provider  string                 `json:"provider"`
	Title     string                 `json:"title"`
	Body      string                 `json:"body"`
	Level     string                 `json:"level,omitempty"`
	Target    string                 `json:"target,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp int64                  `json:"timestamp"`
}

func (m *DispatchMessage) Type() string { return MessageTypeDispatch }

// AckMessage is sent by worker to acknowledge task completion
type AckMessage struct {
	TaskID    string `json:"task_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

func (m *AckMessage) Type() string { return MessageTypeAck }

// ErrorMessage represents an error
type ErrorMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (m *ErrorMessage) Type() string { return MessageTypeError }

// EventMessage is sent by worker to notify core of events
type EventMessage struct {
	WorkerID  string                 `json:"worker_id"`
	EventType string                 `json:"event_type"` // online, offline, error
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp int64                  `json:"timestamp"`
}

func (m *EventMessage) Type() string { return MessageTypeEvent }

// ConnectionState represents the connection state
type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateRegistered
	StateReady
)

func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateRegistered:
		return "registered"
	case StateReady:
		return "ready"
	default:
		return "unknown"
	}
}

// WorkerConfig is the worker configuration
type WorkerConfig struct {
	WorkerID          string        `json:"worker_id"`
	CoreURL           string        `json:"core_url"` // WebSocket URL
	ReconnectDelay    time.Duration `json:"reconnect_delay"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
	Capabilities      []string      `json:"capabilities"`
}

// DefaultWorkerConfig returns default worker configuration
func DefaultWorkerConfig() *WorkerConfig {
	return &WorkerConfig{
		ReconnectDelay:    5 * time.Second,
		HeartbeatInterval: 30 * time.Second,
		Capabilities:      []string{},
	}
}
