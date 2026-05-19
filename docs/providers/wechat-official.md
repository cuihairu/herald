# 自建微信公众号推送

本指南介绍如何使用自己的微信公众号实现消息推送。

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

3. 点击"提交"验证服务器

### 步骤 3: 服务器验证代码

```go
// wechat.go
package main

import (
    "crypto/sha1"
    "encoding/hex"
    "fmt"
    "net/http"
    "sort"
    "strings"
)

func HandleWechatCallback(w http.ResponseWriter, r *http.Request) {
    signature := r.QueryGet("signature")
    timestamp := r.QueryGet("timestamp")
    nonce := r.QueryGet("echostr")
    token := "your_custom_token" // 与后台配置一致

    // 1. 将 token、timestamp、nonce 三个参数进行字典序排序
    params := []string{token, timestamp, nonce}
    sort.Strings(params)

    // 2. 将三个参数字符串拼接成一个字符串进行 sha1 加密
    joined := strings.Join(params, "")
    h := sha1.New()
    h.Write([]byte(joined))
    hashed := hex.EncodeToString(h.Sum(nil))

    // 3. 加密后的字符串与 signature 对比
    if hashed == signature {
        w.Write([]byte(echostr))
        return
    }

    w.WriteHeader(http.StatusUnauthorized)
}
```

## 获取 Access Token

模板消息 API 需要 Access Token：

```go
type AccessTokenResponse struct {
    ErrCode     int    `json:"errcode"`
    ErrMsg      string `json:"errmsg"`
    AccessToken string `json:"access_token"`
    ExpiresIn   int    `json:"expires_in"`
}

func GetAccessToken(appID, appSecret string) (string, error) {
    url := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/token?grant_type=client_credential&appid=%s&secret=%s", 
        appID, appSecret)
    
    resp, err := http.Get(url)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var result AccessTokenResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", err
    }

    if result.ErrCode != 0 {
        return "", fmt.Errorf("wechat error: %s", result.ErrMsg)
    }

    return result.AccessToken, nil
}
```

## 发送模板消息

### 请求格式

```go
type TemplateMessageRequest struct {
    ToUser     string                 `json:"touser"`      // 用户 OpenID
    TemplateID string                 `json:"template_id"`  // 模板ID
    Page       string                 `json:"page,omitempty"`
    Data       map[string]TemplateData `json:"data"`
}

type TemplateData struct {
    Value string `json:"value"`
    Color string `json:"color,omitempty"`
}
```

### 发送代码

```go
func SendTemplateMessage(accessToken string, msg *TemplateMessageRequest) error {
    url := fmt.Sprintf("https://api.weixin.qq.com/cgi-bin/message/template/send?access_token=%s", accessToken)
    
    resp, err := http.Post(url, "application/json", toJSON(msg))
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    var result struct {
        ErrCode int    `json:"errcode"`
        ErrMsg  string `json:"errmsg"`
        MsgID   int64  `json:"msgid"`
    }
    
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return err
    }

    if result.ErrCode != 0 {
        return fmt.Errorf("wechat error: %s", result.ErrMsg)
    }

    return nil
}
```

## 获取用户 OpenID

用户关注后需要获取 OpenID 才能发送消息：

### 方式 1: 通过用户授权

```go
// 生成授权链接
authURL := fmt.Sprintf("https://open.weixin.qq.com/connect/oauth2/authorize?appid=%s&redirect_uri=%s&response_type=code&scope=snsapi_base#wechat_redirect",
    appID, url.QueryEscape("https://your-domain.com/callback"))
```

### 方式 2: 通过关注事件

```go
type FollowEvent struct {
    ToUserName   string `xml:"ToUserName"`
    FromUserName string `xml:"FromUserName"` // 这是用户的 OpenID
    CreateTime   int64  `xml:"CreateTime"`
    Event        string `xml:"Event"`
    EventKey     string `xml:"EventKey"`
}

func HandleFollowEvent(body []byte) (string, error) {
    var event FollowEvent
    if err := xml.Unmarshal(body, &event); err != nil {
        return "", err
    }
    
    if event.Event == "subscribe" {
        // 保存用户的 OpenID 到数据库
        saveUserID(event.FromUserName)
    }
    
    // 返回欢迎消息
    return fmt.Sprintf(`
        <xml>
            <ToUserName><![CDATA[%s]]></ToUserName>
            <FromUserName><![CDATA[%s]]></FromUserName>
            <CreateTime>%d</CreateTime>
            <MsgType><![CDATA[text]]></MsgType>
            <Content><![CDATA[欢迎关注！]]></Content>
        </xml>
    `, event.FromUserName, event.ToUserName, time.Now().Unix()), nil
}
```

## 配置示例

### config.yaml

```yaml
providers:
  wechat-official:
    type: builtin
    enabled: true
    config:
      app_id: "${WECHAT_APP_ID}"
      app_secret: "${WECHAT_APP_SECRET}"
      template_id: "${WECHAT_TEMPLATE_ID}"
      # 可选：默认跳转链接
      default_url: "https://your-domain.com"
```

### .env

```bash
# 微信服务号配置
WECHAT_APP_ID=wx1234567890abcdef
WECHAT_APP_SECRET=abcdef1234567890abcdef
WECHAT_TEMPLATE_ID=AT0001
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

### Q: Access Token 过期？

A: Access Token 有效期 2 小时，建议缓存并自动刷新：

```go
type TokenCache struct {
    token      string
    expireTime time.Time
    mu         sync.RWMutex
}

func (c *TokenCache) GetToken() (string, error) {
    c.mu.RLock()
    if time.Now().Before(c.expireTime) {
        defer c.mu.RUnlock()
        return c.token, nil
    }
    c.mu.RUnlock()
    
    // 重新获取
    c.mu.Lock()
    defer c.mu.Unlock()
    
    token, err := GetAccessToken(appID, appSecret)
    if err != nil {
        return "", err
    }
    
    c.token = token
    c.expireTime = time.Now().Add(2 * time.Hour - 5 * time.Minute)
    return c.token, nil
}
```

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider
- [微信个人推送](./wechat.md) - 使用第三方服务的简单方案
