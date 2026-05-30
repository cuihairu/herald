package logstore

import (
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func TestNew(t *testing.T) {
	t.Run("with valid limit", func(t *testing.T) {
		store := New(100)
		if store == nil {
			t.Fatal("expected non-nil store")
		}
		if store.limit != 100 {
			t.Errorf("expected limit 100, got %d", store.limit)
		}
	})

	t.Run("with zero limit", func(t *testing.T) {
		store := New(0)
		if store == nil {
			t.Fatal("expected non-nil store")
		}
		if store.limit != 1000 {
			t.Errorf("expected default limit 1000, got %d", store.limit)
		}
	})

	t.Run("with negative limit", func(t *testing.T) {
		store := New(-50)
		if store == nil {
			t.Fatal("expected non-nil store")
		}
		if store.limit != 1000 {
			t.Errorf("expected default limit 1000, got %d", store.limit)
		}
	})
}

func TestLogStore_Add(t *testing.T) {
	t.Run("add single log", func(t *testing.T) {
		store := New(10)
		log := &TaskLog{
			ID:        "log-1",
			Provider:  "email",
			Status:    "pending",
			CreatedAt: time.Now(),
		}
		store.Add(log)

		result := store.GetByID("log-1")
		if result == nil {
			t.Fatal("expected to find log by ID")
		}
		if result.Provider != "email" {
			t.Errorf("expected provider 'email', got '%s'", result.Provider)
		}
	})

	t.Run("add multiple logs", func(t *testing.T) {
		store := New(10)
		for i := 0; i < 5; i++ {
			log := &TaskLog{
				ID:        string(rune(i)),
				Provider:  "test",
				Status:    "pending",
				CreatedAt: time.Now(),
			}
			store.Add(log)
		}

		count := store.Count(nil)
		if count != 5 {
			t.Errorf("expected count 5, got %d", count)
		}
	})

	t.Run("respect limit when exceeded", func(t *testing.T) {
		store := New(3)
		now := time.Now()

		// Add 5 logs, only last 3 should be kept
		for i := 0; i < 5; i++ {
			log := &TaskLog{
				ID:        string(rune('a' + i)),
				Provider:  "test",
				Status:    "pending",
				CreatedAt: now.Add(time.Duration(i) * time.Millisecond),
			}
			store.Add(log)
		}

		count := store.Count(nil)
		if count != 3 {
			t.Errorf("expected count 3 (limit), got %d", count)
		}

		// First logs should be removed
		if store.GetByID("a") != nil {
			t.Error("expected first log to be removed")
		}
		// Last logs should be present
		if store.GetByID("c") == nil {
			t.Error("expected third log to be present")
		}
		if store.GetByID("e") == nil {
			t.Error("expected last log to be present")
		}
	})
}

func TestLogStore_UpdateStatus(t *testing.T) {
	t.Run("update existing log", func(t *testing.T) {
		store := New(10)
		createdAt := time.Now()
		log := &TaskLog{
			ID:        "log-1",
			Provider:  "email",
			Status:    "pending",
			CreatedAt: createdAt,
		}
		store.Add(log)

		// Wait a bit to ensure duration is measurable
		time.Sleep(10 * time.Millisecond)
		store.UpdateStatus("log-1", "success", "")

		result := store.GetByID("log-1")
		if result == nil {
			t.Fatal("expected to find log by ID")
		}
		if result.Status != "success" {
			t.Errorf("expected status 'success', got '%s'", result.Status)
		}
		if result.Error != "" {
			t.Errorf("expected no error, got '%s'", result.Error)
		}
		if result.CompletedAt == nil {
			t.Error("expected CompletedAt to be set")
		}
		if result.Duration <= 0 {
			t.Errorf("expected positive duration, got %d", result.Duration)
		}
	})

	t.Run("update with error", func(t *testing.T) {
		store := New(10)
		log := &TaskLog{
			ID:        "log-1",
			Provider:  "email",
			Status:    "pending",
			CreatedAt: time.Now(),
		}
		store.Add(log)

		errMsg := "connection failed"
		store.UpdateStatus("log-1", "failed", errMsg)

		result := store.GetByID("log-1")
		if result == nil {
			t.Fatal("expected to find log by ID")
		}
		if result.Status != "failed" {
			t.Errorf("expected status 'failed', got '%s'", result.Status)
		}
		if result.Error != errMsg {
			t.Errorf("expected error '%s', got '%s'", errMsg, result.Error)
		}
	})

	t.Run("update non-existent log", func(t *testing.T) {
		store := New(10)
		// Should not panic
		store.UpdateStatus("non-existent", "success", "")
	})
}

func TestLogStore_Get(t *testing.T) {
	store := New(10)
	now := time.Now()

	// Add logs with different properties
	store.Add(&TaskLog{ID: "1", Provider: "email", Status: "success", Level: "info", CreatedAt: now.Add(-2 * time.Hour)})
	store.Add(&TaskLog{ID: "2", Provider: "slack", Status: "failed", Level: "error", CreatedAt: now.Add(-1 * time.Hour)})
	store.Add(&TaskLog{ID: "3", Provider: "email", Status: "pending", Level: "info", CreatedAt: now})
	store.Add(&TaskLog{ID: "4", Provider: "telegram", Status: "success", Level: "warning", CreatedAt: now})
	store.Add(&TaskLog{ID: "5", Provider: "slack", Status: "success", Level: "info", CreatedAt: now})

	t.Run("get all logs", func(t *testing.T) {
		result := store.Get(0, 0, nil)
		if len(result) != 5 {
			t.Errorf("expected 5 logs, got %d", len(result))
		}
	})

	t.Run("with offset", func(t *testing.T) {
		result := store.Get(1, 0, nil)
		if len(result) != 4 {
			t.Errorf("expected 4 logs with offset 1, got %d", len(result))
		}
	})

	t.Run("with limit", func(t *testing.T) {
		result := store.Get(0, 2, nil)
		if len(result) != 2 {
			t.Errorf("expected 2 logs with limit 2, got %d", len(result))
		}
	})

	t.Run("with offset and limit", func(t *testing.T) {
		result := store.Get(1, 2, nil)
		if len(result) != 2 {
			t.Errorf("expected 2 logs with offset 1 and limit 2, got %d", len(result))
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		filter := &Filter{Status: "success"}
		result := store.Get(0, 0, filter)
		if len(result) != 3 {
			t.Errorf("expected 3 success logs, got %d", len(result))
		}
		for _, log := range result {
			if log.Status != "success" {
				t.Errorf("expected status 'success', got '%s'", log.Status)
			}
		}
	})

	t.Run("filter by provider", func(t *testing.T) {
		filter := &Filter{Provider: "email"}
		result := store.Get(0, 0, filter)
		if len(result) != 2 {
			t.Errorf("expected 2 email logs, got %d", len(result))
		}
		for _, log := range result {
			if log.Provider != "email" {
				t.Errorf("expected provider 'email', got '%s'", log.Provider)
			}
		}
	})

	t.Run("filter by level", func(t *testing.T) {
		filter := &Filter{Level: "info"}
		result := store.Get(0, 0, filter)
		if len(result) != 3 {
			t.Errorf("expected 3 info logs, got %d", len(result))
		}
	})

	t.Run("filter by since", func(t *testing.T) {
		since := now.Add(-90 * time.Minute)
		filter := &Filter{Since: since}
		result := store.Get(0, 0, filter)
		// Logs 2 (1hr ago), 3,4,5 (now) are all more recent than -90 min
		// Log 1 (2hr ago) is older than -90 min
		if len(result) != 4 {
			t.Errorf("expected 4 logs since 90 min ago, got %d", len(result))
		}
	})

	t.Run("filter by until", func(t *testing.T) {
		until := now.Add(-90 * time.Minute)
		filter := &Filter{Until: until}
		result := store.Get(0, 0, filter)
		// Only log 2 is at -1 hour, log 1 is at -2 hours, both should be included
		// But -2 hours is before -90 min, so only log 2 should match
		if len(result) != 1 {
			t.Errorf("expected 1 log until 90 min ago, got %d", len(result))
		}
	})

	t.Run("multiple filters", func(t *testing.T) {
		filter := &Filter{Provider: "email", Status: "success"}
		result := store.Get(0, 0, filter)
		if len(result) != 1 {
			t.Errorf("expected 1 email success log, got %d", len(result))
		}
		if result[0].ID != "1" {
			t.Errorf("expected log ID '1', got '%s'", result[0].ID)
		}
	})

	t.Run("offset beyond available logs", func(t *testing.T) {
		result := store.Get(100, 0, nil)
		if len(result) != 0 {
			t.Errorf("expected 0 logs with offset beyond count, got %d", len(result))
		}
	})

	t.Run("negative offset", func(t *testing.T) {
		result := store.Get(-1, 2, nil)
		if len(result) != 2 {
			t.Errorf("expected 2 logs with negative offset, got %d", len(result))
		}
	})
}

func TestLogStore_Count(t *testing.T) {
	store := New(10)
	now := time.Now()

	store.Add(&TaskLog{ID: "1", Provider: "email", Status: "success", CreatedAt: now})
	store.Add(&TaskLog{ID: "2", Provider: "slack", Status: "failed", CreatedAt: now})
	store.Add(&TaskLog{ID: "3", Provider: "email", Status: "pending", CreatedAt: now})

	t.Run("count all", func(t *testing.T) {
		count := store.Count(nil)
		if count != 3 {
			t.Errorf("expected count 3, got %d", count)
		}
	})

	t.Run("count with filter", func(t *testing.T) {
		filter := &Filter{Provider: "email"}
		count := store.Count(filter)
		if count != 2 {
			t.Errorf("expected count 2 for email, got %d", count)
		}
	})
}

func TestLogStore_GetByID(t *testing.T) {
	store := New(10)
	now := time.Now()

	store.Add(&TaskLog{ID: "log-1", Provider: "email", Status: "pending", CreatedAt: now})
	store.Add(&TaskLog{ID: "log-2", Provider: "slack", Status: "pending", CreatedAt: now})

	t.Run("find existing log", func(t *testing.T) {
		result := store.GetByID("log-1")
		if result == nil {
			t.Fatal("expected to find log by ID")
		}
		if result.Provider != "email" {
			t.Errorf("expected provider 'email', got '%s'", result.Provider)
		}
	})

	t.Run("log not found", func(t *testing.T) {
		result := store.GetByID("non-existent")
		if result != nil {
			t.Error("expected nil for non-existent log")
		}
	})
}

func TestLogStore_Clear(t *testing.T) {
	store := New(10)
	now := time.Now()

	store.Add(&TaskLog{ID: "1", Provider: "email", Status: "pending", CreatedAt: now})
	store.Add(&TaskLog{ID: "2", Provider: "slack", Status: "pending", CreatedAt: now})

	count := store.Count(nil)
	if count != 2 {
		t.Errorf("expected count 2 before clear, got %d", count)
	}

	store.Clear()

	count = store.Count(nil)
	if count != 0 {
		t.Errorf("expected count 0 after clear, got %d", count)
	}

	result := store.Get(0, 0, nil)
	if len(result) != 0 {
		t.Errorf("expected no logs after clear, got %d", len(result))
	}
}

func TestLogStore_Stats(t *testing.T) {
	store := New(10)
	now := time.Now()

	// Add various logs
	store.Add(&TaskLog{ID: "1", Provider: "email", Status: "success", Level: "info", CreatedAt: now})
	store.Add(&TaskLog{ID: "2", Provider: "slack", Status: "failed", Level: "error", CreatedAt: now})
	store.Add(&TaskLog{ID: "3", Provider: "email", Status: "success", Level: "info", CreatedAt: now})
	store.Add(&TaskLog{ID: "4", Provider: "telegram", Status: "pending", Level: "warning", CreatedAt: now})
	store.Add(&TaskLog{ID: "5", Provider: "slack", Status: "success", Level: "info", CreatedAt: now})

	stats := store.Stats()

	t.Run("total count", func(t *testing.T) {
		if stats.Total != 5 {
			t.Errorf("expected total 5, got %d", stats.Total)
		}
	})

	t.Run("by provider", func(t *testing.T) {
		if stats.ByProvider["email"] != 2 {
			t.Errorf("expected 2 email logs, got %d", stats.ByProvider["email"])
		}
		if stats.ByProvider["slack"] != 2 {
			t.Errorf("expected 2 slack logs, got %d", stats.ByProvider["slack"])
		}
		if stats.ByProvider["telegram"] != 1 {
			t.Errorf("expected 1 telegram log, got %d", stats.ByProvider["telegram"])
		}
	})

	t.Run("by status", func(t *testing.T) {
		if stats.ByStatus["success"] != 3 {
			t.Errorf("expected 3 success logs, got %d", stats.ByStatus["success"])
		}
		if stats.ByStatus["failed"] != 1 {
			t.Errorf("expected 1 failed log, got %d", stats.ByStatus["failed"])
		}
		if stats.ByStatus["pending"] != 1 {
			t.Errorf("expected 1 pending log, got %d", stats.ByStatus["pending"])
		}
	})

	t.Run("by level", func(t *testing.T) {
		if stats.ByLevel["info"] != 3 {
			t.Errorf("expected 3 info logs, got %d", stats.ByLevel["info"])
		}
		if stats.ByLevel["error"] != 1 {
			t.Errorf("expected 1 error log, got %d", stats.ByLevel["error"])
		}
		if stats.ByLevel["warning"] != 1 {
			t.Errorf("expected 1 warning log, got %d", stats.ByLevel["warning"])
		}
	})
}

func TestNewTaskLog(t *testing.T) {
	t.Run("create from delivery task", func(t *testing.T) {
		now := time.Now()
		task := &core.DeliveryTask{
			ID:       "task-1",
			Provider: "email",
			Level:    "high",
			Payload: core.DeliveryPayload{
				Kind: core.PayloadContent,
				Content: &core.RenderedContent{
					Title: "Test",
					Body:  "Body",
				},
			},
			CreatedAt: now,
		}

		log := NewTaskLog(task)

		if log.ID != task.ID {
			t.Errorf("expected ID '%s', got '%s'", task.ID, log.ID)
		}
		if log.Provider != task.Provider {
			t.Errorf("expected provider '%s', got '%s'", task.Provider, log.Provider)
		}
		if log.Level != task.Level {
			t.Errorf("expected level '%s', got '%s'", task.Level, log.Level)
		}
		if log.Status != "pending" {
			t.Errorf("expected status 'pending', got '%s'", log.Status)
		}
		if log.PayloadKind != string(core.PayloadContent) {
			t.Errorf("expected payload kind '%s', got '%s'", core.PayloadContent, log.PayloadKind)
		}
		if !log.CreatedAt.Equal(now) {
			t.Errorf("expected created at %v, got %v", now, log.CreatedAt)
		}
	})

	t.Run("handle task with provider template", func(t *testing.T) {
		now := time.Now()
		task := &core.DeliveryTask{
			ID:       "task-2",
			Provider: "aliyunsms",
			Level:    "normal",
			Payload: core.DeliveryPayload{
				Kind: core.PayloadProviderTemplate,
				ProviderTemplate: &core.ProviderTemplatePayload{
					TemplateCode: "SMS_123",
				},
			},
			CreatedAt: now,
		}

		log := NewTaskLog(task)

		if log.PayloadKind != string(core.PayloadProviderTemplate) {
			t.Errorf("expected payload kind '%s', got '%s'", core.PayloadProviderTemplate, log.PayloadKind)
		}
	})
}

func TestLogStore_Concurrency(t *testing.T) {
	t.Run("concurrent access", func(t *testing.T) {
		store := New(100)
		done := make(chan struct{})

		// Concurrent writes
		for i := 0; i < 10; i++ {
			go func(n int) {
				defer func() { done <- struct{}{} }()
				log := &TaskLog{
					ID:        string(rune(n)),
					Provider:  "test",
					Status:    "pending",
					CreatedAt: time.Now(),
				}
				store.Add(log)
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < 10; i++ {
			<-done
		}

		// Should have 10 logs
		count := store.Count(nil)
		if count != 10 {
			t.Errorf("expected count 10 after concurrent adds, got %d", count)
		}
	})
}
