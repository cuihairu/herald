package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/runtime"
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

// NotificationService orchestrates the notification processing pipeline
type NotificationService struct {
	templates  *template.Manager
	router     *route.Router
	runtime    *runtime.Manager
	dedup      *dedup.Dedup
	planner    *DeliveryPlanner
}

// NewNotificationService creates a new NotificationService
func NewNotificationService(
	templates *template.Manager,
	router *route.Router,
	runtime *runtime.Manager,
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

// GetRuntime returns the runtime manager
func (s *NotificationService) GetRuntime() *runtime.Manager {
	return s.runtime
}

// formatChannelErrors formats channel errors into a single string
func formatChannelErrors(errs []ChannelError) string {
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, fmt.Sprintf("%s: %s", e.Channel, e.Error))
	}
	return fmt.Sprintf("%v", msgs)
}

// dedupKey generates a stable key from notification content
func dedupKey(n *core.Notification) string {
	h := sha256.New()
	h.Write([]byte(n.Type))
	h.Write([]byte(n.Level))
	h.Write([]byte(n.TemplateRef))
	for _, c := range n.Channels {
		h.Write([]byte(c))
	}
	for k, v := range n.Params {
		h.Write([]byte(k))
		h.Write([]byte(fmt.Sprintf("%v", v)))
	}
	if n.Content != nil {
		h.Write([]byte(n.Content.Title))
		h.Write([]byte(n.Content.Body))
	}
	return hex.EncodeToString(h.Sum(nil))
}
