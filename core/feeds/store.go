// Package feeds is the §9 RSS pull channel: delivery records projected
// into per-category and per-audience feeds that readers pull on their
// own cadence. Herald never pushes here — RSS is a pull channel (边界
// 审计 §4), not a provider; the store holds items, the api package
// renders and serves them, and private-feed visibility is decided at
// read time from the audience's live subscription relations.
package feeds

import (
	"fmt"
	"sync"
	"time"
)

// DefaultLimit is the per-category and per-audience item retention when
// a store is built without an explicit cap.
const DefaultLimit = 500

// Item is one feed entry — the pull projection of one notification.
// An empty AudienceID marks 公开内容 (public broadcast); a named
// audience marks personal content that only its private feed may carry.
type Item struct {
	ID          string // notification id; also the feed guid
	Category    string
	Title       string
	Body        string
	AudienceID  string // empty = 公开内容
	PublishedAt time.Time
}

// Store holds feed items per category (FIFO capped), answering the two
// §9 feed shapes: the public per-category feeds and the personal feed.
type Store struct {
	mu    sync.RWMutex
	logs  map[string][]Item // category -> items, oldest first
	limit int
}

// NewStore builds a store keeping up to limit items per category log
// (DefaultLimit when limit <= 0).
func NewStore(limit int) *Store {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return &Store{logs: make(map[string][]Item), limit: limit}
}

// Add projects one notification into its category log. Items need an
// id, a category and a title — a projection missing any of them is a
// programming error and is refused rather than rendered as an empty
// feed entry.
func (s *Store) Add(it Item) error {
	if it.ID == "" {
		return fmt.Errorf("feeds: item id cannot be empty")
	}
	if it.Category == "" {
		return fmt.Errorf("feeds: item category cannot be empty")
	}
	if it.Title == "" {
		return fmt.Errorf("feeds: item title cannot be empty")
	}
	if it.PublishedAt.IsZero() {
		it.PublishedAt = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	log := append(s.logs[it.Category], it)
	if len(log) > s.limit {
		log = log[len(log)-s.limit:]
	}
	s.logs[it.Category] = log
	return nil
}

// Public returns the category's 公开内容, newest first: items projected
// without an audience reference. Personal items never surface here,
// regardless of who asks.
func (s *Store) Public(category string) []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return newestFirst(public(s.logs[category]))
}

// Private returns the audience's personal items, newest first, keeping
// only categories the allows callback admits (the caller wires this to
// the audience's live subscription relations — §9 read-time visibility:
// 取关即从下一次拉取起消失).
func (s *Store) Private(audienceID string, allows func(category string) bool) []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var mine []Item
	for _, items := range s.logs {
		for _, it := range items {
			if it.AudienceID == audienceID && (allows == nil || allows(it.Category)) {
				mine = append(mine, it)
			}
		}
	}
	return newestFirst(mine)
}

func public(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.AudienceID == "" {
			out = append(out, it)
		}
	}
	return out
}

// newestFirst returns a copy ordered latest publication first — the
// order RSS readers expect. The FIFO logs stay oldest-first internally;
// reversing at read keeps Add O(1).
func newestFirst(items []Item) []Item {
	out := make([]Item, len(items))
	for i, it := range items {
		out[len(items)-1-i] = it
	}
	return out
}
