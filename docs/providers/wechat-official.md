# 微信公众号推送

本指南介绍如何使用自己的微信公众号实现消息推送。

## 快速开始

### 1. 配置 Worker

使用 Worker SDK 开发微信公众号 Worker：

```yaml
providers:
  wechatmp:
    type: worker
    enabled: true
    config:
      target: "wechat-worker-01"
```

### 2. 发送消息

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "order",
    "title": "订单通知",
    "body": "您的订单已发货",
    "level": "info",
    "channels": ["wechatmp"],
    "recipients": {
      "wechatmp": ["用户OpenID"]
    }
  }'
```

## 前置要求

### 企业资质

- ✅ 营业执照
- ✅ 对公账户
- ✅ 300元/年认证费
- ⏱ 审核周期：7-30个工作日

### 技术要求

- ✅ 公网可访问的服务器
- ✅ 已备案的域名
- ✅ SSL 证书（HTTPS）
- ✅ 服务器端口开放（通常 443）

## 申请流程

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│ 注册微信服务号 │ → │   企业认证   │ │ →  开通模板消息  │
│  (7天审核)   │    │  (7-30天)   │    │  (认证后可用) │
└─────────────┘    └─────────────┘    └─────────────┘
```

### 步骤 1: 注册服务号

1. 访问 https://mp.weixin.qq.com/
2. 点击"立即注册"
3. 选择"服务号"（不是订阅号）
4. 填写企业信息
5. 等待审核

### 步骤 2: 企业认证

1. 登录公众号后台
2. 进入"设置与开发" → "基本配置"
3. 点击"微信认证"
4. 上传营业执照、对公账户信息
5. 缴纳认证费用（300元/年）
6. 等待审核

### 步骤 3: 开通模板消息

1. 进入"功能" → "模板消息"
2. 点击"模板库"
3. 选用或申请新模板
4. 记录模板ID（类似 `AT0001`）

## Worker 开发

### Worker 配置

```yaml
# Worker 端配置
worker_id: "wechat-worker-01"
core_url: "ws://localhost:8081"

# 微信公众号配置
wechat:
  app_id: "${WECHATMP_APP_ID}"
  app_secret: "${WECHATMP_APP_SECRET}"
  template_id: "${WECHATMP_TEMPLATE_ID}"
  default_url: "https://your-domain.com"
```

### Worker 实现

使用 Go SDK 开发 Worker：

```go
package main

import (
    "context"
    workersdk "github.com/cuihairu/herald/worker-sdk/go"
    "github.com/cuihairu/herald/protocol"
)

func main() {
    config := &protocol.WorkerConfig{
        WorkerID:          "wechat-worker-01",
        CoreURL:           "ws://localhost:8081",
        ReconnectDelay:    5 * time.Second,
        HeartbeatInterval: 30 * time.Second,
        Capabilities:      []string{"wechatmp"},
    }

    client := workersdk.NewClient(config)

    client.OnTask(func(task *protocol.DispatchMessage) error {
        // 处理任务
        err := sendWechatTemplateMessage(task)

        // 确认任务
        client.Ack(task.TaskID, err == nil, "")
        return err
    })

    client.Connect(context.Background())
    select {}
}
```

### 发送模板消息

```go
type TemplateMessageRequest struct {
    ToUser     string                 `json:"touser"`
    TemplateID string                 `json:"template_id"`
    URL        string                 `json:"url,omitempty"`
    Data       map[string]TemplateData `json:"data"`
}

type TemplateData struct {
    Value string `json:"value"`
    Color string `json:"color,omitempty"`
}

func sendWechatTemplateMessage(task *protocol.DispatchMessage) error {
    // 1. 获取 Access Token
    accessToken, err := getAccessToken()
    if err != nil {
        return err
    }

    // 2. 构造请求
    req := &TemplateMessageRequest{
        ToUser:     task.Target,
        TemplateID: getTemplateID(),
        Data:       buildTemplateData(task),
    }

    // 3. 发送
    url := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/message/template/send?access_token=%s", accessToken)
    return http.Post(url, "application/json", toJSON(req))
}
```

