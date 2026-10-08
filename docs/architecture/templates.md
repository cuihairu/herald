# 模板系统架构

## 设计目标

Herald 模板系统的核心目标是**消息格式与发送渠道解耦**：

- 一次定义，多渠道复用
- 运行时管理（CRUD API）
- 自动渠道适配

## 架构概览

```
┌─────────────────────────────────────────────────────────────────┐
│                        模板系统架构                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  用户请求                                                        │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ { template, params, channels }                            │   │
│  └──────────────────────────────────────────────────────────┘   │
│                          ↓                                      │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    Template Manager                       │   │
│  │  ┌────────────────────────────────────────────────────┐  │   │
│  │  │ 1. Get Template by ID                              │  │   │
│  │  │ 2. Engine.Render() → RenderedData                  │  │   │
│  │  │    (变量替换，得到语义化数据)                         │  │   │
│  │  └────────────────────────────────────────────────────┘  │   │
│  └──────────────────────────────────────────────────────────┘   │
│                          ↓                                      │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │              DeliveryPlanner.buildPayload()               │   │
│  │  ┌────────────────────────────────────────────────────┐  │   │
│  │  │ binding 带 provider 模板且 Provider 支持 →           │  │   │
│  │  │   PayloadProviderTemplate（服务商渲染）              │  │   │
│  │  │ 否则 → selectFormat() 选格式再渲染：                 │  │   │
│  │  │   binding.format > ContentFormats[0] > plain        │  │   │
│  │  └────────────────────────────────────────────────────┘  │   │
│  └──────────────────────────────────────────────────────────┘   │
│                          ↓                                      │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                    Renderer                               │   │
│  │  ┌─────────────┬─────────────┬─────────────┬──────────┐  │   │
│  │  │ HTMLRenderer│MDRenderer   │PlainRenderer│JSONRenderer│ │   │
│  │  └─────────────┴─────────────┴─────────────┴──────────┘  │   │
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
│  │  │ RenderFmt│ template_code │Body │  Body (已渲染)    │  │   │
│  │  └──────────┴──────────┴──────────┴──────────────────┘  │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## 核心组件

### 1. Template（模板定义）

```go
type Template struct {
    ID        string             // 模板唯一标识
    Name      string             // 模板名称
    Title     string             // 标题模板（支持变量）
    Level     string             // 默认级别
    Fields    []Field            // 字段列表
    Bindings  map[string]Binding // 每渠道适配配置（format / template_code / ...）
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

**设计要点**：

- `Title` 和 `Fields.Value` 支持变量替换（Go template 语法）
- `Fields` 是语义化字段，不包含具体格式
- `Level` 用于配合路由规则

### 2. Engine（模板引擎）

```go
type Engine struct {
    templates map[string]*template.Template
    funcMap   template.FuncMap
}

func (e *Engine) Render(tmpl *Template, params map[string]interface{}) (*RenderedData, error)
```

**职责**：

- 执行变量替换（`&#123;&#123;.Variable&#125;&#125;`）
- 内置函数：`toUpper`、`toLower`、`trim`
- 返回 `RenderedData`（语义化数据，与渠道无关）

### 3. Renderer（渲染器）

```go
type Renderer interface {
    Format() RenderFormat
    Render(ctx context.Context, data *RenderedData) (interface{}, error)
}
```

**实现**：

| Renderer | 格式 | 输出示例 |
|----------|------|---------|
| HTMLRenderer | HTML | `<table>...</table>` |
| MarkdownRenderer | Markdown | `### Title\n\n**Key**: Value` |
| PlainRenderer | 纯文本 | `Title\n\nKey: Value` |
| JSONRenderer | JSON | `{"card": {...}}` |

### 4. Manager（模板管理器）

```go
type Manager struct {
    templates map[string]*Template
    engine    *Engine
}

func (m *Manager) Register(tmpl *Template) error
func (m *Manager) Get(id string) (*Template, error)
func (m *Manager) Render(id string, params map[string]interface{}) (*RenderedData, error)
```

## Provider 适配

### 格式选择（selectFormat）

Provider 通过 Capability 里的 `ContentFormats` 字段声明支持的格式，没有独立接口：

```go
// core/provider.go
type ProviderCapability struct {
    // ...
    ContentFormats   []string // html, markdown, plain, json
}
```

`DeliveryPlanner.selectFormat` 的取值顺序：binding 显式写了 `format` 就用它；否则取 `ContentFormats` 的第一项；两者都没有则 `plain`。内置 Provider 的默认值：Email `["html", "plain"]`、Telegram `["markdown", "plain"]`、飞书 `["plain"]`。

### 不同 Provider 的处理方式

planner 按 Provider 声明的 `PayloadKinds` 生成投递载荷，三种取值：`payload_content`（Herald 渲染的标题+正文）、`payload_provider_template`（服务商模板）、`payload_raw`（原始字段兜底）。

| Provider 类型 | PayloadKind | 数据来源 |
|-------------|---------|---------|
| **Email** | payload_content | `task.Payload.Content`（Title/Body/Format） |
| **SMS** | payload_provider_template | `task.Payload.ProviderTemplate.TemplateCode` |
| **IM (Feishu/Telegram)** | payload_content | `task.Payload.Content` |
| **Webhook** | payload_content，raw 兜底 | `task.Payload.Content`，无渲染数据时退 `Payload.Raw` |

### SMS Provider 特殊处理

SMS Provider 使用服务商的模板系统，不参与 Herald 模板渲染：

```go
// SMS provider receives vendor template payload from DeliveryPlanner
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
    templateCode := task.Payload.ProviderTemplate.TemplateCode
    templateParams := task.Payload.ProviderTemplate.Params
    // 直接发送给服务商
}
```

## 数据流转

### 完整流程

```
1. 用户请求
   { template: "alert", params: {Level: "ERROR"} }

2. Manager.Render()
   Template { Title: "【<code v-pre>{{.Level}}</code>】告警" }
   → RenderedData { Title: "【ERROR】告警", Fields: [...] }

3. DeliveryPlanner.selectFormat()
   Email → ContentFormats[0] = "html"
   Telegram → ContentFormats[0] = "markdown"

4. Renderer.Render()
   HTMLRenderer.Render(RenderedData) → "<html>...</html>"

5. 创建 Payload
   Payload.Content { Title, Body: "<html>...</html>", Format: "html" }

6. Provider.Deliver()
   Email: 按 Payload.Content.Format 发送 HTML 邮件
```

## 设计原则

### 1. 渠道无关性

模板定义只包含语义化数据，不包含特定渠道的格式。

### 2. 渲染分离

变量替换在模板引擎，格式转换在渲染器，实际发送在 Provider，三层互不掺和。

### 3. 向后兼容

```go
// 直接发送（不使用模板）
POST /api/v1/notify
{
  "title": "直接标题",
  "body": "直接内容",
  "channels": ["telegram"]
}

// 模板发送
POST /api/v1/notify
{
  "template": "my_template",
  "params": {...},
  "channels": ["telegram"]
}
```

## 扩展性

### 添加新渲染器

```go
type CustomRenderer struct{}

func (r *CustomRenderer) Format() RenderFormat {
    return "custom"
}

func (r *CustomRenderer) Render(ctx context.Context, data *RenderedData) (interface{}, error) {
    // 自定义渲染逻辑
}

// 注册
template.Registry[RenderFormat("custom")] = &CustomRenderer{}
```

### Provider 声明格式支持

在 Factory 返回的 Capability 里声明，列表第一项是默认格式：

```go
PayloadKinds:   []core.PayloadKind{core.PayloadContent},
ContentFormats: []string{"html", "plain"},
```
