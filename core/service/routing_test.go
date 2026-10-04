package service

import (
	"context"
	"testing"

	"github.com/cuihairu/herald/core/groups"
)

// TestExpandRefs pins the pure routing-decision step: reference
// expansion must be side-effect free (no queue access) and fail per
// reference, never for the whole batch. The execution half (enqueue)
// is exercised through the Process tests.
func TestExpandRefs(t *testing.T) {
	t.Run("empty references resolve to nothing", func(t *testing.T) {
		svc, _ := newGroupTestService(t)
		targets, failed := svc.expandRefs(nil)
		if len(targets) != 0 || len(failed) != 0 {
			t.Errorf("expected empty decision, got %d targets, %d failures", len(targets), len(failed))
		}
	})

	t.Run("plain references pass through", func(t *testing.T) {
		svc, _ := newGroupTestService(t)
		targets, failed := svc.expandRefs([]string{"plain", "feishu-oncall"})
		if len(failed) != 0 {
			t.Fatalf("expected no failures, got %v", failed)
		}
		if len(targets) != 2 || targets[0].channel != "plain" || targets[1].channel != "feishu-oncall" {
			t.Errorf("expected two pass-through targets, got %v", targets)
		}
	})

	t.Run("group and user references expand", func(t *testing.T) {
		svc, _ := newGroupTestService(t)
		svc.SetGroupResolver(&stubGroupResolver{groups: map[string][]groups.Member{
			"oncall": {{Channel: "plain"}},
		}})
		svc.SetUserResolver(userTables())
		targets, failed := svc.expandRefs([]string{"plain", "group:oncall", "user:alice"})
		if len(failed) != 0 {
			t.Fatalf("expected no failures, got %v", failed)
		}
		// plain (1) + group member (1) + alice's endpoints merged per
		// provider, sorted: feishu-oncall + plain (2) — 4 in total.
		if len(targets) != 4 {
			t.Errorf("expected 4 targets, got %d: %v", len(targets), targets)
		}
	})

	t.Run("a failing reference does not block the rest", func(t *testing.T) {
		svc, _ := newGroupTestService(t)
		targets, failed := svc.expandRefs([]string{"plain", "group:missing", "feishu-oncall"})
		if len(targets) != 2 {
			t.Errorf("expected 2 targets, got %d: %v", len(targets), targets)
		}
		if len(failed) != 1 || failed[0].Channel != "group:missing" {
			t.Errorf("expected 1 failure for group:missing, got %v", failed)
		}
	})

	t.Run("references without a resolver fail explicitly", func(t *testing.T) {
		svc, _ := newGroupTestService(t)
		targets, failed := svc.expandRefs([]string{"group:any", "user:alice"})
		if len(targets) != 0 {
			t.Errorf("expected 0 targets, got %v", targets)
		}
		if len(failed) != 2 {
			t.Errorf("expected 2 failures, got %v", failed)
		}
	})
}

// stubChannelResolver expands against a fixed channels table.
type stubChannelResolver struct {
	channels map[string][]string
}

func (s *stubChannelResolver) ExpandChannel(name string) ([]string, bool) {
	providers, ok := s.channels[name]
	return providers, ok
}

// TestExpandRefsChannelsBlock pins the "channel 显式 > channels 块"
// priority for plain names: an explicit provider instance wins over the
// channels block; a block name expands to its providers in configured
// order; a name in neither stays a literal provider target (whose miss
// fails at delivery time, not in routing).
func TestExpandRefsChannelsBlock(t *testing.T) {
	t.Run("block name expands to its providers in order", func(t *testing.T) {
		svc, _ := newGroupTestService(t)
		svc.SetChannelResolver(&stubChannelResolver{channels: map[string][]string{
			"ci": {"feishu-oncall", "sms-duty"},
		}})
		targets, failed := svc.expandRefs([]string{"ci"})
		if len(failed) != 0 {
			t.Fatalf("expected no failures, got %v", failed)
		}
		if len(targets) != 2 || targets[0].channel != "feishu-oncall" || targets[1].channel != "sms-duty" {
			t.Errorf("expected [feishu-oncall sms-duty], got %v", targets)
		}
	})

	t.Run("an explicit provider wins over the block", func(t *testing.T) {
		svc, _ := newGroupTestService(t)
		svc.SetChannelResolver(&stubChannelResolver{channels: map[string][]string{
			"plain": {"sms-duty"},
		}})
		targets, failed := svc.expandRefs([]string{"plain"})
		if len(failed) != 0 {
			t.Fatalf("expected no failures, got %v", failed)
		}
		if len(targets) != 1 || targets[0].channel != "plain" {
			t.Errorf("the provider instance must pass through, got %v", targets)
		}
	})

	t.Run("name in neither stays a literal provider target", func(t *testing.T) {
		svc, _ := newGroupTestService(t)
		svc.SetChannelResolver(&stubChannelResolver{channels: map[string][]string{
			"ci": {"feishu-oncall"},
		}})
		targets, failed := svc.expandRefs([]string{"nonexistent"})
		if len(failed) != 0 {
			t.Fatalf("expected no routing failures, got %v", failed)
		}
		if len(targets) != 1 || targets[0].channel != "nonexistent" {
			t.Errorf("expected the literal target, got %v", targets)
		}
	})
}

// TestProcessChannelBlockReference runs the channels block through the
// full pipeline: an explicit channel that is not a provider instance
// expands into one delivery task per backend provider.
func TestProcessChannelBlockReference(t *testing.T) {
	ctx := context.Background()
	svc, queue := newGroupTestService(t)
	svc.SetChannelResolver(&stubChannelResolver{channels: map[string][]string{
		"ci": {"feishu-oncall", "sms-duty"},
	}})

	n := groupsNotification()
	n.Channels = []string{"ci"}
	res, err := svc.Process(ctx, n)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(res.Accepted) != 2 || res.Accepted[0] != "feishu-oncall" || res.Accepted[1] != "sms-duty" {
		t.Fatalf("expected both block providers accepted, got %v (failed=%v)", res.Accepted, res.Failed)
	}
	if len(queue.tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(queue.tasks))
	}
	for i, want := range []string{"feishu-oncall", "sms-duty"} {
		if queue.tasks[i].Provider != want {
			t.Errorf("task[%d].Provider = %s, want %s", i, queue.tasks[i].Provider, want)
		}
	}
}