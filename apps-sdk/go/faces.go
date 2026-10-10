package sdk

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// The §13 faces, in flow order: 配品类 (categories) → 策略与模板
// (policies, templates, callback) → 触发 (dispatch, events) → 查状态
// (deliveries, audit). Everything here is an app-token face; the
// operator faces (/api/v1/audiences/**, admin Bearer) are out of scope
// on purpose — one Client carries one app token, and the audience
// registry is global, not namespaced.

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

// AppShow is the namespace self-check: the token's proven scopes.
type AppShow struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// Show answers with the namespace name and the scopes the token holds —
// the cheapest way to prove a token works before wiring the rest.
func (c *Client) Show(ctx context.Context) (*AppShow, error) {
	var out AppShow
	if err := c.call(ctx, http.MethodGet, "/api/v1/apps/"+c.app, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PutPolicy replaces one policy family (§13.2 策略覆盖): family is one
// of "intensity", "delivery-mode", "escalation", "dedup" — the
// channel-matrix is a §5 safety bottom line and has no app-side
// override. The payload shape is the family's endpoint body.
func (c *Client) PutPolicy(ctx context.Context, family string, payload any) error {
	return c.call(ctx, http.MethodPut, "/api/v1/apps/"+c.app+"/policies/"+family, payload, nil)
}

// Policies is the aggregate read-back of the namespace's policy
// overrides. Families with no override are omitted — an untouched
// namespace reads back empty; AckTimeout is the escalation chain's
// wait-per-segment rendered as a Go duration string.
type Policies struct {
	ChannelIntensity map[string]string `json:"channel_intensity,omitempty"`
	ModeByCategory   map[string]string `json:"mode_by_category,omitempty"`
	AckTimeout       string            `json:"ack_timeout,omitempty"`
	DedupTiers       map[string]string `json:"dedup_tiers,omitempty"`
	DedupWindows     map[string]string `json:"dedup_windows,omitempty"`
}

// Policies reads the namespace's policy overrides back.
func (c *Client) Policies(ctx context.Context) (*Policies, error) {
	var out Policies
	if err := c.call(ctx, http.MethodGet, "/api/v1/apps/"+c.app+"/policies", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
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

// DeleteCallback removes the callback configuration: delivery results
// and unsubscribe backflow stop landing anywhere until SetCallback runs
// again.
func (c *Client) DeleteCallback(ctx context.Context) error {
	return c.call(ctx, http.MethodDelete, "/api/v1/apps/"+c.app+"/callback", nil, nil)
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

// EventsRequest is one §3 事件接入 event in the integrator's own
// vocabulary. Kind names a registered category; Severity maps
// critical→critical, warning→urgent, info→normal (anything else is a
// 422); Target is one audience ref (user:5 maps to user.5). ID is the
// source-side outbox id and rides the dedup gate as event_id — the
// delivery callback echoes it back.
type EventsRequest struct {
	ID         int64          `json:"id,omitempty"`
	Kind       string         `json:"kind"`
	Severity   string         `json:"severity"`
	Title      string         `json:"title,omitempty"`
	Body       string         `json:"body,omitempty"`
	Target     string         `json:"target"`
	DedupKey   string         `json:"dedup_key,omitempty"`
	Meta       map[string]any `json:"meta,omitempty"`
	OccurredAt time.Time      `json:"occurred_at"`
}

// Events pushes one external event at the 事件接入适配面: herald maps
// it onto a namespace dispatch and answers the same DispatchOutcome the
// dispatch face answers. The event semantics stay the integrator's —
// kind/severity/target are its words, not Herald's.
func (c *Client) Events(ctx context.Context, req EventsRequest) (*DispatchOutcome, error) {
	var out DispatchOutcome
	if err := c.call(ctx, http.MethodPost, "/api/v1/apps/"+c.app+"/events", req, &out); err != nil {
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

// TemplateField is one template field: a display label carrying a
// `{{.var}}` value template and an optional type hint.
type TemplateField struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

// TemplateBinding is one channel's per-template configuration (content
// format, vendor SMS template code/id, named param order).
type TemplateBinding struct {
	Format       string            `json:"format,omitempty"`
	TemplateCode string            `json:"template_code,omitempty"`
	TemplateID   string            `json:"template_id,omitempty"`
	Params       map[string]string `json:"params,omitempty"`
	ParamOrder   []string          `json:"param_order,omitempty"`
}

// TemplateDetail is one namespace template with its full definition —
// the single-template read answers this; the list read answers the
// trimmed Template.
type TemplateDetail struct {
	ID        string                     `json:"id"`
	Name      string                     `json:"name"`
	Title     string                     `json:"title,omitempty"`
	Level     string                     `json:"level,omitempty"`
	Fields    []TemplateField            `json:"fields,omitempty"`
	Bindings  map[string]TemplateBinding `json:"bindings,omitempty"`
	CreatedAt time.Time                  `json:"created_at"`
	UpdatedAt time.Time                  `json:"updated_at"`
}

// Template reads one namespace template in full.
func (c *Client) Template(ctx context.Context, id string) (*TemplateDetail, error) {
	var out TemplateDetail
	if err := c.call(ctx, http.MethodGet, "/api/v1/apps/"+c.app+"/templates/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
