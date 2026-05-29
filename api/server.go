package api

import (
	"context"
	"net/http"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/auth"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/service"
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

	notificationSvc := service.NewNotificationService(
		config.TemplateManager,
		config.Router,
		config.Runtime,
		config.Dedup,
	)

	handler := NewHandler(notificationSvc, config.Runtime, config.TemplateManager)
	handler.SetQueue(config.Queue)

	s := &Server{
		addr:    config.Addr,
		handler: handler,
		auth:    config.Auth,
	}

	mux := http.NewServeMux()

	// Public endpoints
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/auth/login", s.auth.HandleLogin)
	mux.HandleFunc("/api/v1/auth/refresh", s.auth.HandleRefresh)
	mux.HandleFunc("/api/v1/auth/me", s.auth.HandleMe)

	// Protected endpoints
	mux.HandleFunc("/api/v1/notify", s.withAuth(s.handleNotify))
	mux.HandleFunc("/api/v1/providers", s.withAuth(s.handleProviders))
	mux.HandleFunc("/api/v1/workers", s.withAuth(s.handleWorkers))
	mux.HandleFunc("/api/v1/queue", s.withAuth(s.handleQueue))
	mux.HandleFunc("/api/v1/providers/{name}/enable", s.withAuth(s.handleProviderEnable))
	mux.HandleFunc("/api/v1/providers/{name}/disable", s.withAuth(s.handleProviderDisable))
	mux.HandleFunc("/api/v1/logs", s.withAuth(s.handleLogs))
	mux.HandleFunc("/api/v1/logs/stats", s.withAuth(s.handleLogsStats))
	mux.HandleFunc("/api/v1/logs/{id}", s.withAuth(s.handleLogByID))
	mux.HandleFunc("/api/v1/config/{name}", s.withAuth(s.handleProviderConfig))

	// Template management
	mux.HandleFunc("/api/v1/templates", s.withAuth(s.handleTemplates))
	mux.HandleFunc("/api/v1/templates/create", s.withAuth(s.handleCreateTemplate))
	mux.HandleFunc("/api/v1/templates/{id}", s.withAuth(s.handleTemplateByID))

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

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

// SetWebSocketServer sets the websocket server for worker endpoints.
func (s *Server) SetWebSocketServer(wsServer *websocket.Server) {
	s.handler.SetWebSocketServer(wsServer)
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
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "provider name is required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handler.HandleProviderConfig(w, r, name)
	case http.MethodPut, http.MethodPost:
		s.handler.HandleUpdateProviderConfig(w, r, name)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleProviderEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "provider name is required", http.StatusBadRequest)
		return
	}
	s.handler.HandleEnableProviderWithName(w, r, name)
}

func (s *Server) handleProviderDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "provider name is required", http.StatusBadRequest)
		return
	}
	s.handler.HandleDisableProviderWithName(w, r, name)
}

// withAuth wraps a handler with authentication middleware
func (s *Server) withAuth(fn http.HandlerFunc) http.HandlerFunc {
	if s.auth == nil || !s.auth.IsEnabled() {
		return fn
	}
	return s.auth.Middleware(fn).ServeHTTP
}

// Template management handlers

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleTemplates(w, r)
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleCreateTemplate(w, r)
}

func (s *Server) handleTemplateByID(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleTemplateByID(w, r)
}
