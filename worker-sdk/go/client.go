package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cuihairu/herald/protocol"
)

// TaskHandler is called when a task is received
type TaskHandler func(task *protocol.DispatchMessage) error

// EventHandler is called when an event is received
type EventHandler func(event *protocol.EventMessage)

// Client is the Worker SDK client
type Client struct {
	config       *protocol.WorkerConfig
	state        atomic.Value // protocol.ConnectionState
	handler      TaskHandler
	eventHandler EventHandler

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Connection state
	connID string

	// Callbacks
	onConnect     func()
	onDisconnect  func(err error)
	onStateChange func(state protocol.ConnectionState)
}

// NewClient creates a new Worker SDK client
func NewClient(config *protocol.WorkerConfig) *Client {
	if config == nil {
		config = protocol.DefaultWorkerConfig()
	}

	c := &Client{
		config: config,
	}
	c.state.Store(protocol.StateDisconnected)

	return c
}

// Config returns the client configuration
func (c *Client) Config() *protocol.WorkerConfig {
	return c.config
}

// State returns the current connection state
func (c *Client) State() protocol.ConnectionState {
	return c.state.Load().(protocol.ConnectionState)
}

// setState sets the connection state
func (c *Client) setState(state protocol.ConnectionState) {
	old := c.state.Load().(protocol.ConnectionState)
	if old != state {
		c.state.Store(state)
		if c.onStateChange != nil {
			c.onStateChange(state)
		}
	}
}

// OnTask sets the task handler
func (c *Client) OnTask(handler TaskHandler) {
	c.handler = handler
}

// OnEvent sets the event handler
func (c *Client) OnEvent(handler EventHandler) {
	c.eventHandler = handler
}

// OnConnect sets the connect callback
func (c *Client) OnConnect(fn func()) {
	c.onConnect = fn
}

// OnDisconnect sets the disconnect callback
func (c *Client) OnDisconnect(fn func(err error)) {
	c.onDisconnect = fn
}

// OnStateChange sets the state change callback
func (c *Client) OnStateChange(fn func(state protocol.ConnectionState)) {
	c.onStateChange = fn
}

// Connect connects to the Herald core
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.State() != protocol.StateDisconnected {
		return fmt.Errorf("already connected or connecting")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.setState(protocol.StateConnecting)

	// TODO: Implement actual WebSocket connection
	// For now, simulate connection
	go c.connectLoop()

	return nil
}

// connectLoop manages the connection loop
func (c *Client) connectLoop() {
	c.wg.Add(1)
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			c.setState(protocol.StateDisconnected)
			return
		default:
		}

		if c.State() == protocol.StateDisconnected || c.State() == protocol.StateConnecting {
			if err := c.doConnect(); err != nil {
				c.setState(protocol.StateDisconnected)
				if c.onDisconnect != nil {
					c.onDisconnect(err)
				}
				// Wait before reconnecting
				select {
				case <-time.After(c.config.ReconnectDelay):
				case <-c.ctx.Done():
					return
				}
				continue
			}
		}

		// Connected, start heartbeat
		if c.State() == protocol.StateReady {
			c.heartbeatLoop()
		}
	}
}

// doConnect performs the actual connection
func (c *Client) doConnect() error {
	// TODO: Implement WebSocket connection
	// For now, simulate successful connection
	time.Sleep(100 * time.Millisecond)

	c.setState(protocol.StateConnected)

	// Send register message
	if err := c.sendRegister(); err != nil {
		return fmt.Errorf("failed to register: %w", err)
	}

	// Wait for register ack
	// TODO: Implement actual message receive
	c.setState(protocol.StateReady)
	c.connID = fmt.Sprintf("conn-%d", time.Now().Unix())

	if c.onConnect != nil {
		c.onConnect()
	}

	return nil
}

// sendRegister sends a register message
func (c *Client) sendRegister() error {
	msg := &protocol.RegisterMessage{
		WorkerID:     c.config.WorkerID,
		Platform:     detectPlatform(),
		Version:      "1.0.0",
		Capabilities: c.config.Capabilities,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// TODO: Send via WebSocket
	fmt.Printf("[Worker SDK] Sending register: %s\n", string(data))
	return nil
}

// heartbeatLoop sends periodic heartbeats
func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if c.State() != protocol.StateReady {
				return
			}
			if err := c.sendHeartbeat(); err != nil {
				fmt.Printf("[Worker SDK] Heartbeat error: %v\n", err)
				return
			}
		}
	}
}

// sendHeartbeat sends a heartbeat message
func (c *Client) sendHeartbeat() error {
	msg := &protocol.HeartbeatMessage{
		WorkerID:  c.config.WorkerID,
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// TODO: Send via WebSocket
	fmt.Printf("[Worker SDK] Sending heartbeat: %s\n", string(data))
	return nil
}

// Ack acknowledges a task completion
func (c *Client) Ack(taskID string, success bool, errMsg string) error {
	msg := &protocol.AckMessage{
		TaskID:    taskID,
		Success:   success,
		Error:     errMsg,
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// TODO: Send via WebSocket
	fmt.Printf("[Worker SDK] Sending ack: %s\n", string(data))
	return nil
}

// SendEvent sends an event to the core
func (c *Client) SendEvent(eventType string, data map[string]interface{}) error {
	msg := &protocol.EventMessage{
		WorkerID:  c.config.WorkerID,
		EventType: eventType,
		Data:      data,
		Timestamp: time.Now().Unix(),
	}

	dataBytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// TODO: Send via WebSocket
	fmt.Printf("[Worker SDK] Sending event: %s\n", string(dataBytes))
	return nil
}

// Disconnect disconnects from the core
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.State() == protocol.StateDisconnected {
		return nil
	}

	c.cancel()
	c.wg.Wait()
	c.setState(protocol.StateDisconnected)

	return nil
}

// Close closes the client
func (c *Client) Close() error {
	return c.Disconnect()
}

func detectPlatform() string {
	// Simple platform detection
	return "unknown"
}
