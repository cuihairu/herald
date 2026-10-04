package service

import (
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