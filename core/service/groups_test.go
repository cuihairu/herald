package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/escalation"
	"github.com/cuihairu/herald/core/groups"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/rules"
	"github.com/cuihairu/herald/core/template"
)

// stubGroupResolver expands against a fixed table.
type stubGroupResolver struct {
	groups map[string][]groups.Member
}

func (s *stubGroupResolver) ExpandGroup(name string) ([]groups.Member, bool) {
	members, ok := s.groups[name]
	return members, ok
}

func newGroupTestService(t *testing.T) (*NotificationService, *mockQueue) {
	t.Helper()
	runtime := newMockProviderRuntime()
	for _, name := range []string{"feishu-oncall", "sms-duty", "plain"} {
		runtime.RegisterProvider(name, &mockProvider{
			providerType: "test",
			capability: core.ProviderCapability{
				PayloadKinds:   []core.PayloadKind{core.PayloadContent},
				ContentFormats: []string{"plain"},
			},
		}, true)
	}
	queue := newMockQueue()
	svc := NewNotificationService(template.NewManager(), route.NewRouter(&route.Config{}), runtime, nil, queue)
	return svc, queue
}

func groupsNotification() *core.Notification {
	return &core.Notification{
		Type:  "alert",
		Level: "error",
		Content: &core.DirectContent{
			Title: "Group routed",
			Body:  "body",
		},
	}
}

// rulesDecisionForGroups is a route decision whose channels name a group —
// the expansion must work identically for rule-routed references.
func rulesDecisionForGroups() rules.Decision {
	return rules.Decision{
		RuleID:   "group-router",
		Mode:     rules.ModeActive,
		Action:   rules.ActionRoute,
		Channels: []string{groups.Ref("ops-oncall")},
	}
}

func TestProcessGroupReferences(t *testing.T) {
	ctx := context.Background()

	t.Run("explicit group reference expands to members", func(t *testing.T) {
		svc, queue := newGroupTestService(t)
		svc.SetGroupResolver(&stubGroupResolver{groups: map[string][]groups.Member{
			"ops-oncall": {
				{Channel: "feishu-oncall"},
				{Channel: "sms-duty", Recipients: []string{"13800000000"}},
			},
		}})

		n := groupsNotification()
		n.Channels = []string{groups.Ref("ops-oncall")}
		res, err := svc.Process(ctx, n)
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(res.Accepted) != 2 || res.Accepted[0] != "feishu-oncall" || res.Accepted[1] != "sms-duty" {
			t.Fatalf("expected both members, got %v (failed=%v)", res.Accepted, res.Failed)
		}
		if len(queue.tasks) != 2 {
			t.Fatalf("expected 2 tasks, got %d", len(queue.tasks))
		}
		// The pinned recipients ride along on the sms member's task.
		smsTask := queue.tasks[1]
		if smsTask.Provider != "sms-duty" {
			t.Fatalf("expected sms-duty second, got %+v", smsTask)
		}
		if len(smsTask.Targets) != 1 || smsTask.Targets[0] != "13800000000" {
			t.Fatalf("member recipients must pin the targets, got %v", smsTask.Targets)
		}
		// The member without a pin falls back to the notification's own
		// recipients (here: none).
		if len(queue.tasks[0].Targets) != 0 {
			t.Fatalf("unpinned member must fall back to notification targets, got %v", queue.tasks[0].Targets)
		}
	})

	t.Run("notification recipients are the fallback", func(t *testing.T) {
		svc, queue := newGroupTestService(t)
		svc.SetGroupResolver(&stubGroupResolver{groups: map[string][]groups.Member{
			"ops": {{Channel: "sms-duty"}},
		}})
		n := groupsNotification()
		n.Channels = []string{groups.Ref("ops")}
		n.Recipients = map[string][]string{"sms-duty": {"13900000000"}}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if got := queue.tasks[0].Targets; len(got) != 1 || got[0] != "13900000000" {
			t.Fatalf("expected the notification's recipients as fallback, got %v", got)
		}
	})

	t.Run("rule route channels expand through the same path", func(t *testing.T) {
		svc, queue := newGroupTestService(t)
		svc.SetGroupResolver(&stubGroupResolver{groups: map[string][]groups.Member{
			"ops-oncall": {{Channel: "feishu-oncall"}},
		}})
		decision := rulesDecisionForGroups()
		svc.SetRuleEngine(&stubEvaluator{decision: &decision})

		if _, err := svc.Process(ctx, groupsNotification()); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(queue.tasks) != 1 || queue.tasks[0].Provider != "feishu-oncall" {
			t.Fatalf("rule-routed group reference must expand, got %+v", queue.tasks)
		}
	})

	t.Run("unknown group fails visibly and spares the rest", func(t *testing.T) {
		svc, queue := newGroupTestService(t)
		svc.SetGroupResolver(&stubGroupResolver{groups: map[string][]groups.Member{}})

		n := groupsNotification()
		n.Channels = []string{groups.Ref("ghost"), "plain"}
		res, err := svc.Process(ctx, n)
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(res.Accepted) != 1 || res.Accepted[0] != "plain" {
			t.Fatalf("healthy channels must still deliver, got %+v", res)
		}
		if len(res.Failed) != 1 || res.Failed[0].Channel != groups.Ref("ghost") {
			t.Fatalf("the dead reference must be recorded, got %+v", res.Failed)
		}
		if len(queue.tasks) != 1 {
			t.Fatalf("expected one queued task, got %d", len(queue.tasks))
		}
	})

	t.Run("group reference without a resolver fails visibly", func(t *testing.T) {
		svc, _ := newGroupTestService(t)

		n := groupsNotification()
		n.Channels = []string{groups.Ref("ops-oncall")}
		res, err := svc.Process(ctx, n)
		// Every channel failed, so Process reports the aggregate error —
		// with the resolver gap named.
		if err == nil || !strings.Contains(err.Error(), "no group resolver") {
			t.Fatalf("expected the all-channels-failed error naming the resolver, got %v", err)
		}
		if len(res.Failed) != 1 || res.Failed[0].Channel != groups.Ref("ops-oncall") {
			t.Fatalf("expected the reference to fail, got %+v", res.Failed)
		}
		if len(res.TaskIDs) != 0 {
			t.Fatalf("nothing may be delivered for an unresolved reference, got %v", res.TaskIDs)
		}
	})

	t.Run("escalation targets expand", func(t *testing.T) {
		svc, queue := newGroupTestService(t)
		svc.SetGroupResolver(&stubGroupResolver{groups: map[string][]groups.Member{
			"wide": {{Channel: "feishu-oncall"}, {Channel: "sms-duty"}},
		}})
		if err := svc.DeliverEscalation(ctx, escalation.Pending{
			RuleID:    "r",
			AlertID:   "a",
			To:        []string{groups.Ref("wide")},
			Timeout:   time.Minute,
			CreatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("DeliverEscalation: %v", err)
		}
		if len(queue.tasks) != 2 {
			t.Fatalf("escalation to a group must reach every member, got %d tasks", len(queue.tasks))
		}
	})
}
