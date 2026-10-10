// Command example walks the §13 integration flow against a running
// herald with one app token: self-check → categories → (trigger, when
// the token carries the scope) → deliveries → audit. It is a
// read-mostly demo — the one event it fires lands as a normal dispatch
// whose refused rows are answers, not errors.
//
// Configuration comes from the environment (HERALD_URL, HERALD_APP,
// HERALD_TOKEN); without HERALD_TOKEN there is nothing to authenticate
// with and the demo refuses to start.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	sdk "github.com/cuihairu/herald/apps-sdk/go"
)

// demoConfig is the demo client's deployment coordinates.
type demoConfig struct {
	BaseURL string
	App     string
	Token   string
}

// main terminates the process on failure and is not callable from
// tests; run() is the testable entry point.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, loadDemoConfig()); err != nil {
		fmt.Fprintln(os.Stderr, "appsdk-example:", err)
		os.Exit(1)
	}
}

// loadDemoConfig reads the deployment coordinates from the environment,
// falling back to the local single-node defaults the guide seeds.
func loadDemoConfig() demoConfig {
	return demoConfig{
		BaseURL: envOr("HERALD_URL", "http://127.0.0.1:8080"),
		App:     envOr("HERALD_APP", "demo-app"),
		Token:   os.Getenv("HERALD_TOKEN"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// run walks the §13 flow: every step is one SDK call and the first
// failure returns, so a mis-seeded token or an unreachable herald
// fails fast with the underlying *sdk.Error.
func run(ctx context.Context, cfg demoConfig) error {
	if cfg.Token == "" {
		return errors.New("HERALD_TOKEN is required (any seeded app token)")
	}
	c := sdk.New(cfg.BaseURL, cfg.App, cfg.Token)

	// Self-check: the token proves itself with name + scopes; the rest
	// of the walk keys off what it grants.
	show, err := c.Show(ctx)
	if err != nil {
		return fmt.Errorf("self-check: %w", err)
	}
	fmt.Printf("herald %s  app=%s  scopes=%v\n", cfg.BaseURL, show.Name, show.Scopes)

	cats, err := c.Categories(ctx)
	if err != nil {
		return fmt.Errorf("categories: %w", err)
	}
	for _, cat := range cats {
		fmt.Printf("category %s (default urgency: %s)\n", cat.Name, cat.DefaultUrgency)
	}

	// Trigger: one external event in the integrator's own vocabulary.
	// Refused audiences (no relation, no surface, no channel) come back
	// as rows of the outcome — answers, not failures.
	if hasScope(show.Scopes, "trigger") {
		out, err := c.Events(ctx, sdk.EventsRequest{
			Kind:     "alerts",
			Severity: "info",
			Title:    "hello from the appsdk example",
			Target:   "user:1",
			Meta:     map[string]any{"demo": true},
		})
		if err != nil {
			return fmt.Errorf("trigger: %w", err)
		}
		fmt.Printf("triggered: category=%s urgency=%s tasks=%d\n", out.Category, out.Urgency, len(out.TaskIDs))
		for _, ref := range out.Refused {
			fmt.Printf("  refused: %s/%s (%s)\n", ref.Audience, ref.Channel, ref.Reason)
		}
	}

	page, err := c.Deliveries(ctx, sdk.DeliveriesQuery{Limit: 5})
	if err != nil {
		return fmt.Errorf("deliveries: %w", err)
	}
	fmt.Printf("deliveries: %d rows (showing %d)\n", page.Total, len(page.Logs))
	for _, row := range page.Logs {
		fmt.Printf("  %s  %s  via %s  audience=%s\n", row.CreatedAt.Format(time.RFC3339), row.Status, row.Provider, row.AudienceID)
	}

	events, err := c.Audit(ctx, time.Time{})
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	fmt.Printf("audit: %d events\n", len(events))
	return nil
}

// hasScope reports whether the token's granted scope set contains one.
func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}
