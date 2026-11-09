package service

import (
	"context"
	"fmt"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/feeds"
	"github.com/cuihairu/herald/core/template"
)

func (s *NotificationService) enqueue(ctx context.Context, n *core.Notification, channels []string, renderedData *template.RenderedData, result *ProcessResult) {
	// Routing decision first (pure), delivery execution after: the queue
	// is only touched by the execution half.
	targets, failed := s.expandRefs(channels)
	result.Failed = append(result.Failed, failed...)
	s.deliverTargets(ctx, n, targets, renderedData, result)
}

// deliverTargets is the execution half of the pipeline shared by every
// delivery path: rss-class channels project into the feed store instead
// of a provider task, everything else plans and pushes. Callers hand in
// already-expanded targets — enqueue expands references first; the app
// dispatch face resolves them from contact surfaces.
func (s *NotificationService) deliverTargets(ctx context.Context, n *core.Notification, targets []deliveryTarget, renderedData *template.RenderedData, result *ProcessResult) {
	projected := false
	for _, dt := range targets {
		if s.feeds != nil && audience.ClassifyChannel(dt.channel) == audience.ChannelRSS {
			// §9 pull channel: project once per notification — a mixed
			// rss+email fan-out both files the item and delivers the push
			// half; the pull half never becomes a provider task.
			if !projected {
				s.projectFeed(n, renderedData, result)
				projected = true
			}
			continue
		}
		s.enqueueOne(ctx, n, dt, renderedData, result)
	}
}

// projectFeed files one notification into the feed store as its pull
// projection (§9): the rendered (or direct) title, the direct body, the
// audience reference (empty marks 公开内容) and the notification id as
// the feed guid. A store that refuses the item lands in result.Failed —
// reported like a failed provider, not swallowed.
func (s *NotificationService) projectFeed(n *core.Notification, renderedData *template.RenderedData, result *ProcessResult) {
	title := directTitle(n)
	if renderedData != nil && renderedData.Title != "" {
		title = renderedData.Title
	}
	body := directBody(n)
	if err := s.feeds.Add(feeds.Item{
		ID:          n.ID,
		Category:    n.Type,
		Title:       title,
		Body:        body,
		AudienceID:  n.AudienceID,
		PublishedAt: n.CreatedAt,
	}); err != nil {
		result.Failed = append(result.Failed, ChannelError{Channel: "rss", Error: err.Error()})
	}
}

// enqueueOne plans and pushes the delivery to one concrete channel.
func (s *NotificationService) enqueueOne(ctx context.Context, n *core.Notification, dt deliveryTarget, renderedData *template.RenderedData, result *ProcessResult) {
	provider, err := s.runtime.GetProvider(dt.channel)
	if err != nil {
		result.Failed = append(result.Failed, ChannelError{Channel: dt.channel, Error: err.Error()})
		return
	}

	if !s.runtime.IsEnabled(dt.channel) {
		result.Failed = append(result.Failed, ChannelError{Channel: dt.channel, Error: "provider is disabled"})
		return
	}

	// A pinned target list (group member recipients) overrides the
	// notification's own; without one the notification decides.
	targets := dt.targets
	if len(targets) == 0 {
		targets = resolveTargets(n, dt.channel)
	}

	task, err := s.planner.Plan(ctx, provider, n, renderedData, targets, dt.channel)
	if err != nil {
		result.Failed = append(result.Failed, ChannelError{Channel: dt.channel, Error: err.Error()})
		return
	}

	if err := s.queue.Push(ctx, task); err != nil {
		result.Failed = append(result.Failed, ChannelError{Channel: dt.channel, Error: fmt.Sprintf("queue: %v", err)})
		return
	}

	result.TaskIDs = append(result.TaskIDs, task.ID)
	result.Accepted = append(result.Accepted, dt.channel)
}
