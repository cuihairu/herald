package callback

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

// ProviderName is the queue-side provider name every callback task
// carries; heraldd registers one instance of this provider under it.
const ProviderName = "app-callback"

// Provider posts signed callback events to the URL the emitter pinned
// in the task targets. It carries no per-instance configuration: the
// event, its signature and its destination all travel on the task, so
// one instance serves every app namespace.
type Provider struct {
	status *core.ProviderStatus
	client *httpclient.Client
}

// NewProvider builds the callback dispatcher provider.
func NewProvider() *Provider {
	return &Provider{
		status: &core.ProviderStatus{
			Name:   ProviderName,
			Type:   ProviderName,
			Status: "available",
			Since:  time.Now(),
		},
		client: httpclient.NewClient(nil),
	}
}

// Name identifies the provider on the queue task.
func (p *Provider) Name() string { return ProviderName }

// Type is the provider family.
func (p *Provider) Type() string { return ProviderName }

// Status reports the provider's health.
func (p *Provider) Status() *core.ProviderStatus { return p.status }

// Deliver posts one callback event. A non-2xx answer is an error so the
// pipeline's retry budget and at-least-once redelivery apply to the
// callback exactly as they do to any delivery.
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	if len(task.Targets) == 0 || task.Targets[0] == "" {
		return fmt.Errorf("callback: task carries no url")
	}
	eventID, _ := task.Payload.Raw["event_id"].(string)
	body, _ := task.Payload.Raw["body"].(string)
	signature, _ := task.Payload.Raw["signature"].(string)
	if body == "" || signature == "" {
		return fmt.Errorf("callback: task carries no signed event body")
	}
	// Non-2xx answers come back as errors from the client itself, so
	// the pipeline's retry budget applies to them unchanged.
	_, err := p.client.PostJSONWithHeaders(ctx, task.Targets[0], json.RawMessage(body), map[string]string{
		"Content-Type":  "application/json",
		SignatureHeader: signature,
		EventIDHeader:   eventID,
	})
	return err
}
