// Command quickstart is the minimal library-embedding example: a
// zero-configuration App (in-memory queue, background delivery pool, dedup),
// the builtin log provider, and one synchronous dispatch.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cuihairu/herald"
	"github.com/cuihairu/herald/config"
	"github.com/cuihairu/herald/core"
)

// main terminates the process on failure and is not callable from tests;
// run() is the testable entry point.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "quickstart:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg := config.Default()
	cfg.Providers["log"] = config.ProviderConfig{Type: "log"}

	// run builds a fixed in-memory config that New always accepts.
	app, _ := herald.New(cfg)
	defer func() { _ = app.Close() }()

	// DispatchSync blocks until the pool has delivered (or failed) every
	// task created for this notification; use Dispatch for fire-and-forget.
	res, err := app.DispatchSync(ctx, &core.Notification{
		Type:     "quickstart",
		Level:    "info",
		Channels: []string{"log"},
		Content:  &core.DirectContent{Title: "Hello from herald", Body: "embedded via herald.New"},
	})
	if err != nil {
		return err
	}
	fmt.Printf("accepted channels: %v\n", res.Accepted)
	return nil
}
