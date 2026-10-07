package sdk

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// The §13 faces, in flow order: 配品类 (categories) → 策略与模板
// (policies, templates, callback) → 触发 (dispatch) → 查状态
// (deliveries, audit, relations).

// Category is one registered message category.
type Category struct {
	Name           string `json:"name"`
	DefaultUrgency string `json:"default_urgency"`
}

// RegisterCategory registers a category with its default urgency
// (§13.2 品类注册). Re-registering with the same urgency is idempotent;
// a different one is a conflict.
func (c *Client) RegisterCategory(ctx context.Context, name, defaultUrgency string) error {
	return c.call(ctx, http.MethodPost, "/api/v1/apps/"+c.app+"/categories",
		map[string]string{"name": name, "default_urgency": defaultUrgency}, nil)
}

// Categories lists the namespace's categories.
func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	var out struct {
		Categories []Category `json:"categories"`
	}
	if err := c.call(ctx, http.MethodGet, "/api/v1/apps/"+c.app+"/categories", nil, &out); err != nil {
		return nil, err
	}
	return out.Categories, nil
}

// PutPolicy replaces one policy family (§13.2 策略覆盖): family is one
// of "intensity", "delivery-mode", "escalation", "dedup" — the
// channel-matrix is a §5 safety bottom line and has no app-side
// override. The payload shape is the family's endpoint body.
func (c *Client) PutPolicy(ctx context.Context, family string, payload any) error {
	return c.call(ctx, http.MethodPut, "/api/v1/apps/"+c.app+"/policies/"+family, payload, nil)
}

// SetCallback points the §13.5 webhook face at the app's URL and signs
// every callback with the shared secret (16-128 chars).
func (c *Client) SetCallback(ctx context.Context, url, secret string) error {
	return c.call(ctx, http.MethodPut, "/api/v1/apps/"+c.app+"/callback",
		map[string]string{"url": url, "secret": secret}, nil)
}

// CallbackConfig is the masked read of the callback face.
type CallbackConfig struct {
	URL       string `json:"url"`
	HasSecret bool   `json:"has_secret"`
}

