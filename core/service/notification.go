package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/template"
	"github.com/google/uuid"
)

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
// Returns the list of created task IDs.
func (s *NotificationService) Process(ctx context.Context, n *core.Notification, queue core.Queue) ([]string, error) {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	n.CreatedAt = time.Now()

	// Dedup check
	if s.dedup != nil && s.dedup.Check(n.ID, n.Channels, n.Params) {
		return nil, nil
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
		// Template level as fallback
		if n.Level == "" && renderedData.Level != "" {
			n.Level = renderedData.Level
		}
	}

	// Generate DeliveryTasks for each channel
	var taskIDs []string
	for _, channel := range channels {
		provider, err := s.runtime.GetProvider(channel)
		if err != nil {
			continue
		}

		// Resolve targets for this channel
		targets := resolveTargets(n, channel)

		task, err := s.planner.Plan(ctx, provider, n, renderedData, targets, channel)
		if err != nil {
			continue
		}

		if err := queue.Push(ctx, task); err != nil {
			return taskIDs, fmt.Errorf("queue push: %w", err)
		}

		taskIDs = append(taskIDs, task.ID)
	}

	return taskIDs, nil
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
