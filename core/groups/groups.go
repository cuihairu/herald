// Package groups implements notification groups: named delivery audiences
// that any channel reference can name via the "group:" prefix and that
// expand to their members at delivery planning. See
// docs/design-notification-groups.md for the design rationale.
package groups

import (
	"fmt"
	"regexp"
	"strings"
)

// RefPrefix marks a channel reference as a group: `group:ops-oncall` names
// the group "ops-oncall". The explicit category word avoids the @-mention
// ambiguity and cannot collide with provider instance names.
const RefPrefix = "group:"

// Ref builds the channel reference for a group id.
func Ref(id string) string {
	return RefPrefix + id
}

// IsRef reports whether a channel reference names a group.
func IsRef(ref string) bool {
	return strings.HasPrefix(ref, RefPrefix)
}

// Member is one delivery target inside a group: a provider instance with
// optionally pinned recipients. Pinned recipients override the
// notification's own recipient list for that channel — the group's
// audience is the group's decision.
type Member struct {
	// Channel is the provider instance name (never another group
	// reference: groups do not nest, expansion is single-level).
	Channel string `json:"channel" yaml:"channel"`
	// Recipients optionally pins the target list for this channel (phone
	// numbers, chat ids, addresses — whatever the provider expects).
	Recipients []string `json:"recipients,omitempty" yaml:"recipients,omitempty"`
}

// Group is a named audience: id, an optional description and the member
// list every `group:id` reference expands to.
type Group struct {
	ID          string   `json:"id" yaml:"id"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Members     []Member `json:"members" yaml:"members"`
}

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// Bounds: a group is "a team", not a directory; larger lists almost
// always want a real identity system feeding members in via the API.
const (
	maxMembers          = 64
	maxRecipients       = 64
	maxDescriptionChars = 512
	maxNameChars        = 64
)

// Normalize trims the id.
func (g *Group) Normalize() {
	g.ID = strings.TrimSpace(g.ID)
}

// Validate checks structural hygiene: bounded id and description, a
// non-empty bounded member list of well-formed channels (no duplicates —
// one member per channel, multiple targets go in Recipients) and bounded
// non-blank recipient entries.
func (g Group) Validate() error {
	if !idPattern.MatchString(g.ID) {
		return fmt.Errorf("groups: invalid group id %q (want 1-64 chars of letters, digits, dot, dash, underscore)", g.ID)
	}
	if len(g.Description) > maxDescriptionChars {
		return fmt.Errorf("groups: group %q: description is %d chars, max is %d", g.ID, len(g.Description), maxDescriptionChars)
	}
	if len(g.Members) == 0 {
		return fmt.Errorf("groups: group %q: members must list at least one entry", g.ID)
	}
	if len(g.Members) > maxMembers {
		return fmt.Errorf("groups: group %q: %d members, max is %d", g.ID, len(g.Members), maxMembers)
	}
	seen := make(map[string]bool, len(g.Members))
	for i, m := range g.Members {
		channel := strings.TrimSpace(m.Channel)
		if channel == "" || len(channel) > maxNameChars {
			return fmt.Errorf("groups: group %q: member %d channel must be 1-64 chars", g.ID, i)
		}
		if strings.HasPrefix(channel, RefPrefix) {
			return fmt.Errorf("groups: group %q: member %d names a group (%q): groups do not nest", g.ID, i, m.Channel)
		}
		if seen[channel] {
			// Two members on one channel is the classic half-a-roster
			// accident; a channel with several targets is one member with
			// a recipients list.
			return fmt.Errorf("groups: group %q: duplicate member channel %q (use recipients for several targets)", g.ID, m.Channel)
		}
		seen[channel] = true
		if len(m.Recipients) > maxRecipients {
			return fmt.Errorf("groups: group %q: member %q has %d recipients, max is %d", g.ID, m.Channel, len(m.Recipients), maxRecipients)
		}
		for j, rc := range m.Recipients {
			if strings.TrimSpace(rc) == "" {
				return fmt.Errorf("groups: group %q: member %q recipient %d is empty", g.ID, m.Channel, j)
			}
		}
	}
	return nil
}
