package herald_test

import (
	"context"
	"fmt"

	"github.com/cuihairu/herald"
	"github.com/cuihairu/herald/core"
)

// echoProvider prints the notification title, so the example below has
// observable output without external services.
type echoProvider struct{}

func (echoProvider) Deliver(_ context.Context, task *core.DeliveryTask) error {
	title := ""
	if task.Payload.Content != nil {
		title = task.Payload.Content.Title
	}
	fmt.Println("delivered:", title)
	return nil
}

func (echoProvider) Name() string                 { return "echo" }
func (echoProvider) Type() string                 { return "echo" }
func (echoProvider) Status() *core.ProviderStatus { return &core.ProviderStatus{Name: "echo", Type: "echo", Status: "ok"} }

// ExampleNew embeds Herald with the zero-configuration defaults: an
// in-memory queue, a background delivery pool and dedup.
func ExampleNew() {
	app, err := herald.New(nil)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() { _ = app.Close() }()

	if err := app.Runtime().RegisterProvider("echo", echoProvider{}, true); err != nil {
		fmt.Println("error:", err)
		return
	}

	res, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:     "demo",
		Channels: []string{"echo"},
		Content:  &core.DirectContent{Title: "hello"},
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(res.Accepted)
	// Output:
	// delivered: hello
	// [echo]
}
