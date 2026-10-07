package api

import (
	"context"
	"net/http"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/ack"
	"github.com/cuihairu/herald/core/apps"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/auth"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/digest"
	"github.com/cuihairu/herald/core/escalation"
	"github.com/cuihairu/herald/core/feeds"
	"github.com/cuihairu/herald/core/groups"
	"github.com/cuihairu/herald/core/incident"
	"github.com/cuihairu/herald/core/roster"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/rules"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/service"
	"github.com/cuihairu/herald/core/template"
	"github.com/cuihairu/herald/core/websocket"
	"github.com/cuihairu/herald/core/worker"
	"github.com/cuihairu/herald/internal/logger"
)

// Server is the herald server
type Server struct {
	addr    string
	handler *Handler
	server  *http.Server
	auth    *auth.Auth
	// apps is the §13.1 integration-namespace registry; nil keeps the
	// whole app face 404 (not configured = closed).
	apps *apps.Registry
	// notificationSvc is kept for the digest flush face: the §10 flip
	// loop calls back through FlushDigestBatch per flipped window.
	notificationSvc *service.NotificationService
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
	WorkerRegistry  *worker.Registry
	Rules           *rules.Engine   // optional; nil keeps static routing only
	Groups          *groups.Manager // optional; nil keeps group endpoints off
	// Users carries the user-level audience tables (core/audience);
	// optional, nil keeps "user:" references unresolvable.
	Users *audience.Manager
	// Rosters supplies the silence schedule placeholder: nil keeps roster
	// endpoints off and every roster-named silence gate open (fail open).
	Rosters    *roster.Manager
	AckStore   ack.Store           // optional; nil keeps alert endpoints off
	Escalation *escalation.Manager // optional; enables ack-cancel of upgrades
	Incidents  *incident.Store     // optional; nil keeps incident endpoints off
	// CardCallbackEncryptKey verifies Feishu interactive-card callbacks
	// (POST /api/v1/callbacks/feishu). Empty keeps encrypted callbacks
	// rejected; the endpoint itself needs an AckStore to be useful.
	CardCallbackEncryptKey string
	// Digest wires the §10 aggregator: folding preferences collect events
	// into windows instead of delivering them. nil keeps every event
	// direct. DigestPrefs resolves per-audience preferences; nil falls
	// back to the category default table.
	Digest      *digest.Aggregator
	DigestPrefs *audience.PreferenceRegistry
	// Feeds wires the §9 RSS pull channel (边界审计 §4): the store the
	// notification service projects rss-classified channels into and the
	// endpoints that render it. nil keeps the pull half off — the rss
	// channel then behaves as an unknown provider. FeedSurfaces resolves
	// private feed tokens to audiences; FeedRelations is the read-time
	// subscription table a private feed consults per pull.
	Feeds         *feeds.Store
	FeedMeta      feeds.ChannelMeta
	FeedSurfaces  *audience.SurfaceRegistry
	FeedRelations *audience.Registry
	// Sources wires the §8 来源适配器: the registry mutator behind the
	// bot / MP / in-app entries, its surface registry for identity
	// resolution, and the per-entry configs (empty secret/token keeps
	// that endpoint 404). nil keeps every source entry off.
	Sources        *audience.SourceAdapter
	SourceSurfaces *audience.SurfaceRegistry
	SourceBot      BotSourceConfig
	SourceWeChatMP WeChatMPSourceConfig
	// Delivery is the §6 strategy件 (channel intensity × message
	// urgency × mode) built from the delivery: config block. The
	// trigger face (expandRef/category dimension) wires it into the
	// pipeline in a later batch; until then it is validated at
	// startup and held here for that face.
	Delivery *audience.DeliveryPolicy
	// Apps seeds the §13.1 集成者接入面 (namespaced app tokens with
	// 分级权限). nil keeps every /api/v1/apps/** endpoint 404.
	Apps *apps.Registry
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
		config.Queue,
	)
	// Digest aggregator (§10): when wired, folding preferences collect
	// events into windows instead of delivering them; nil keeps every
	// event direct.
	if config.Digest != nil {
		notificationSvc.SetDigest(config.Digest, config.DigestPrefs)
	}
	if config.Rules != nil {
		notificationSvc.SetRuleEngine(config.Rules)
		// The runtime manager observes shadow hits into the delivery log.
		notificationSvc.SetRuleObserver(config.Runtime)
		// The silence gate consults the roster read face for rules whose
		// silence names a roster; nil (no rosters configured) keeps those
		// gates open.
		config.Rules.SetRosterSource(config.Rosters)
	}

	handler := NewHandler(notificationSvc, config.Runtime, config.TemplateManager)
	handler.SetQueue(config.Queue)
	handler.SetWorkerRegistry(config.WorkerRegistry)
	if config.Rules != nil {
		handler.SetRuleEngine(config.Rules)
	}
	if config.Groups != nil {
		handler.SetGroupManager(config.Groups)
		// Group references in any channel list resolve through the
		// manager's live table.
		notificationSvc.SetGroupResolver(config.Groups.Resolver())
	}
	if config.Users != nil {
		// "user:" references in any channel list resolve through the
		// manager's audience/recipient tables.
		notificationSvc.SetUserResolver(config.Users)
	}
	// Plain channel names that are not provider instances resolve through
	// the router's channels block (an explicit provider always wins).
	notificationSvc.SetChannelResolver(config.Router)
	if config.Rosters != nil {
		handler.SetRosterManager(config.Rosters)
	}
	if config.AckStore != nil {
		handler.SetAckStore(config.AckStore)
	}
	if config.Escalation != nil {
		handler.SetEscalationManager(config.Escalation)
		// The manager arms upgrades for routed rule events; the service
		// delivers them when one fires. An ack arriving through the API
		// cancels via the handler's reference to the same manager.
		notificationSvc.SetEscalationScheduler(config.Escalation)
		config.Escalation.SetNotifier(notificationSvc)
	}
	if config.Incidents != nil {
		handler.SetIncidentStore(config.Incidents)
		notificationSvc.SetIncidentStore(config.Incidents)
		// The engine reports recovered alert groups (fired, match stopped
		// holding); the service closes the episode and delivers the
		// recovery summary on the episode's original channels.
		if config.Rules != nil {
			config.Rules.SetResolvedFunc(notificationSvc.HandleResolved)
		}
	}
	if config.CardCallbackEncryptKey != "" {
		handler.SetCardCallbackKey(config.CardCallbackEncryptKey)
	}
	// RSS pull channel (§9): the service projects rss-classified channels
	// into the store; the feed endpoints render it back out. nil keeps
	// the pull half off entirely.
	if config.Feeds != nil {
		notificationSvc.SetFeeds(config.Feeds)
		handler.SetFeeds(config.Feeds, config.FeedMeta, config.FeedSurfaces, config.FeedRelations)
	}
	// Source adapters (§8): platform entries converge external actions
	// (bot /start /stop, MP follow events, in-app checkboxes) onto the
	// registries. nil keeps every entry off.
	if config.Sources != nil {
		handler.SetSources(config.Sources, config.SourceSurfaces, config.SourceBot, config.SourceWeChatMP)
	}

	s := &Server{
		addr:            config.Addr,
		handler:         handler,
		auth:            config.Auth,
		apps:            config.Apps,
		notificationSvc: notificationSvc,
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

	// Rule management
	mux.HandleFunc("/api/v1/rules", s.withAuth(s.handleRules))
	mux.HandleFunc("/api/v1/rules/{id}", s.withAuth(s.handleRuleByID))
	mux.HandleFunc("/api/v1/groups", s.withAuth(s.handleGroups))
	mux.HandleFunc("/api/v1/groups/{id}", s.withAuth(s.handleGroupByID))
	mux.HandleFunc("/api/v1/rosters", s.withAuth(s.handleRosters))
	mux.HandleFunc("/api/v1/rosters/{id}", s.withAuth(s.handleRosterByID))
	mux.HandleFunc("/api/v1/alerts/{id}", s.withAuth(s.handleAlertByID))
	mux.HandleFunc("/api/v1/alerts/{id}/ack", s.withAuth(s.handleAlertAck))
	// Feishu's servers call this endpoint — it authenticates with the
	// card-callback encryption key, not with the API token.
	mux.HandleFunc("/api/v1/callbacks/feishu", s.handler.HandleFeishuCallback)

	// Incident ledger
	mux.HandleFunc("/api/v1/incidents", s.withAuth(s.handleIncidents))
	mux.HandleFunc("/api/v1/incidents/{id}", s.withAuth(s.handleIncidentByID))

	// RSS pull channel (§9): readers pull on their own cadence with no
	// auth headers, so the private feed's URL token IS the credential —
	// the same trust shape as the unauthenticated bot callbacks.
	mux.HandleFunc("/feeds/{name}", s.handler.HandleFeed)
	mux.HandleFunc("/feeds/private/{name}", s.handler.HandlePrivateFeed)

	// §13.1 集成者接入面: app-token auth (分级权限), independent of the
	// operator API key. The namespace rides in the path.
	mux.HandleFunc("/api/v1/apps/{app}", s.withApp(apps.ScopeQuery, s.handleAppShow))
	mux.HandleFunc("/api/v1/apps/{app}/categories", s.handleAppCategories)
	// §13.2 policy overrides: each family PUTs its own shape, the
	// aggregate GET reads the whole set back.
	mux.HandleFunc("/api/v1/apps/{app}/policies", s.handleAppPoliciesRead)
	mux.HandleFunc("/api/v1/apps/{app}/policies/intensity", s.handleAppPoliciesIntensity)
	mux.HandleFunc("/api/v1/apps/{app}/policies/delivery-mode", s.handleAppPoliciesMode)
	mux.HandleFunc("/api/v1/apps/{app}/policies/escalation", s.handleAppPoliciesEscalation)
	mux.HandleFunc("/api/v1/apps/{app}/policies/dedup", s.handleAppPoliciesDedup)
	// §13.2 模板注册: namespace-scoped templates with per-channel
	// bindings; rendering stays inside the namespace.
	mux.HandleFunc("/api/v1/apps/{app}/templates", s.handleAppTemplates)
	mux.HandleFunc("/api/v1/apps/{app}/templates/{id}", s.handleAppTemplateByID)

	// §8 source entries: platform-vouched callbacks authenticate with
	// their shared secrets, the in-app checkbox face sits behind the
	// API token like the rest of the operator surface.
	mux.HandleFunc("/api/v1/callbacks/bot", s.handler.HandleBotCallback)
	mux.HandleFunc("/api/v1/callbacks/wechat-mp", s.handler.HandleWeChatMPCallback)
	mux.HandleFunc("/api/v1/audiences/{id}/subscriptions", s.withAuth(s.handler.HandleSubscriptions))

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

// Rule management handlers

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRules(w, r)
}

func (s *Server) handleRuleByID(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRuleByID(w, r)
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGroups(w, r)
}

func (s *Server) handleGroupByID(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleGroupByID(w, r)
}

func (s *Server) handleRosters(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRosters(w, r)
}

func (s *Server) handleRosterByID(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleRosterByID(w, r)
}

// Alert acknowledgement handlers

func (s *Server) handleAlertByID(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleAlertByID(w, r)
}

func (s *Server) handleAlertAck(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleAlertAck(w, r)
}

func (s *Server) handleIncidents(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleIncidents(w, r)
}

// SetCardCallbackKey sets the Feishu card-callback encryption key on the
// underlying handler.
func (s *Server) SetCardCallbackKey(key string) {
	s.handler.SetCardCallbackKey(key)
}

// FlushDigestBatch flushes one flipped digest window through the
// pipeline — the digest flip loop's callback face (§10 定时器).
func (s *Server) FlushDigestBatch(b digest.Batch) error {
	return s.notificationSvc.FlushDigestBatch(b)
}

func (s *Server) handleIncidentByID(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleIncidentByID(w, r)
}
