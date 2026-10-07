package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/groups"
	"github.com/cuihairu/herald/core/rules"
)

func usersNotification() *core.Notification {
	return &core.Notification{
		Type:  "alert",
		Level: "error",
		Content: &core.DirectContent{
			Title: "User routed",
			Body:  "body",
		},
	}
}

// userTables is the audience-side mirror of newGroupTestService's provider
// table (feishu-oncall, sms-duty, plain): alice and bob both live on
// feishu-oncall, carol only on sms-duty.
func userTables() *audience.Manager {
	m, err := audience.NewManager(
		map[string]audience.Audience{
			"ops":        {Recipients: []string{"alice", "bob"}},
			"single-dir": {Recipients: []string{"carol"}},
		},
		map[string]audience.Recipient{
			"alice": {Endpoints: []audience.Endpoint{
				{Type: "feishu-oncall", Target: "@alice"},
				{Type: "plain", Target: "alice-chan"},
			}},
			"bob": {Endpoints: []audience.Endpoint{
				{Type: "feishu-oncall", Target: "@bob"},
				{Type: "sms-duty", Target: "13900000000"},
			}},
			"carol": {Endpoints: []audience.Endpoint{
				{Type: "sms-duty", Target: "13800000000"},
			}},
		},
	)
	if err != nil {
		panic(err)
	}
	return m
}

func TestProcessUserReferences(t *testing.T) {
	ctx := context.Background()

	t.Run("user reference merges endpoints per provider", func(t *testing.T) {
		svc, queue := newGroupTestService(t)
		svc.SetUserResolver(userTables())

		n := usersNotification()
		n.Channels = []string{audience.Ref("ops")}
		res, err := svc.Process(ctx, n)
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(res.TaskIDs) != 3 {
			t.Fatalf("expected 3 tasks (one per provider), got %d (accepted=%v failed=%v)", len(res.TaskIDs), res.Accepted, res.Failed)
		}
		// Deterministic per-provider order: feishu-oncall, plain, sms-duty.
		if len(queue.tasks) != 3 {
			t.Fatalf("expected 3 tasks, got %d", len(queue.tasks))
		}
		feishu := queue.tasks[0]
		if feishu.Provider != "feishu-oncall" {
			t.Fatalf("expected feishu-oncall first, got %+v", queue.tasks)
		}
		// Alice and bob share the provider: one task with both targets.
		if len(feishu.Targets) != 2 || feishu.Targets[0] != "@alice" || feishu.Targets[1] != "@bob" {
			t.Fatalf("shared provider must bundle both targets, got %v", feishu.Targets)
		}
		if queue.tasks[1].Provider != "plain" || len(queue.tasks[1].Targets) != 1 {
			t.Fatalf("expected the plain endpoint as a task of its own, got %+v", queue.tasks[1])
		}
		if queue.tasks[2].Provider != "sms-duty" || len(queue.tasks[2].Targets) != 1 {
			t.Fatalf("expected the sms endpoint as a task of its own, got %+v", queue.tasks[2])
		}
		// Every queued task starts its state machine as queued.
		for i, qt := range queue.tasks {
			if qt.Status != core.StatusQueued {
				t.Fatalf("task %d must be planned as queued, got %q", i, qt.Status)
			}
		}
	})

	t.Run("direct recipient reference resolves", func(t *testing.T) {
		svc, queue := newGroupTestService(t)
		svc.SetUserResolver(userTables())

		n := usersNotification()
		n.Channels = []string{audience.Ref("carol")}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(queue.tasks) != 1 || queue.tasks[0].Provider != "sms-duty" {
			t.Fatalf("expected carol's sms task, got %+v", queue.tasks)
		}
	})

	t.Run("audience table takes precedence over recipient table", func(t *testing.T) {
		m, err := audience.NewManager(
			map[string]audience.Audience{"shared": {Recipients: []string{"carol"}}},
			map[string]audience.Recipient{
				"shared": {Endpoints: []audience.Endpoint{{Type: "plain", Target: "direct-id"}}},
				"carol":  {Endpoints: []audience.Endpoint{{Type: "sms-duty", Target: "13800000000"}}},
			},
		)
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}
		svc, queue := newGroupTestService(t)
		svc.SetUserResolver(m)

		n := usersNotification()
		n.Channels = []string{audience.Ref("shared")}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(queue.tasks) != 1 || queue.tasks[0].Provider != "sms-duty" {
			t.Fatalf("the audience must win, got %+v", queue.tasks)
		}
	})

	t.Run("unknown user fails visibly and spares the rest", func(t *testing.T) {
		svc, queue := newGroupTestService(t)
		svc.SetGroupResolver(&stubGroupResolver{groups: map[string][]groups.Member{
			"ops-oncall": {{Channel: "plain"}},
		}})
		svc.SetUserResolver(userTables())

		n := usersNotification()
		n.Channels = []string{groups.Ref("ops-oncall"), audience.Ref("ghost")}
		res, err := svc.Process(ctx, n)
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(queue.tasks) != 1 || queue.tasks[0].Provider != "plain" {
			t.Fatalf("the valid channel must still be delivered, got %d tasks", len(queue.tasks))
		}
		if len(res.Failed) != 1 || !strings.Contains(res.Failed[0].Error, "unknown user") {
			t.Fatalf("expected the unknown-user failure, got %v", res.Failed)
		}
	})

	t.Run("user reference without resolver fails visibly", func(t *testing.T) {
		svc, queue := newGroupTestService(t)
		_ = queue

		n := usersNotification()
		n.Channels = []string{audience.Ref("alice")}
		res, err := svc.Process(ctx, n)
		// Every channel failed, so Process reports the aggregate error —
		// with the resolver gap named.
		if err == nil || !strings.Contains(err.Error(), "no user resolver") {
			t.Fatalf("expected the all-channels-failed error naming the resolver, got %v", err)
		}
		if len(res.Failed) != 1 || res.Failed[0].Channel != audience.Ref("alice") {
			t.Fatalf("expected the reference to fail, got %+v", res.Failed)
		}
		if len(res.TaskIDs) != 0 {
			t.Fatalf("nothing may be delivered for an unresolved reference, got %v", res.TaskIDs)
		}
	})

	t.Run("rule route channels expand user references the same way", func(t *testing.T) {
		svc, queue := newGroupTestService(t)
		svc.SetUserResolver(userTables())
		decision := rules.Decision{
			RuleID:   "user-router",
			Mode:     rules.ModeActive,
			Action:   rules.ActionRoute,
			Channels: []string{audience.Ref("carol")},
		}
		svc.SetRuleEngine(&stubEvaluator{decision: &decision})

		if _, err := svc.Process(ctx, usersNotification()); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(queue.tasks) != 1 || queue.tasks[0].Provider != "sms-duty" {
			t.Fatalf("rule-routed user reference must expand, got %+v", queue.tasks)
		}
	})
}

