package feeds

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
	"time"
)

func it(id, category, audience string, at time.Time) Item {
	return Item{ID: id, Category: category, Title: "title " + id, Body: "body " + id,
		AudienceID: audience, PublishedAt: at}
}

func TestAddRejectsIncompleteItems(t *testing.T) {
	s := NewStore(0)
	base := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	if err := s.Add(Item{Category: "alerts", Title: "t", PublishedAt: base}); err == nil {
		t.Error("Add without id = nil, want error")
	}
	if err := s.Add(Item{ID: "i1", Title: "t", PublishedAt: base}); err == nil {
		t.Error("Add without category = nil, want error")
	}
	if err := s.Add(Item{ID: "i1", Category: "alerts", PublishedAt: base}); err == nil {
		t.Error("Add without title = nil, want error")
	}
}

func TestPublicFeedCarriesOnlyAnonymousItems(t *testing.T) {
	s := NewStore(0)
	base := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	_ = s.Add(it("pub1", "alerts", "", base))
	_ = s.Add(it("alice1", "alerts", "alice", base.Add(time.Minute)))
	_ = s.Add(it("pub2", "alerts", "", base.Add(2*time.Minute)))
	_ = s.Add(it("other", "bills", "", base))

	got := s.Public("alerts")
	if len(got) != 2 || got[0].ID != "pub2" || got[1].ID != "pub1" {
		t.Fatalf("Public(alerts) = %+v, want [pub2 pub1] newest first", got)
	}
	if got := s.Public("missing"); len(got) != 0 {
		t.Fatalf("Public(missing) = %+v, want empty", got)
	}
}

func TestPrivateFeedFiltersByAudienceAndCategoryAdmission(t *testing.T) {
	s := NewStore(0)
	base := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	_ = s.Add(it("a-bills", "bills", "alice", base))
	_ = s.Add(it("a-alerts", "alerts", "alice", base.Add(time.Minute)))
	_ = s.Add(it("b-bills", "bills", "bob", base))
	_ = s.Add(it("pub", "bills", "", base))

	// alice only admits bills (her subscription relation set, wired by
	// the caller) — the alerts item stays hidden from this pull.
	got := s.Private("alice", func(category string) bool { return category == "bills" })
	if len(got) != 1 || got[0].ID != "a-bills" {
		t.Fatalf("Private(alice, bills) = %+v, want [a-bills]", got)
	}

	// Without any admission the audience sees nothing — 取关即从下一次
	// 拉取起消失.
	if got := s.Private("alice", func(string) bool { return false }); len(got) != 0 {
		t.Fatalf("Private(alice, none) = %+v, want empty", got)
	}
	// Bob's items never leak into alice's pull.
	if got := s.Private("alice", func(string) bool { return true }); len(got) != 2 {
		t.Fatalf("Private(alice, all) = %d items, want 2", len(got))
	}
}

func TestStoreCapsPerCategoryLog(t *testing.T) {
	s := NewStore(3)
	base := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		_ = s.Add(it(fmt.Sprintf("n%d", i), "alerts", "", base.Add(time.Duration(i)*time.Minute)))
	}
	got := s.Public("alerts")
	if len(got) != 3 {
		t.Fatalf("capped log = %d items, want 3", len(got))
	}
	// The oldest two were dropped: newest-first order starts at n4.
	if got[0].ID != "n4" || got[2].ID != "n2" {
		t.Fatalf("capped order = %s,%s,%s, want n4..n2", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestAddFillsZeroPublishTime(t *testing.T) {
	s := NewStore(0)
	if err := s.Add(Item{ID: "x", Category: "c", Title: "t"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := s.Public("c"); len(got) != 1 || got[0].PublishedAt.IsZero() {
		t.Fatalf("zero-time item = %+v, want publish time filled", got)
	}
}

func TestRenderRSSShape(t *testing.T) {
	now := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	items := []Item{
		{ID: "n-1", Category: "alerts", Title: "CPU 高 <&>", Body: "node-17 离线", PublishedAt: now},
		{ID: "n-2", Category: "alerts", Title: "旧一点", PublishedAt: now.Add(-time.Hour)},
	}
	out, err := RenderRSS(ChannelMeta{Title: "Herald 告警", Link: "https://herald.example", Description: "公开告警"}, items, now)
	if err != nil {
		t.Fatalf("RenderRSS: %v", err)
	}
	for _, want := range []string{
		`<?xml version="1.0"`,
		`<rss version="2.0">`,
		"<title>Herald 告警</title>",
		"<link>https://herald.example</link>",
		"<title>CPU 高 &lt;&amp;&gt;</title>", // escaping through encoding/xml
		"<guid isPermaLink=\"false\">n-1</guid>",
		"Wed, 07 Oct 2026 10:00:00 +0000",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered feed misses %q\ngot:\n%s", want, out)
		}
	}
	// Items arrive newest first as given; the older one sits below.
	if strings.Index(out, "n-1") > strings.Index(out, "n-2") {
		t.Error("feed order scrambled: n-1 must precede n-2")
	}
	// It must still parse as XML.
	var doc rssXML
	if err := xml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("rendered feed does not parse: %v", err)
	}
	if len(doc.Channel.Items) != 2 {
		t.Fatalf("parsed %d items, want 2", len(doc.Channel.Items))
	}
}

func TestRenderRSSRejectsEmptyTitleAndAllowsEmptyItems(t *testing.T) {
	if _, err := RenderRSS(ChannelMeta{}, nil, time.Now()); err == nil {
		t.Error("RenderRSS without title = nil, want error")
	}
	out, err := RenderRSS(ChannelMeta{Title: "Herald"}, nil, time.Now())
	if err != nil {
		t.Fatalf("RenderRSS empty: %v", err)
	}
	if !strings.Contains(out, "<rss") || strings.Contains(out, "<item>") {
		t.Errorf("empty feed = %q, want channel without items", out)
	}
}
