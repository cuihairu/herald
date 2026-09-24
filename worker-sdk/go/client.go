package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/protocol"
	gws "github.com/gorilla/websocket"
)

// controlTimeout bounds each register/heartbeat write+read round trip.
const controlTimeout = 10 * time.Second

// TaskHandler is called when a task is received from the queue
type TaskHandler func(task *core.DeliveryTask) error

// EventHandler is called when an event is received
type EventHandler func(event *protocol.EventMessage)

// Client is the Worker SDK client.
// Remote workers register via WebSocket and consume tasks from a Queue.
// A config with an empty CoreURL skips the control plane entirely
// (embedded mode: queue consumption only).
type Client struct {
	config       *protocol.WorkerConfig
	queue        core.Queue
	state        atomic.Value // protocol.ConnectionState
	handler      TaskHandler
	eventHandler EventHandler

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	conn   *gws.Conn
	connID string

	onConnect     func()
	onDisconnect  func(err error)
	onStateChange func(state protocol.ConnectionState)
}

// NewClient creates a new Worker SDK client
func NewClient(config *protocol.WorkerConfig, queue core.Queue) *Client {
	if config == nil {
		config = protocol.DefaultWorkerConfig()
	}

	c := &Client{
		config: config,
		queue:  queue,
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

// Run starts the worker: registers with core via WebSocket, then consumes tasks from queue.
// Blocks until context is cancelled.
func (c *Client) Run(ctx context.Context) error {
	c.mu.Lock()
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	// Step 1: Register with core via WebSocket
	c.setState(protocol.StateConnecting)
	if err := c.register(ctx); err != nil {
		c.setState(protocol.StateDisconnected)
		return fmt.Errorf("failed to register: %w", err)
	}
	c.setState(protocol.StateReady)

	if c.onConnect != nil {
		c.onConnect()
	}

	// Step 2: Read control-plane messages and send heartbeats (only when
	// a control-plane connection exists).
	if c.controlConn() != nil {
		c.wg.Add(1)
		go c.readLoop(ctx)
		c.wg.Add(1)
		go c.heartbeatLoop(ctx)
	}

	// Step 3: Consume tasks from queue
	c.consumeLoop(ctx)

	return nil
}

// register performs the register/ack handshake with the Herald core. An
// empty CoreURL skips the control plane (embedded mode) and returns nil.
func (c *Client) register(ctx context.Context) error {
	if c.config.CoreURL == "" {
		return nil
	}

	dialer := gws.Dialer{HandshakeTimeout: controlTimeout}
	conn, _, err := dialer.DialContext(ctx, c.config.CoreURL, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.config.CoreURL, err)
	}

	msg := &protocol.RegisterMessage{
		WorkerID:     c.config.WorkerID,
		Mode:         "remote",
		Platform:     detectPlatform(),
		Version:      "1.0.0",
		Capabilities: c.config.Capabilities,
	}
	if err := writeControl(conn, msg); err != nil {
		// Defensive in practice: a peer reset is only surfaced by the next
		// write after the first one succeeds, and register writes exactly
		// once — read-side failures surface at ReadMessage below.
		_ = conn.Close()
		return err
	}

	// Defensive: the connection is freshly dialed and nothing has closed
	// it, so the read deadline is always settable here.
	if err := conn.SetReadDeadline(time.Now().Add(controlTimeout)); err != nil {
		_ = conn.Close()
		return err
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("read register ack: %w", err)
	}
	var ack protocol.RegisterAckMessage
	if err := json.Unmarshal(data, &ack); err != nil {
		_ = conn.Close()
		return fmt.Errorf("decode register ack: %w", err)
	}
	if !ack.Success {
		_ = conn.Close()
		return fmt.Errorf("register rejected: %s", ack.Error)
	}
	// Clear the ack deadline; the read loop runs without one.
	// Defensive: the live connection makes this deadline always settable.
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.connID = fmt.Sprintf("conn-%d", time.Now().Unix())
	c.mu.Unlock()
	return nil
}

// readLoop consumes control-plane messages until the connection drops or
// ctx is cancelled. A dropped connection marks the client Disconnected and
// fires onDisconnect; queue consumption is independent and keeps running.
// The SDK does not auto-reconnect — restart Run to re-register.
func (c *Client) readLoop(ctx context.Context) {
	defer c.wg.Done()

	for {
		if ctx.Err() != nil {
			return
		}
		conn := c.controlConn()
		if conn == nil {
			return
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() == nil && c.State() == protocol.StateReady {
				c.setState(protocol.StateDisconnected)
				if c.onDisconnect != nil {
					c.onDisconnect(err)
				}
			}
			return
		}
		c.dispatchEvent(data)
	}
}

// dispatchEvent routes an event message to the event handler; other
// message types are ignored.
func (c *Client) dispatchEvent(data []byte) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Type != protocol.MessageTypeEvent {
		return
	}
	if c.eventHandler == nil {
		return
	}
	var event protocol.EventMessage
	if err := json.Unmarshal(data, &event); err == nil {
		c.eventHandler(&event)
	}
}

// controlConn returns the live control-plane connection, or nil.
func (c *Client) controlConn() *gws.Conn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

// writeControl marshals and sends one protocol message with a deadline.
// writeControl marshals and sends one control frame. It is a package-level
// variable so tests can inject transport failures into register/heartbeat.
var writeControl = func(conn *gws.Conn, msg protocol.Message) error {
	payload, err := protocol.MarshalMessage(msg)
	if err != nil {
		return err
	}
	// Defensive: gorilla's SetWriteDeadline only records the timestamp and
	// never touches the network, so it cannot fail.
	if err := conn.SetWriteDeadline(time.Now().Add(controlTimeout)); err != nil {
		return err
	}
	return conn.WriteMessage(gws.TextMessage, payload)
}

// consumeLoop pops tasks from the queue and processes them
func (c *Client) consumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			c.setState(protocol.StateDisconnected)
			return
		default:
		}

		if c.queue == nil {
			// No queue configured (legacy mode)
			time.Sleep(1 * time.Second)
			continue
		}

		task, err := c.queue.Pop(ctx)
		if err != nil {
			if ctx.Err() != nil {
				c.setState(protocol.StateDisconnected)
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if c.handler != nil {
			if err := c.handler(task); err != nil {
				if nackErr := c.queue.Nack(ctx, task.ID, err); nackErr != nil {
					fmt.Printf("[Worker SDK] Nack failed: %v\n", nackErr)
				}
				continue
			}
		}

		if err := c.queue.Ack(ctx, task.ID); err != nil {
			fmt.Printf("[Worker SDK] Ack failed: %v\n", err)
		}
	}
}

// heartbeatLoop sends periodic heartbeats via WebSocket
func (c *Client) heartbeatLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.State() != protocol.StateReady {
				return
			}
			if err := c.sendHeartbeat(); err != nil {
				fmt.Printf("[Worker SDK] Heartbeat error: %v\n", err)
			}
		}
	}
}

func (c *Client) sendHeartbeat() error {
	conn := c.controlConn()
	if conn == nil {
		return nil
	}
	if err := writeControl(conn, &protocol.HeartbeatMessage{
		WorkerID:  c.config.WorkerID,
		Timestamp: time.Now().Unix(),
	}); err != nil {
		return err
	}
	return nil
}

// Disconnect stops the worker
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.State() == protocol.StateDisconnected {
		return nil
	}

	c.cancel()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.wg.Wait()
	c.setState(protocol.StateDisconnected)
	return nil
}

// Close closes the client
func (c *Client) Close() error {
	return c.Disconnect()
}

func detectPlatform() string {
	return runtime.GOOS
}
