> English · [中文](./templates.md)

# Template System

## Overview

Templates are channel-agnostic: content is defined once and reused across channels, <code v-pre>{{.xxx}}</code> variables are substituted at send time, and the rendering format is chosen automatically from each channel's default.

## Basic Usage

### 1. Define a Template

Define templates in `config.yaml`:

```yaml
templates:
  server_alert:
    name: "Server Alert"
    title: "【{{.Level}}】{{.Service}} service abnormal"
    level: "error"
    fields:
      - label: "Server"
        value: "{{.Server}}"
        type: "text"
      - label: "Error"
        value: "{{.Error}}"
        type: "text"
      - label: "Time"
        value: "{{.Timestamp}}"
        type: "datetime"

  deploy_notify:
    name: "Deploy Notification"
    title: "Deploy finished: {{.Env}} environment"
    level: "info"
    fields:
      - label: "Environment"
        value: "{{.Env}}"
      - label: "Version"
        value: "{{.Version}}"
      - label: "Branch"
        value: "{{.Branch}}"
```

### 2. Send a Message Using a Template

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "template": "server_alert",
    "params": {
      "Level": "CRITICAL",
      "Service": "order-service",
      "Server": "order-01",
      "Error": "CPU usage 95%",
      "Timestamp": "2026-05-27 14:30:00"
    },
    "channels": ["email", "feishu"]
  }'
```

## Rendering Formats

| Format | Description | Suitable For |
|------|------|---------|
| `html` | HTML format | Email |
| `markdown` | Markdown format | Telegram, etc. |
| `plain` | Plain text | Generic text messages |
| `json` | JSON format | Feishu cards, DingTalk cards |

Selection rule (`DeliveryPlanner.selectFormat`): if the binding explicitly sets `format`, use it; otherwise take the first entry of the `ContentFormats` the Provider declares; if neither exists, `plain`. Default formats of the built-in providers: Email `html`, Telegram `markdown`, Feishu `plain` (to get a Feishu card you must set `format: json` in the binding).

## Variable Syntax

Templates use Go template syntax and support:

### Basic Variables

```yaml
title: "Service alert: {{.Service}}"
```

### Pipelines

```yaml
title: "Service alert: {{.Service | toUpper}}"
```

### Conditionals

```yaml
title: "{{if .Urgent}}【URGENT】{{end}}{{.Title}}"
```

### Built-in Functions

| Function | Description |
|------|------|
| `toUpper` | Convert to uppercase |
| `toLower` | Convert to lowercase |
| `trim` | Trim leading and trailing whitespace |

## API Management

### Create a Template

```bash
curl -X POST http://localhost:8080/api/v1/templates/create \
  -H "Content-Type: application/json" \
  -d '{
    "id": "custom_alert",
    "name": "Custom Alert",
    "title": "Alert: {{.Title}}",
    "level": "warning",
    "fields": [
      { "label": "Detail", "value": "{{.Detail}}", "type": "text" }
    ]
  }'
```

### List Templates

```bash
curl http://localhost:8080/api/v1/templates
```

### Get a Single Template

```bash
curl http://localhost:8080/api/v1/templates/server_alert
```

### Update a Template

```bash
curl -X PUT http://localhost:8080/api/v1/templates/server_alert \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Server Alert (updated)",
    "title": "【{{.Level}}】{{.Service}}"
  }'
```

### Delete a Template

```bash
curl -X DELETE http://localhost:8080/api/v1/templates/server_alert
```

## SMS Provider Special Handling

SMS providers (Aliyun, Tencent, etc.) use the **template system provided by the service provider**; Template Binding lets you define once and adapt to multiple channels:

```yaml
templates:
  verify_code:
    name: "Verification code"
    title: "Verification code notification"
    fields:
      - label: "code"
        value: "{{.Code}}"
        type: "text"
      - label: "product"
        value: "{{.Product}}"
        type: "text"
    bindings:
      aliyunsms:
        template_code: "SMS_123456789"
        params:
          code: "code"
          product: "product"
      tencentsms:
        template_id: "123456"
        param_order: ["code", "product"]
```

At send time you only specify the template and the parameters; DeliveryPlanner automatically builds the payload the service provider needs from the Binding:

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "template": "verify_code",
    "params": {
      "Code": "123456",
      "Product": "Herald"
    },
    "channels": ["aliyunsms"],
    "recipients": {
      "aliyunsms": ["13800138000"]
    }
  }'
```

## Channel-Specific Rendering

The same template is rendered by each channel in its own default format from `ContentFormats`:

| Template data | Email (default html) | Telegram (default markdown) | Feishu (default plain) |
|---------|-------|----------|--------|
| Title + Fields | HTML table | Markdown text | Plain text |
| Binding format: json | Not the default; must be set explicitly | Not the default; must be set explicitly | Card message |

## Best Practices

Use semantic field names (<code v-pre>{{.ServiceName}}</code> over <code v-pre>{{.s1}}</code>); do not mix channel-specific formatting into the template body; `level` must be set correctly because routing rules match on it; before going live, send one message through each target channel and verify how it renders.
