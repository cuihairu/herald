package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cuihairu/herald/internal/logger"
	"github.com/cuihairu/herald/protocol"
	"github.com/gorilla/websocket"
)

// Default upgrader
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

// ConnectionState represents a worker connection state
type ConnectionState struct {
	WorkerID      string
	Platform      string
	Version       string
	Capabilities  []string
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	Status        map[string]interface{}
	conn          *websocket.Conn
	mu            sync.RWMutex
}

// ConnHandler handles worker connections
type ConnHandler interface {
	// OnRegister is called when a worker registers
	OnRegister(workerID string, msg *protocol.RegisterMessage) error

	// OnTaskAck is called when a task is acknowledged
	OnTaskAck(taskID string, success bool, errMsg string) error

	// OnWorkerEvent is called when a worker sends an event
	OnWorkerEvent(workerID string, event *protocol.EventMessage) error

	// OnDisconnect is called when a worker disconnects
	OnDisconnect(workerID string)
}

// Server handles WebSocket connections from workers
type Server struct {
	addr    string
	handler ConnHandler
	server  *http.Server

	mu      sync.RWMutex
	workers map[string]*ConnectionState

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	readTimeout  time.Duration
	writeTimeout time.Duration
	pingTimeout  time.Duration
	pingInterval time.Duration
}

// Config is the WebSocket server configuration
type Config struct {
	Addr           string        // Listen address
	ReadTimeout    time.Duration // Read timeout
	WriteTimeout   time.Duration // Write timeout
	PingTimeout    time.Duration // Ping timeout
	PingInterval   time.Duration // Ping interval
	MaxMessageSize int64         // Max message size
}

// NewServer creates a new WebSocket server
func NewServer(config *Config, handler ConnHandler) *Server {
	if config == nil {
		config = &Config{
			Addr:         ":8081",
			ReadTimeout:  60 * time.Second,
			WriteTimeout: 60 * time.Second,
			PingTimeout:  30 * time.Second,
			PingInterval: 20 * time.Second,
		}
	}

	mux := http.NewServeMux()
	s := &Server{
		addr:         config.Addr,
		handler:      handler,
		workers:      make(map[string]*ConnectionState),
		readTimeout:  config.ReadTimeout,
		writeTimeout: config.WriteTimeout,
		pingTimeout:  config.PingTimeout,
		pingInterval: config.PingInterval,
	}

	mux.HandleFunc("/worker", s.handleWebSocket)

	s.server = &http.Server{
		Addr:    config.Addr,
		Handler: mux,
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel

	return s
}

// Start starts the WebSocket server
func (s *Server) Start() error {
	logger.Info("websocket server starting", "addr", s.addr)

	// Start stale worker checker
	s.wg.Add(1)
	go s.checkStaleWorkers()

	// Start HTTP server
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("websocket server error", "error", err)
		}
	}()

	return nil
}

// Stop stops the WebSocket server
func (s *Server) Stop() error {
	logger.Info("websocket server stopping")

	// Close all worker connections
	s.mu.Lock()
	for _, state := range s.workers {
		_ = state.conn.Close()
	}
	s.mu.Unlock()

	// Cancel context
	s.cancel()

	// Shutdown server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.server.Shutdown(ctx)

	// Wait for goroutines
	s.wg.Wait()

	return nil
}

// handleWebSocket handles a WebSocket connection
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("failed to upgrade websocket", "error", err)
		return
	}

	// Create connection state
	state := &ConnectionState{
		conn:        conn,
		ConnectedAt: time.Now(),
		Status:      make(map[string]interface{}),
	}

	// Set read/write deadlines
	if err := conn.SetReadDeadline(time.Now().Add(s.readTimeout)); err != nil {
		logger.Error("failed to set read deadline", "error", err)
		_ = conn.Close()
		return
	}

	// Set ping handler
	conn.SetPingHandler(func(appData string) error {
		if err := conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
			return err
		}
		return conn.WriteMessage(websocket.PongMessage, []byte(appData))
	})

	logger.Info("worker connected", "remote_addr", r.RemoteAddr)

	// Handle connection
	s.handleConnection(state)
}

// handleConnection handles messages from a connection
func (s *Server) handleConnection(state *ConnectionState) {
	s.wg.Add(1)
	defer s.wg.Done()

	conn := state.conn

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Error("websocket read error", "error", err)
			}

			// Handle disconnect
			if state.WorkerID != "" {
				s.handleDisconnect(state.WorkerID)
			}
			return
		}

		// Only accept text messages
		if messageType != websocket.TextMessage {
			continue
		}

		// Reset read deadline
		if err := conn.SetReadDeadline(time.Now().Add(s.readTimeout)); err != nil {
			logger.Error("failed to reset read deadline", "error", err)
			return
		}

		// Parse message
		if err := s.handleMessage(state, data); err != nil {
			logger.Error("failed to handle message", "error", err)
		}
	}
}

// handleMessage handles a message from a worker
func (s *Server) handleMessage(state *ConnectionState, data []byte) error {
	// Parse message type
	var raw struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse message type: %w", err)
	}

	switch raw.Type {
	case protocol.MessageTypeRegister:
		var msg protocol.RegisterMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return fmt.Errorf("failed to parse register message: %w", err)
		}
		return s.handleRegister(state, &msg)

	case protocol.MessageTypeHeartbeat:
		var msg protocol.HeartbeatMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return fmt.Errorf("failed to parse heartbeat message: %w", err)
		}
		return s.handleHeartbeat(&msg)

	case protocol.MessageTypeAck:
		var msg protocol.AckMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return fmt.Errorf("failed to parse ack message: %w", err)
		}
		return s.handleAck(&msg)

	case protocol.MessageTypeEvent:
		var msg protocol.EventMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return fmt.Errorf("failed to parse event message: %w", err)
		}
		return s.handleEvent(&msg)

	default:
		logger.Warn("unknown message type", "type", raw.Type)
		return nil
	}
}

