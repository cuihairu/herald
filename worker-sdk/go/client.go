package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/protocol"
)

// TaskHandler is called when a task is received from the queue
type TaskHandler func(task *core.DeliveryTask) error

// EventHandler is called when an event is received
type EventHandler func(event *protocol.EventMessage)

// Client is the Worker SDK client.
// Remote workers register via WebSocket and consume tasks from a Queue.
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

	// Step 2: Start heartbeat
	c.wg.Add(1)
	go c.heartbeatLoop(ctx)

	// Step 3: Consume tasks from queue
	c.consumeLoop(ctx)

	return nil
}

// register registers the worker with the Herald core via WebSocket
func (c *Client) register(ctx context.Context) error {
	msg := &protocol.RegisterMessage{
		WorkerID:     c.config.WorkerID,
		Mode:         "remote",
		Platform:     detectPlatform(),
		Version:      "1.0.0",
		Capabilities: c.config.Capabilities,
	}

	// TODO: Implement actual WebSocket registration
	data, _ := json.Marshal(msg)
	fmt.Printf("[Worker SDK] Registering: %s\n", string(data))
	c.connID = fmt.Sprintf("conn-%d", time.Now().Unix())
	return nil
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
	msg := &protocol.HeartbeatMessage{
		WorkerID:  c.config.WorkerID,
		Timestamp: time.Now().Unix(),
	}
	data, _ := json.Marshal(msg)
	fmt.Printf("[Worker SDK] Heartbeat: %s\n", string(data))
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
	c.wg.Wait()
	c.setState(protocol.StateDisconnected)
	return nil
}

// Close closes the client
func (c *Client) Close() error {
	return c.Disconnect()
}

func detectPlatform() string {
	return "unknown"
}