func TestMergeUserEndpoints(t *testing.T) {
	// Out-of-order input must produce sorted per-provider, bundled output
	// (determinism keeps task and log order stable across runs).
	eps := []audience.Endpoint{
		{Type: "sms-duty", Target: "13900000000"},
		{Type: "feishu-oncall", Target: "@alice"},
		{Type: "mail", Target: "a@b.c"},
		{Type: "feishu-oncall", Target: "@bob"},
	}
	got := mergeUserEndpoints(eps)
	if len(got) != 3 {
		t.Fatalf("expected 3 providers, got %v", got)
	}
	if got[0].channel != "feishu-oncall" || len(got[0].targets) != 2 || got[0].targets[0] != "@alice" || got[0].targets[1] != "@bob" {
		t.Errorf("feishu must bundle both targets first: %+v", got[0])
	}
	if got[1].channel != "mail" || len(got[1].targets) != 1 {
		t.Errorf("mail must come second: %+v", got[1])
	}
	if got[2].channel != "sms-duty" || len(got[2].targets) != 1 {
		t.Errorf("sms-duty must come third: %+v", got[2])
	}

	// An empty expansion produces no targets (a resolver that resolved an
	// audience to nothing yields an empty, harmless plan).
	if out := mergeUserEndpoints(nil); len(out) != 0 {
		t.Errorf("empty endpoints must merge to nothing, got %v", out)
	}
}
