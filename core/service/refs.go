package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/groups"
)

// deliveryTarget is one concrete delivery after reference expansion: a
// provider instance and optionally pinned recipients (group members may
// pin them; nil falls back to the notification's own recipient list).
type deliveryTarget struct {
	channel string
	targets []string
}

// expandRefs resolves a batch of channel references into concrete
// delivery targets. It is the pure decision step of routing: it never
// touches the queue, so Routing (decide who gets what) stays separable
// from Delivery (Plan, Push, retry). A reference that fails to resolve
// is recorded as a per-channel failure and the rest still expand — no
// single bad reference takes the notification down.
func (s *NotificationService) expandRefs(refs []string) ([]deliveryTarget, []ChannelError) {
	targets := make([]deliveryTarget, 0, len(refs))
	var failed []ChannelError
	for _, ref := range refs {
		expanded, err := s.expandRef(ref)
		if err != nil {
			// An unknown group (or a group reference with no resolver) is
			// configuration drift: that reference fails, visibly, and the
			// remaining channels still go out.
			failed = append(failed, ChannelError{Channel: ref, Error: err.Error()})
			continue
		}
		targets = append(targets, expanded...)
	}
	return targets, failed
}

// expandRef resolves one channel reference. Plain names resolve by
// priority: an explicit provider instance wins over the channels block
// (channel 显式 > channels 块); a name in neither stays a literal
// provider target whose miss fails that channel at delivery time, as
// before. The routes table sits below both — it only routes
// notifications that named no channels at all. "group:" references
// expand to the group's members, each member channel resolving through
// the same priority (an explicit provider instance still wins; a block
// key fans out to its providers); "user:" references expand to their
// endpoints, merged per provider instance.
func (s *NotificationService) expandRef(ref string) ([]deliveryTarget, error) {
	if groups.IsRef(ref) {
		if s.groups == nil {
			return nil, fmt.Errorf("group reference %q but no group resolver is configured", ref)
		}
		members, ok := s.groups.ExpandGroup(strings.TrimPrefix(ref, groups.RefPrefix))
		if !ok {
			return nil, fmt.Errorf("unknown group %q", strings.TrimPrefix(ref, groups.RefPrefix))
		}
		out := make([]deliveryTarget, 0, len(members))
		for _, m := range members {
			for _, p := range s.resolveChannel(m.Channel) {
				out = append(out, deliveryTarget{channel: p, targets: m.Recipients})
			}
		}
		return out, nil
	}
	if audience.IsUserRef(ref) {
		if s.users == nil {
			return nil, fmt.Errorf("user reference %q but no user resolver is configured", ref)
		}
		name := strings.TrimPrefix(ref, audience.UserPrefix)
		eps, ok := s.users.ExpandUser(name)
		if !ok {
			return nil, fmt.Errorf("unknown user %q", name)
		}
		return mergeUserEndpoints(eps), nil
	}
	out := make([]deliveryTarget, 0, 1)
	for _, p := range s.resolveChannel(ref) {
		out = append(out, deliveryTarget{channel: p})
	}
	return out, nil
}

// resolveChannel resolves a channel name to concrete provider instances by
// the same priority everywhere: an explicit provider instance wins over
// the channels block; a block name expands to its providers in configured
// order; a name in neither stays literal (delivery time decides).
func (s *NotificationService) resolveChannel(ref string) []string {
	if _, err := s.runtime.GetProvider(ref); err != nil && s.channels != nil {
		if providers, ok := s.channels.ExpandChannel(ref); ok {
			return providers
		}
	}
	return []string{ref}
}

// mergeUserEndpoints folds endpoints onto their provider instance so a
// user fan-out is one task per provider (all targets bundled in it),
// never one task per endpoint — several recipients sharing an instance
// still produce a single delivery. Deterministic order keeps tasks and
// logs stable across runs.
func mergeUserEndpoints(eps []audience.Endpoint) []deliveryTarget {
	byType := make(map[string][]string)
	for _, ep := range eps {
		byType[ep.Type] = append(byType[ep.Type], ep.Target)
	}
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)
	out := make([]deliveryTarget, 0, len(types))
	for _, t := range types {
		out = append(out, deliveryTarget{channel: t, targets: byType[t]})
	}
	return out
}
