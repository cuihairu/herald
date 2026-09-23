package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/protocol"
	workersdk "github.com/cuihairu/herald/worker-sdk/go"
)

// DemoProvider implements a simple provider for demonstration
type DemoProvider struct{}

func (p *DemoProvider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	fmt.Printf("[Demo Worker] Delivered task:\n")
	fmt.Printf("  Task ID: %s\n", task.ID)
	fmt.Printf("  Provider: %s\n", task.Provider)
	fmt.Printf("  Targets: %v\n", task.Targets)
	return nil
}

func main() {
	// Run until the process is interrupted (Ctrl+C or SIGTERM).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		fmt.Printf("Failed to start: %v\n", err)
		os.Exit(1)
	}
}

// run builds the demo worker client with its demo callbacks and runs it
// until ctx is canceled.
func run(ctx context.Context) error {
	config := &protocol.WorkerConfig{
		WorkerID:          "demo-worker-001",
		CoreURL:           "ws://localhost:8081/worker",
		ReconnectDelay:    5 * time.Second,
		HeartbeatInterval: 30 * time.Second,
		Capabilities:      []string{"demo"},
	}

	// Queue is set to nil for demo; in production, pass a redis queue
	client := workersdk.NewClient(config, nil)

	client.OnTask(handleDemoTask)
	client.OnEvent(onDemoEvent)
	client.OnConnect(onDemoConnect)
	client.OnDisconnect(onDemoDisconnect)
	client.OnStateChange(onDemoStateChange)

	return client.Run(ctx)
}

func handleDemoTask(task *core.DeliveryTask) error {
	fmt.Printf("[Demo Worker] Received task:\n")
	fmt.Printf("  Task ID: %s\n", task.ID)
	fmt.Printf("  Provider: %s\n", task.Provider)
	if task.Payload.Content != nil {
		fmt.Printf("  Title: %s\n", task.Payload.Content.Title)
		fmt.Printf("  Body: %s\n", task.Payload.Content.Body)
	}
	return nil
}

func onDemoEvent(event *protocol.EventMessage) {
	fmt.Printf("[Demo Worker] Received event: %s\n", event.EventType)
}

func onDemoConnect() {
	fmt.Println("[Demo Worker] Connected to Herald core")
}

func onDemoDisconnect(err error) {
	fmt.Printf("[Demo Worker] Disconnected: %v\n", err)
}

func onDemoStateChange(state protocol.ConnectionState) {
	fmt.Printf("[Demo Worker] State changed: %s\n", state)
}