// handleRegister handles a worker registration
func (s *Server) handleRegister(state *ConnectionState, msg *protocol.RegisterMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update state
	state.WorkerID = msg.WorkerID
	state.Platform = msg.Platform
	state.Version = msg.Version
	state.Capabilities = msg.Capabilities
	state.LastHeartbeat = time.Now()

	// Store connection
	s.workers[msg.WorkerID] = state

	logger.Info("worker registered",
		"worker_id", msg.WorkerID,
		"platform", msg.Platform,
		"version", msg.Version,
		"capabilities", msg.Capabilities,
	)

	// Send ACK
	ack := &protocol.RegisterAckMessage{
		WorkerID:  msg.WorkerID,
		Success:   true,
		ServerID:  "herald-core",
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(ack)
	if err != nil {
		return fmt.Errorf("failed to marshal ack: %w", err)
	}

	if err := state.conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
		return err
	}

	if err := state.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to send ack: %w", err)
	}

	// Notify handler
	if s.handler != nil {
		if err := s.handler.OnRegister(msg.WorkerID, msg); err != nil {
			logger.Error("handler onregister error", "error", err)
		}
	}

	return nil
}

// handleHeartbeat handles a heartbeat message
func (s *Server) handleHeartbeat(msg *protocol.HeartbeatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.workers[msg.WorkerID]
	if !ok {
		return fmt.Errorf("worker not found: %s", msg.WorkerID)
	}

	state.LastHeartbeat = time.Now()
	if msg.Status != nil {
		state.Status = msg.Status
	}

	return nil
}

// handleAck handles a task acknowledgment
func (s *Server) handleAck(msg *protocol.AckMessage) error {
	logger.Info("task acknowledged",
		"task_id", msg.TaskID,
		"success", msg.Success,
		"error", msg.Error,
	)

	if s.handler != nil {
		if err := s.handler.OnTaskAck(msg.TaskID, msg.Success, msg.Error); err != nil {
			logger.Error("handler ontaskack error", "error", err)
		}
	}

	return nil
}

// handleEvent handles a worker event
func (s *Server) handleEvent(msg *protocol.EventMessage) error {
	logger.Info("worker event",
		"worker_id", msg.WorkerID,
		"event_type", msg.EventType,
	)

	if s.handler != nil {
		if err := s.handler.OnWorkerEvent(msg.WorkerID, msg); err != nil {
			logger.Error("handler onevent error", "error", err)
		}
	}

	return nil
}

// handleDisconnect handles a worker disconnection
func (s *Server) handleDisconnect(workerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.workers[workerID]
	if !ok {
		return
	}

	// Close connection
	_ = state.conn.Close()

	// Remove from workers
	delete(s.workers, workerID)

	logger.Info("worker disconnected", "worker_id", workerID)

	// Notify handler
	if s.handler != nil {
		s.handler.OnDisconnect(workerID)
	}
}

// checkStaleWorkers periodically checks for stale workers
func (s *Server) checkStaleWorkers() {
	defer s.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for workerID, state := range s.workers {
				if now.Sub(state.LastHeartbeat) > 2*time.Minute {
					logger.Warn("worker stale, closing connection",
						"worker_id", workerID,
						"last_heartbeat", state.LastHeartbeat,
					)
					_ = state.conn.Close()
					delete(s.workers, workerID)

					if s.handler != nil {
						s.handler.OnDisconnect(workerID)
					}
				}
			}
			s.mu.Unlock()
		}
	}
}

// DispatchTask dispatches a task to a worker
func (s *Server) DispatchTask(workerID string, task *protocol.DispatchMessage) error {
	s.mu.RLock()
	state, ok := s.workers[workerID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("worker not found: %s", workerID)
	}

	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if err := state.conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
		return err
	}

	if err := state.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to send task: %w", err)
	}

	logger.Info("task dispatched",
		"worker_id", workerID,
		"task_id", task.TaskID,
		"provider", task.Provider,
	)

	return nil
}

// GetWorkers returns all connected workers
func (s *Server) GetWorkers() map[string]*ConnectionState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*ConnectionState, len(s.workers))
	for k, v := range s.workers {
		result[k] = v
	}

	return result
}

// GetWorker returns a specific worker state
func (s *Server) GetWorker(workerID string) (*ConnectionState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.workers[workerID]
	if !ok {
		return nil, fmt.Errorf("worker not found: %s", workerID)
	}

	return state, nil
}

// GetWorkerCount returns the number of connected workers
func (s *Server) GetWorkerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.workers)
}

// DisconnectWorker disconnects a worker
func (s *Server) DisconnectWorker(workerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.workers[workerID]
	if !ok {
		return fmt.Errorf("worker not found: %s", workerID)
	}

	_ = state.conn.Close()
	delete(s.workers, workerID)

	logger.Info("worker disconnected", "worker_id", workerID, "reason", "requested")

	if s.handler != nil {
		s.handler.OnDisconnect(workerID)
	}

	return nil
}
