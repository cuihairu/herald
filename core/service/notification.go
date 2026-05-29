package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/template"
	"github.com/google/uuid"
)

// ProcessResult holds the outcome of notification processing
type ProcessResult struct {
	NotificationID string
	TaskIDs        []string
	Accepted       []string // channels that succeeded
	Failed         []ChannelError
}

// ChannelError records a per-channel failure
type ChannelError struct {
	Channel string
	Error   string
}

// ProviderRuntime defines the runtime capabilities needed by NotificationService
type ProviderRuntime interface {
	GetProvider(name string) (core.Provider, error)
	IsEnabled(name string) bool
}

// NotificationService orchestrates the notification processing pipeline
type NotificationService struct {
	templates *template.Manager
	router    *route.Router
	runtime   ProviderRuntime
	dedup     *dedup.Dedup
	planner   *DeliveryPlanner
}

// NewNotificationService creates a new NotificationService
func NewNotificationService(
	templates *template.Manager,
	router *route.Router,
	runtime ProviderRuntime,
	dedup *dedup.Dedup,
) *NotificationService {
	return &NotificationService{
		templates: templates,
		router:    router,
		runtime:   runtime,
		dedup:     dedup,
		planner:   NewDeliveryPlanner(templates),
	}
}

// Process processes a Notification, generates DeliveryTasks, and enqueues them.
func (s *NotificationService) Process(ctx context.Context, n *core.Notification, queue core.Queue) (*ProcessResult, error) {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	n.CreatedAt = time.Now()

	// Dedup check — key derived from notification content, not auto-generated ID
	if s.dedup != nil && s.dedup.Check(dedupKey(n)) {
		return &ProcessResult{NotificationID: n.ID}, nil
	}

	// Resolve channels
	channels := n.Channels
	if len(channels) == 0 {
		routed, err := s.router.Route(n.Type, n.Level)
		if err != nil {
			return nil, fmt.Errorf("no route: %w", err)
		}
		channels = routed
	}

	// Render template if specified
	var renderedData *template.RenderedData
	if n.TemplateRef != "" {
		var err error
		renderedData, err = s.templates.Render(n.TemplateRef, n.Params)
		if err != nil {
			return nil, fmt.Errorf("template render: %w", err)
		}
		if n.Level == "" && renderedData.Level != "" {
			n.Level = renderedData.Level
		}
	}

	// Generate DeliveryTasks for each channel, collecting errors
	result := &ProcessResult{NotificationID: n.ID}
	for _, channel := range channels {
		provider, err := s.runtime.GetProvider(channel)
		if err != nil {
			result.Failed = append(result.Failed, ChannelError{Channel: channel, Error: err.Error()})
			continue
		}

		if !s.runtime.IsEnabled(channel) {
			result.Failed = append(result.Failed, ChannelError{Channel: channel, Error: "provider is disabled"})
			continue
		}

		targets := resolveTargets(n, channel)

		task, err := s.planner.Plan(ctx, provider, n, renderedData, targets, channel)
		if err != nil {
			result.Failed = append(result.Failed, ChannelError{Channel: channel, Error: err.Error()})
			continue
		}

		if err := queue.Push(ctx, task); err != nil {
			result.Failed = append(result.Failed, ChannelError{Channel: channel, Error: fmt.Sprintf("queue: %v", err)})
			continue
		}

		result.TaskIDs = append(result.TaskIDs, task.ID)
		result.Accepted = append(result.Accepted, channel)
	}

	// If zero tasks created, return error
	if len(result.TaskIDs) == 0 && len(result.Failed) > 0 {
		return result, fmt.Errorf("all channels failed: %s", formatChannelErrors(result.Failed))
	}

	return result, nil
}

// resolveTargets returns the target list for a given channel
func resolveTargets(n *core.Notification, channel string) []string {
	if n.Recipients != nil {
		if targets, ok := n.Recipients[channel]; ok && len(targets) > 0 {
			return targets
		}
	}
	return nil
}

// formatChannelErrors formats channel errors into a single string
func formatChannelErrors(errs []ChannelError) string {
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, fmt.Sprintf("%s: %s", e.Channel, e.Error))
	}
	return fmt.Sprintf("%v", msgs)
}

// dedupKey generates a stable, canonical key from notification content
func dedupKey(n *core.Notification) string {
	// Build a canonical structure with sorted keys
	channels := make([]string, len(n.Channels))
	copy(channels, n.Channels)
	sort.Strings(channels)

	key := struct {
		Type       string              `json:"type"`
		Level      string              `json:"level"`
		Template   string              `json:"template"`
		Channels   []string            `json:"channels"`
		Recipients map[string][]string `json:"recipients"`
		Params     map[string]any      `json:"params"`
		Title      string              `json:"title,omitempty"`
		Body       string              `json:"body,omitempty"`
	}{
		Type:       n.Type,
		Level:      n.Level,
		Template:   n.TemplateRef,
		Channels:   channels,
		Recipients: n.Recipients,
		Params:     n.Params,
	}
	if n.Content != nil {
		key.Title = n.Content.Title
		key.Body = n.Content.Body
	}

	// json.Marshal on struct fields is deterministic (field order follows struct definition)
	// map entries are NOT guaranteed sorted, so we rely on the struct field order
	data, _ := json.Marshal(key)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
