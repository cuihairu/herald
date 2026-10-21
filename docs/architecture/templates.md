# Template System Architecture

## Design Goals

The core goal of Herald's template system is to **decouple message format from the sending channel**:

- Define once, reuse across channels
- Runtime management (CRUD API)
- Automatic channel adaptation

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                  Template System Architecture                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  User request                                                   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ { template, params, channels }                            │   │
│  └──────────────────────────────────────────────────────────┘   │
│                          ↓                                      │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    Template Manager                       │   │
│  │  ┌────────────────────────────────────────────────────┐  │   │
│  │  │ 1. Get Template by ID                              │  │   │
│  │  │ 2. Engine.Render() → RenderedData                  │  │   │
│  │  │    (variable substitution, yielding semantic data)  │  │   │
│  │  └────────────────────────────────────────────────────┘  │   │
│  └──────────────────────────────────────────────────────────┘   │
│                          ↓                                      │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │              DeliveryPlanner.buildPayload()               │   │
│  │  ┌────────────────────────────────────────────────────┐  │   │
│  │  │ binding has provider template + Provider support → │  │   │
│  │  │  PayloadProviderTemplate (vendor-rendered)         │  │   │
│  │  │ else → selectFormat() picks format, then renders:  │  │   │
│  │  │   binding.format > ContentFormats[0] > plain        │  │   │
│  │  └────────────────────────────────────────────────────┘  │   │
│  └──────────────────────────────────────────────────────────┘   │
│                          ↓                                      │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    Renderer                               │   │
│  │  ┌─────────────┬──────────────────┬─────────────┬──────────┐  │   │
│  │  │ HTMLRenderer│MarkdownRenderer│PlainRenderer│JSONRenderer│ │   │
│  │  └─────────────┴──────────────────┴─────────────┴──────────┘  │   │
│  └──────────────────────────────────────────────────────────┘   │
│                          ↓                                      │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    Task                                   │   │
│  │  { Title, Body, RenderFormat, Data }                     │   │
│  └──────────────────────────────────────────────────────────┘   │
│                          ↓                                      │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    Provider.Deliver()                     │   │
│  │  ┌──────────┬──────────┬──────────┬──────────────────┐  │   │
│  │  │  Email   │   SMS    │    IM    │   Rich Media     │  │   │
│  │  │ RenderFmt│ template_code │Body │  Body (rendered)  │  │   │
│  │  └──────────┴──────────┴──────────┴──────────────────┘  │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Template (template definition)

```go
type Template struct {
    ID        string             // unique template identifier
    Name      string             // template name
    Title     string             // title template (supports variables)
    Level     string             // default level
    Fields    []Field            // field list
    Bindings  map[string]Binding // per-channel adaptation config (format / template_code / ...)
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

**Design points**:

- `Title` and `Fields.Value` support variable substitution (Go template syntax)
- `Fields` are semantic fields and carry no concrete format
- `Level` is used together with routing rules

### 2. Engine (template engine)

```go
type Engine struct {
    templates map[string]*template.Template
    funcMap   template.FuncMap
}

func (e *Engine) Render(tmpl *Template, params map[string]interface{}) (*RenderedData, error)
```

**Responsibilities**:

- Perform variable substitution (`&#123;&#123;.Variable&#125;&#125;`)
- Built-in functions: `toUpper`, `toLower`, `trim`
- Return `RenderedData` (semantic data, channel-independent)

### 3. Renderer

```go
type Renderer interface {
    Format() RenderFormat
    Render(ctx context.Context, data *RenderedData) (interface{}, error)
}
```

**Implementations**:

| Renderer | Format | Output example |
|----------|------|---------|
| HTMLRenderer | HTML | `<table>...</table>` |
| MarkdownRenderer | Markdown | `### Title\n\n**Key**: Value` |
| PlainRenderer | plain text | `Title\n\nKey: Value` |
| JSONRenderer | JSON | `{"card": {...}}` |

