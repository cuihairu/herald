package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cuihaitao/herald/protocol"
	workersdk "github.com/cuihaitao/herald/worker-sdk/go"
)

func main() {
	// Create worker config
	config := &protocol.WorkerConfig{
		WorkerID:         "demo-worker-001",
		CoreURL:          "ws://localhost:8080/worker",
		ReconnectDelay:   5 * time.Second,
		HeartbeatInterval: 30 * time.Second,
		Capabilities:     []string{"demo"},
	}

	// Create client
	client := workersdk.NewClient(config)

	// Set handlers
	client.OnTask(func(task *protocol.DispatchMessage) error {
		fmt.Printf("[Demo Worker] Received task:\n")
		fmt.Printf("  Task ID: %s\n", task.TaskID)
		fmt.Printf("  Provider: %s\n", task.Provider)
		fmt.Printf("  Title: %s\n", task.Title)
		fmt.Printf("  Body: %s\n", task.Body)

		// Process task
		time.Sleep(100 * time.Millisecond)

		// Acknowledge
		return client.Ack(task.TaskID, true, "")
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

	// Connect
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		os.Exit(1)
	}

	// Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("[Demo Worker] Running, press Ctrl+C to exit...")
	<-sigCh

	// Disconnect
	fmt.Println("[Demo Worker] Shutting down...")
	if err := client.Disconnect(); err != nil {
		fmt.Printf("Error disconnecting: %v\n", err)
	}

	fmt.Println("[Demo Worker] Bye!")
}
