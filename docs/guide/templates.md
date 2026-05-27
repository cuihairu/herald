# 模板系统

## 概述

Herald 的模板系统是一个**与渠道无关**的消息格式化解决方案，允许你：

- 定义一次模板，多渠道复用
- 使用变量进行参数替换
- 根据渠道自动选择最佳渲染格式

## 基本用法

### 1. 定义模板

在 `config.yaml` 中定义模板：

```yaml
templates:
  server_alert:
    name: "服务器告警"
    title: "【{{.Level}}】{{.Service}} 服务异常"
    level: "error"
    fields:
      - label: "服务器"
        value: "{{.Server}}"
        type: "text"
      - label: "错误信息"
        value: "{{.Error}}"
        type: "text"
      - label: "时间"
        value: "{{.Timestamp}}"
        type: "datetime"

  deploy_notify:
    name: "部署通知"
    title: "部署完成: {{.Env}} 环境"
    level: "info"
    fields:
      - label: "环境"
        value: "{{.Env}}"
      - label: "版本"
        value: "{{.Version}}"
      - label: "分支"
        value: "{{.Branch}}"
```

### 2. 使用模板发送消息

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "templateId": "server_alert",
    "params": {
      "Level": "CRITICAL",
      "Service": "order-service",
      "Server": "order-01",
      "Error": "CPU 使用率 95%",
      "Timestamp": "2026-05-27 14:30:00"
    },
    "renderAs": "html",
    "channels": ["email", "feishu"]
  }'
```

## 渲染格式

支持多种渲染格式，系统会自动根据 Provider 能力选择最佳格式：

| 格式 | 说明 | 适用场景 |
|------|------|---------|
| `auto` | 自动选择 | 默认，根据 Provider 能力选择 |
| `html` | HTML 格式 | 邮件、富文本消息 |
| `markdown` | Markdown 格式 | Telegram、GitHub 等 |
| `plain` | 纯文本 | 通用文本消息 |
| `json` | JSON 格式 | 飞书卡片、钉钉卡片 |

### 渲染格式选择流程

```
用户请求 renderAs
        ↓
Handler.chooseFormat()
        ↓
┌───────────────────────────────┐
│ Provider 实现 FormattableProvider? │
└───────────────────────────────┘
        ↓ Yes              ↓ No
  使用 Provider 声明      使用 Plain Text
  的支持格式
```

## 变量语法

模板使用 Go template 语法，支持：

### 基本变量

```yaml
title: "服务告警：{{.Service}}"
```

### 管道操作

```yaml
title: "服务告警：{{.Service | toUpper}}"
```

### 条件判断

```yaml
title: "{{if .Urgent}}【紧急】{{end}}{{.Title}}"
```

### 内置函数

| 函数 | 说明 |
|------|------|
| `toUpper` | 转大写 |
| `toLower` | 转小写 |
| `trim` | 去除首尾空格 |

## API 管理

### 创建模板

```bash
curl -X POST http://localhost:8080/api/v1/templates \
  -H "Content-Type: application/json" \
  -d '{
    "id": "custom_alert",
    "name": "自定义告警",
    "title": "告警：{{.Title}}",
    "level": "warning",
    "fields": [
      { "label": "详情", "value": "{{.Detail}}", "type": "text" }
    ]
  }'
```

### 获取模板列表

```bash
curl http://localhost:8080/api/v1/templates
```

### 获取单个模板

```bash
curl http://localhost:8080/api/v1/templates/server_alert
```

### 更新模板

```bash
curl -X PUT http://localhost:8080/api/v1/templates/server_alert \
  -H "Content-Type: application/json" \
  -d '{
    "name": "服务器告警（已更新）",
    "title": "【{{.Level}}】{{.Service}}"
  }'
```

### 删除模板

```bash
curl -X DELETE http://localhost:8080/api/v1/templates/server_alert
```

## SMS Provider 特殊处理

SMS Provider（阿里云、腾讯云等）使用**服务商提供的模板系统**：

```yaml
providers:
  aliyunsms:
    access_key_id: "${ALIYUN_ACCESS_KEY_ID}"
    access_key_secret: "${ALIYUN_ACCESS_KEY_SECRET}"
    sign_name: "Herald"
```

发送 SMS 时，直接传递服务商的模板 ID：

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "title": "验证码",
    "body": "",
    "level": "info",
    "channels": ["aliyunsms"],
    "data": {
      "template_code": "SMS_123456789",
      "template_params": {
        "code": "123456",
        "product": "Herald"
      }
    },
    "target": "13800138000"
  }'
```

## 渠道差异化渲染

同一个模板，不同渠道自动渲染为最佳格式：

| 模板数据 | Email | Telegram | Feishu |
|---------|-------|----------|--------|
| Title + Fields | HTML 表格 | Markdown 文本 | 富文本消息 |
| renderAs: json | 不支持 | 不支持 | 卡片消息 |

## 最佳实践

1. **模板定义**：使用语义化的字段定义，而非特定渠道的格式
2. **变量命名**：使用驼峰命名，如 `&#123;&#123;.ServiceName&#125;&#125;`
3. **级别设置**：合理设置 `level` 以配合路由规则
4. **测试验证**：在不同渠道测试模板渲染效果
