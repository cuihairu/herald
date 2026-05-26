package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/auth"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/template"
	"github.com/cuihairu/herald/core/websocket"
	"github.com/cuihairu/herald/internal/logger"
)

// Server is the herald server
type Server struct {
	addr    string
	handler *Handler
	server  *http.Server
	auth    *auth.Auth

	router  *route.Router
	queue   core.Queue
	runtime *runtime.Manager
	dedup   *dedup.Dedup
}

// Config is the server configuration
type Config struct {
	Addr    string
	Timeout time.Duration

	Router          *route.Router
	Queue           core.Queue
	Runtime         *runtime.Manager
	Dedup           *dedup.Dedup
	Auth            *auth.Auth
	TemplateManager *template.Manager
}

// NewServer creates a new server
func NewServer(config *Config) *Server {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	handler := NewHandler(config.Router, config.Queue, config.Runtime, config.Dedup, config.TemplateManager)

	s := &Server{
		addr:    config.Addr,
		handler: handler,
		auth:    config.Auth,
		router:  config.Router,
		queue:   config.Queue,
		runtime: config.Runtime,
		dedup:   config.Dedup,
	}

	mux := http.NewServeMux()

	// Public endpoints (no auth required)
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/auth/login", s.auth.HandleLogin)
	mux.HandleFunc("/api/v1/auth/refresh", s.auth.HandleRefresh)
	mux.HandleFunc("/api/v1/auth/me", s.auth.HandleMe)

	// Protected endpoints (auth required if enabled)
	mux.HandleFunc("/api/v1/notify", s.withAuth(s.handleNotify))
	mux.HandleFunc("/api/v1/events", s.withAuth(s.handleEvents))
	mux.HandleFunc("/api/v1/providers", s.withAuth(s.handleProviders))
	mux.HandleFunc("/api/v1/workers", s.withAuth(s.handleWorkers))
	mux.HandleFunc("/api/v1/queue", s.withAuth(s.handleQueue))
	mux.HandleFunc("/api/v1/providers/", s.withAuth(s.handleProviderAction))
	mux.HandleFunc("/api/v1/logs", s.withAuth(s.handleLogs))
	mux.HandleFunc("/api/v1/logs/stats", s.withAuth(s.handleLogsStats))
	mux.HandleFunc("/api/v1/logs/", s.withAuth(s.handleLogByID))
	mux.HandleFunc("/api/v1/config/", s.withAuth(s.handleProviderConfig))

	// Template management endpoints
	mux.HandleFunc("/api/v1/templates", s.withAuth(s.handleTemplates))
	mux.HandleFunc("/api/v1/templates/create", s.withAuth(s.handleCreateTemplate))
	mux.HandleFunc("/api/v1/templates/", s.withAuth(s.handleTemplateByID))

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

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handler.HandleLogs(w, r)
}

func (s *Server) handleLogsStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handler.HandleLogsStats(w, r)
}

func (s *Server) handleLogByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handler.HandleLogByID(w, r)
}

func (s *Server) handleProviderConfig(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	prefix := "/api/v1/config/"

	if len(path) <= len(prefix) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	rest := path[len(prefix):]
	parts := strings.Split(rest, "/")
	if len(parts) < 1 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	name := parts[0]

	switch r.Method {
	case http.MethodGet:
		s.handler.HandleProviderConfig(w, r, name)
	case http.MethodPut, http.MethodPost:
		s.handler.HandleUpdateProviderConfig(w, r, name)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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

// withAuth wraps a handler with authentication middleware
func (s *Server) withAuth(fn http.HandlerFunc) http.HandlerFunc {
	if s.auth == nil || !s.auth.IsEnabled() {
		return fn
	}
	return s.auth.Middleware(fn).ServeHTTP
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

// Template management handlers

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleTemplates(w, r)
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleCreateTemplate(w, r)
}

func (s *Server) handleTemplateByID(w http.ResponseWriter, r *http.Request) {
	// Extract template ID from path like /api/v1/templates/{id}
	path := r.URL.Path
	prefix := "/api/v1/templates/"

	if len(path) <= len(prefix) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	_ = path[len(prefix):] // ID is extracted by handler from PathValue
	s.handler.HandleTemplateByID(w, r)
}
