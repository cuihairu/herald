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
	config := &protocol.WorkerConfig{
		WorkerID:          "demo-worker-001",
		CoreURL:           "ws://localhost:8081/worker",
		ReconnectDelay:    5 * time.Second,
		HeartbeatInterval: 30 * time.Second,
		Capabilities:      []string{"demo"},
	}

	// Queue is set to nil for demo; in production, pass a redis queue
	client := workersdk.NewClient(config, nil)

	client.OnTask(func(task *core.DeliveryTask) error {
		fmt.Printf("[Demo Worker] Received task:\n")
		fmt.Printf("  Task ID: %s\n", task.ID)
		fmt.Printf("  Provider: %s\n", task.Provider)
		if task.Payload.Content != nil {
			fmt.Printf("  Title: %s\n", task.Payload.Content.Title)
			fmt.Printf("  Body: %s\n", task.Payload.Content.Body)
		}
		return nil
	})

	client.OnEvent(func(event *protocol.EventMessage) {
		fmt.Printf("[Demo Worker] Received event: %s\n", event.EventType)
	})

	client.OnConnect(func() {
		fmt.Println("[Demo Worker] Connected to Herald core")
	})

	client.OnDisconnect(func(err error) {
		fmt.Printf("[Demo Worker] Disconnected: %v\n", err)
	})

	client.OnStateChange(func(state protocol.ConnectionState) {
		fmt.Printf("[Demo Worker] State changed: %s\n", state)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Run(ctx); err != nil {
		fmt.Printf("Failed to start: %v\n", err)
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("[Demo Worker] Running, press Ctrl+C to exit...")
	<-sigCh

	fmt.Println("[Demo Worker] Shutting down...")
	_ = client.Disconnect()
	fmt.Println("[Demo Worker] Bye!")
}