### 4. Manager (template manager)

```go
type Manager struct {
    templates map[string]*Template
    engine    *Engine
}

func (m *Manager) Register(tmpl *Template) error
func (m *Manager) Get(id string) (*Template, error)
func (m *Manager) Render(id string, params map[string]interface{}) (*RenderedData, error)
```

## Provider Adaptation

### Format Selection (selectFormat)

A Provider declares the formats it supports through the `ContentFormats` field in its Capability; there is no separate interface:

```go
// core/provider.go
type ProviderCapability struct {
    // ...
    ContentFormats   []string // html, markdown, plain, json
}
```

`DeliveryPlanner.selectFormat` resolves in this order: if the binding explicitly sets `format`, use it; otherwise take the first entry of `ContentFormats`; if neither exists, `plain`. Built-in Provider defaults: Email `["html", "plain"]`, Telegram `["markdown", "plain"]`, Feishu `["plain"]`.

### How Different Providers Are Handled

The planner generates the delivery payload according to the `PayloadKinds` the Provider declares, with three values (the wire values in `core/types.go`): `content` (the title + body rendered by Herald), `provider_template` (a vendor template), and `raw` (raw fields as a fallback).

| Provider type | PayloadKind | Data source |
|-------------|---------|---------|
| **Email** | content | `task.Payload.Content` (Title/Body/Format) |
| **SMS** | provider_template | `task.Payload.ProviderTemplate.TemplateCode` |
| **IM (Feishu/Telegram)** | content | `task.Payload.Content` |
| **Webhook** | content, raw fallback | `task.Payload.Content`, falling back to `Payload.Raw` when there is no rendered data |

### Special Handling for SMS Providers

SMS Providers use the vendor's template system and do not take part in Herald template rendering:

```go
// SMS provider receives vendor template payload from DeliveryPlanner
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
    templateCode := task.Payload.ProviderTemplate.TemplateCode
    templateParams := task.Payload.ProviderTemplate.Params
    // send directly to the vendor
}
```

## Data Flow

### Full Flow

```
1. User request
   { template: "alert", params: {Level: "ERROR"} }

2. Manager.Render()
   Template { Title: "[<code v-pre>{{.Level}}</code>] Alert" }
   → RenderedData { Title: "[ERROR] Alert", Fields: [...] }

3. DeliveryPlanner.selectFormat()
   Email → ContentFormats[0] = "html"
   Telegram → ContentFormats[0] = "markdown"

4. Renderer.Render()
   HTMLRenderer.Render(RenderedData) → "<html>...</html>"

5. Create the Payload
   Payload.Content { Title, Body: "<html>...</html>", Format: "html" }

6. Provider.Deliver()
   Email: send an HTML email according to Payload.Content.Format
```

## Design Principles

### 1. Channel Independence

A template definition contains only semantic data, none of the format of any particular channel.

### 2. Separation of Rendering

Variable substitution happens in the template engine, format conversion in the renderer, and actual sending in the Provider; the three layers do not interfere with one another.

### 3. Backward Compatibility

```go
// direct send (without a template)
POST /api/v1/notify
{
  "title": "Direct title",
  "body": "Direct content",
  "channels": ["telegram"]
}

// template send
POST /api/v1/notify
{
  "template": "my_template",
  "params": {...},
  "channels": ["telegram"]
}
```

## Extensibility

### Adding a New Renderer

```go
type CustomRenderer struct{}

func (r *CustomRenderer) Format() RenderFormat {
    return "custom"
}

func (r *CustomRenderer) Render(ctx context.Context, data *RenderedData) (interface{}, error) {
    // custom rendering logic
}

// register
template.Registry[RenderFormat("custom")] = &CustomRenderer{}
```

### Providers Declaring Format Support

Declare it in the Capability returned by the Factory; the first item in the list is the default format:

```go
PayloadKinds:   []core.PayloadKind{core.PayloadContent},
ContentFormats: []string{"html", "plain"},
```