// Callback reads the callback configuration; the secret never leaves
// the server.
func (c *Client) Callback(ctx context.Context) (*CallbackConfig, error) {
	var out CallbackConfig
	if err := c.call(ctx, http.MethodGet, "/api/v1/apps/"+c.app+"/callback", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DispatchRequest is one §13.3 trigger. Category and Audiences are the
// only required fields; urgency defaults to the category's registered
// default and relation_type defaults to subscription (指派型触发必须
// 显式声明 enrollment). Content rides either Template+Params or the
// direct Title/Body pair.
type DispatchRequest struct {
	Category     string         `json:"category"`
	Urgency      string         `json:"urgency,omitempty"`
	RelationType string         `json:"relation_type,omitempty"`
	Audiences    []string       `json:"audiences"`
	DedupKey     string         `json:"dedup_key,omitempty"`
	EventID      string         `json:"event_id,omitempty"`
	State        string         `json:"state,omitempty"`
	Template     string         `json:"template,omitempty"`
	Params       map[string]any `json:"params,omitempty"`
	Title        string         `json:"title,omitempty"`
	Body         string         `json:"body,omitempty"`
}

// DispatchOutcome is the §13.3 受理结果与投递计划摘要: who matched, who
// was refused and why, what the plan looks like. Suppressed marks a
// dedup fold — the acceptance succeeded, the delivery did not happen.
type DispatchOutcome struct {
	NotificationID string   `json:"notification_id"`
	Category       string   `json:"category"`
	Urgency        string   `json:"urgency"`
	Mode           string   `json:"mode,omitempty"`
	Suppressed     bool     `json:"suppressed,omitempty"`
	Refused        []Ref    `json:"refused,omitempty"`
	Folded         []string `json:"folded,omitempty"`
	TaskIDs        []string `json:"task_ids,omitempty"`
}

// Ref is one refused audience×channel with the reason.
type Ref struct {
	Audience string `json:"audience"`
	Channel  string `json:"channel"`
	Reason   string `json:"reason"`
}

// Dispatch triggers one namespace event.
func (c *Client) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchOutcome, error) {
	var out DispatchOutcome
	if err := c.call(ctx, http.MethodPost, "/api/v1/apps/"+c.app+"/dispatch", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeliveriesQuery narrows the §13.4 投递状态 read. Zero fields are
// ignored; Limit is clamped server-side (default 50, ceiling 500).
type DeliveriesQuery struct {
	Audience string
	Category string
	Status   string
	Offset   int
	Limit    int
}

// DeliveryRow is one delivery attempt's settled row.
type DeliveryRow struct {
	ID         string    `json:"id"`
	Provider   string    `json:"provider"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	AudienceID string    `json:"audience_id,omitempty"`
	Category   string    `json:"category,omitempty"`
	Source     string    `json:"source,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// DeliveryPage is one page of the deliveries read.
type DeliveryPage struct {
	Total  int           `json:"total"`
	Offset int           `json:"offset"`
	Limit  int           `json:"limit"`
	Logs   []DeliveryRow `json:"logs"`
}

// Deliveries reads the namespace's delivery attempts, always scoped to
// the app's own source stamp.
func (c *Client) Deliveries(ctx context.Context, q DeliveriesQuery) (*DeliveryPage, error) {
	path := fmt.Sprintf("/api/v1/apps/%s/deliveries?audience=%s&category=%s&status=%s&offset=%d&limit=%d",
		c.app, q.Audience, q.Category, q.Status, q.Offset, q.Limit)
	var out DeliveryPage
	if err := c.call(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AuditEvent is one entry of the namespace's audit trail.
type AuditEvent struct {
	Kind         string    `json:"kind"`
	Category     string    `json:"category,omitempty"`
	AudienceID   string    `json:"audience_id,omitempty"`
	RelationType string    `json:"relation_type,omitempty"`
	Source       string    `json:"source,omitempty"`
	Detail       string    `json:"detail,omitempty"`
	At           time.Time `json:"at"`
}

// Audit reads the namespace's audit trail (dispatch folds and relation
// changes) strictly after since; zero time reads everything.
func (c *Client) Audit(ctx context.Context, since time.Time) ([]AuditEvent, error) {
	path := "/api/v1/apps/" + c.app + "/audit"
	if !since.IsZero() {
		path += "?since=" + since.UTC().Format(time.RFC3339)
	}
	var out struct {
		Events []AuditEvent `json:"events"`
	}
	if err := c.call(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Events, nil
}

// Relation is one audience×category×channel entitlement (类型/来源/策略位).
type Relation struct {
	AudienceID string `json:"audience_id"`
	Category   string `json:"category"`
	Channel    string `json:"channel"`
	Type       string `json:"type"`
	Source     string `json:"source"`
	Policy     struct {
		AllowUnsubscribe bool `json:"allow_unsubscribe"`
		MustDeliver      bool `json:"must_deliver"`
	} `json:"policy"`
}

// Relations reads one audience's standing relations. This face is the
// operator's — it answers with the whole registry's view of the
// audience, not just one namespace's.
func (c *Client) Relations(ctx context.Context, audienceID string) ([]Relation, error) {
	var out struct {
		Relations []Relation `json:"relations"`
	}
	if err := c.call(ctx, http.MethodGet, "/api/v1/audiences/"+audienceID+"/relations", nil, &out); err != nil {
		return nil, err
	}
	return out.Relations, nil
}

// RegisterTemplate registers one namespace template (§13.2 模板注册).
// The body is the template's API shape: id, name, title, level, fields
// with `{{.var}}` references, and the optional locales/channel binds.
func (c *Client) RegisterTemplate(ctx context.Context, tmpl any) error {
	return c.call(ctx, http.MethodPost, "/api/v1/apps/"+c.app+"/templates", tmpl, nil)
}

// Template is one registered namespace template.
type Template struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	Level string `json:"level,omitempty"`
}

// Templates lists the namespace's templates.
func (c *Client) Templates(ctx context.Context) ([]Template, error) {
	var out []Template
	if err := c.call(ctx, http.MethodGet, "/api/v1/apps/"+c.app+"/templates", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteTemplate removes one namespace template.
func (c *Client) DeleteTemplate(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodDelete, "/api/v1/apps/"+c.app+"/templates/"+id, nil, nil)
}
