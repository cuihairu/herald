package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/cuihaitao/herald/core"
	"github.com/cuihaitao/herald/core/dedup"
	"github.com/cuihaitao/herald/core/route"
	"github.com/cuihaitao/herald/core/runtime"
	"github.com/cuihaitao/herald/core/websocket"
	"github.com/cuihaitao/herald/internal/logger"
)

// Server is the herald server
type Server struct {
	addr    string
	handler *Handler
	server  *http.Server

	router  *route.Router
	queue   core.Queue
	runtime *runtime.Manager
	dedup   *dedup.Dedup
}

// Config is the server configuration
type Config struct {
	Addr    string
	Timeout time.Duration

	Router  *route.Router
	Queue   core.Queue
	Runtime *runtime.Manager
	Dedup   *dedup.Dedup
}

// NewServer creates a new server
func NewServer(config *Config) *Server {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	handler := NewHandler(config.Router, config.Queue, config.Runtime, config.Dedup)

	s := &Server{
		addr:    config.Addr,
		handler: handler,
		router:  config.Router,
		queue:   config.Queue,
		runtime: config.Runtime,
		dedup:   config.Dedup,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/notify", s.handleNotify)
	mux.HandleFunc("/api/v1/events", s.handleEvents)
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/providers", s.handleProviders)
	mux.HandleFunc("/api/v1/workers", s.handleWorkers)
	mux.HandleFunc("/api/v1/queue", s.handleQueue)
	mux.HandleFunc("/api/v1/providers/", s.handleProviderAction)

	s.server = &http.Server{
		Addr:         config.Addr,
		Handler:      mux,
		ReadTimeout:  config.Timeout,
		WriteTimeout: config.Timeout,
	}

	return s
}

// Start starts the server
func (s *Server) Start(ctx context.Context) error {
	logger.Info("server starting", "addr", s.addr)

	// Start dispatcher
	go s.dispatch(ctx)

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

// Shutdown shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	logger.Info("server shutting down")
	return s.server.Shutdown(ctx)
}

func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handler.HandleNotify(w, r)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handler.HandleEvent(w, r)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handler.HandleStatus(w, r)
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handler.HandleProviders(w, r)
}

func (s *Server) handleWorkers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handler.HandleWorkers(w, r)
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handler.HandleQueue(w, r)
}

func (s *Server) handleProviderAction(w http.ResponseWriter, r *http.Request) {
	// Extract provider name from path like /api/v1/providers/{name}/enable or /api/v1/providers/{name}/disable
	path := r.URL.Path
	prefix := "/api/v1/providers/"

	if len(path) <= len(prefix) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	rest := path[len(prefix):]
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	name := parts[0]
	action := parts[1]

	switch action {
	case "enable":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handler.HandleEnableProviderWithName(w, r, name)
	case "disable":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handler.HandleDisableProviderWithName(w, r, name)
	default:
		http.Error(w, "invalid action", http.StatusBadRequest)
	}
}

// SetWebSocketServer sets the WebSocket server
func (s *Server) SetWebSocketServer(wsServer *websocket.Server) {
	s.handler.SetWebSocketServer(wsServer)
}

// dispatch processes events and tasks from the queue
func (s *Server) dispatch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		default:
			// Process events first
			event, err := s.queue.Pop(ctx)
			if err == nil {
				s.processEvent(ctx, event)
				continue
			}

			// Process tasks
			task, err := s.queue.PopTask(ctx)
			if err == nil {
				s.processTask(ctx, task)
				continue
			}

			// Small sleep to avoid busy loop
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (s *Server) processEvent(ctx context.Context, event *core.Event) {
	providers, err := s.router.Route(event)
	if err != nil {
		logger.Warn("no route for event", "type", event.Type, "error", err)
		return
	}

	for _, provider := range providers {
		task := &core.Task{
			ID:        event.ID,
			Provider:  provider,
			Title:     event.Type,
			Body:      "", // Will be filled from event data
			Level:     event.Labels["level"],
			Data:      event.Data,
			CreatedAt: event.Timestamp,
		}

		if err := s.queue.PushTask(ctx, task); err != nil {
			logger.Error("failed to push task", "error", err)
		}
	}
}

func (s *Server) processTask(ctx context.Context, task *core.Task) {
	err := s.runtime.Deliver(ctx, task)
	if err != nil {
		logger.Error("failed to deliver task", "task_id", task.ID, "provider", task.Provider, "error", err)
	} else {
		logger.Info("task delivered", "task_id", task.ID, "provider", task.Provider)
	}
}