## 服务器配置

### 步骤 1: 域名与服务器

```
                    ┌─────────────────┐
                    │   你的服务器     │
                    │  (公网IP + 域名)  │
┌──────────────┐     │  443端口 (HTTPS)│
│ 微信服务器    │◄────┤─────────────────┤
└──────────────┘     │                 │
                     │  /wechat/callback│
                     └─────────────────┘
```

### 步骤 2: 配置服务器地址

1. 进入"开发" → "基本配置"
2. 填写服务器配置：

| 配置项 | 说明 | 示例 |
|--------|------|------|
| URL | 服务器地址 | `https://api.example.com/wechat/callback` |
| Token | 自定义令牌 | `your_custom_token` |
| EncodingAESKey | 消息加密密钥 | 随机生成或自定义 |
| 消息加密方式 | 安全模式 | 推荐"安全模式" |

### 步骤 3: 服务器验证代码

```go
func HandleWechatCallback(w http.ResponseWriter, r *http.Request) {
    signature := r.URL.Query().Get("signature")
    timestamp := r.URL.Query().Get("timestamp")
    nonce := r.URL.Query().Get("nonce")
    echostr := r.URL.Query().Get("echostr")
    token := "your_custom_token"

    params := []string{token, timestamp, nonce}
    sort.Strings(params)
    joined := strings.Join(params, "")

    h := sha1.New()
    h.Write([]byte(joined))
    hashed := hex.EncodeToString(h.Sum(nil))

    if hashed == signature {
        w.Write([]byte(echostr))
        return
    }

    w.WriteHeader(http.StatusUnauthorized)
}
```

## 获取用户 OpenID

用户关注后需要获取 OpenID 才能发送消息：

### 通过关注事件

```go
type FollowEvent struct {
    ToUserName   string `xml:"ToUserName"`
    FromUserName string `xml:"FromUserName"` // 用户 OpenID
    CreateTime   int64  `xml:"CreateTime"`
    Event        string `xml:"Event"`
}

func HandleFollowEvent(body []byte) string {
    var event FollowEvent
    xml.Unmarshal(body, &event)

    if event.Event == "subscribe" {
        saveUserID(event.FromUserName)
    }

    return fmt.Sprintf(`
        <xml>
            <ToUserName><![CDATA[%s]]></ToUserName>
            <FromUserName><![CDATA[%s]]></FromUserName>
            <CreateTime>%d</CreateTime>
            <MsgType><![CDATA[text]]></MsgType>
            <Content><![CDATA[欢迎关注！]]></Content>
        </xml>
    `, event.FromUserName, event.ToUserName, time.Now().Unix())
}
```

## 限制说明

| 限制项 | 说明 |
|--------|------|
| **推送频率** | 单用户每分钟 100 条 |
| **模板数量** | 最多 25 个模板 |
| **模板审核** | 需要微信审核（1-7天） |
| **行业限制** | 需要与公众号行业匹配 |
| **关键词** | 内容不能包含敏感词 |

## 常见问题

### Q: 模板被拒怎么办？

A: 根据拒绝原因修改：
- 内容不符合行业规范
- 包含推广信息
- 模板格式不规范

### Q: 用户收不到消息？

A: 检查：
- 用户是否已关注公众号
- OpenID 是否正确
- 模板是否已通过审核
- 是否触发了频率限制

### Q: Worker 连接失败？

A: 检查：
- WebSocket 地址是否正确（`ws://localhost:8081`）
- Herald Core 是否运行
- 网络是否可达

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider
- [微信个人推送](./wechat.md) - 使用第三方服务的简单方案
- [Worker SDK](/runtime/sdk) - 了解 Worker 开发
