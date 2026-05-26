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
│  │ { templateId, params, renderAs, channels }               │   │
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
│  │                    Handler.chooseFormat()                 │   │
│  │  ┌────────────────────────────────────────────────────┐  │   │
│  │  │ Provider implements FormattableProvider?            │  │   │
│  │  │   YES → 使用 Provider 声明的格式                     │  │   │
│  │  │   NO  → 使用 Plain Text                             │  │   │
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
    ID        string      // 模板唯一标识
    Name      string      // 模板名称
    Title     string      // 标题模板（支持变量）
    Level     string      // 默认级别
    Fields    []Field     // 字段列表
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

func (e *Engine) Render(tmpl *Template, params map[string]interface{}) *RenderedData
```

**职责**：

- 执行变量替换（`{{.Variable}}`）
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

### FormattableProvider 接口

支持格式选择的 Provider 实现：

```go
type FormattableProvider interface {
    SupportedFormats() []string
    DefaultFormat() string
}
```

### 不同 Provider 的处理方式

| Provider 类型 | 渲染方式 | 数据来源 |
|-------------|---------|---------|
| **Email** | Handler 渲染 | `task.Body` + `task.RenderFormat` |
| **SMS** | 服务商渲染 | `task.Data["template_code"]` |
| **IM (Feishu/Telegram)** | Handler 渲染 | `task.Body` |
| **Webhook** | Handler 渲染 | `task.Body` |

### SMS Provider 特殊处理

SMS Provider 使用服务商的模板系统，不参与 Herald 模板渲染：

```go
// aliyunsms provider
func (p *Provider) Deliver(ctx context.Context, task *core.Task) error {
    templateCode := task.Data["template_code"].(string)
    templateParams := task.Data["template_params"].(map[string]interface{})
    // 直接发送给服务商
}
```

## 数据流转

### 完整流程

```
1. 用户请求
   { templateId: "alert", params: {Level: "ERROR"} }

2. Manager.Render()
   Template { Title: "【{{.Level}}】告警" }
   → RenderedData { Title: "【ERROR】告警", Fields: [...] }

3. Handler.chooseFormat()
   Provider: Email → FormattableProvider.DefaultFormat() = "html"
   Provider: Telegram → 不实现接口 → "plain"

4. Renderer.Render()
   HTMLRenderer.Render(RenderedData) → "<html>...</html>"

5. 创建 Task
   Task { Body: "<html>...</html>", RenderFormat: "html" }

6. Provider.Deliver()
   Email: 读取 RenderFormat，发送 HTML 邮件
```

## 设计原则

### 1. 渠道无关性

模板定义只包含语义化数据，不包含特定渠道的格式。

### 2. 渲染分离

- **模板引擎**：处理变量替换
- **渲染器**：处理格式转换
- **Provider**：处理实际发送

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
  "templateId": "my_template",
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

```go
func (p *Provider) SupportedFormats() []string {
    return []string{"html", "plain"}
}

func (p *Provider) DefaultFormat() string {
    return "html"
}
```
