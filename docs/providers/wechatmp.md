# 微信公众号模板消息

通过微信公众号模板消息接口向已关注用户（OpenID）发送服务通知。

## 作用

`wechatmp` Builtin Provider 直接调用微信公众平台模板消息 API（`/cgi-bin/message/template/send`），把 Herald 的通知内容推送到用户的微信服务通知列表。适合订单状态、账单提醒、审批流转等服务通知场景。

> ⚠️ **前置要求**：需**微信服务号**且已通过**微信认证**（企业资质、对公账户、300元/年认证费、审核 7-30 工作日），并在后台开通“模板消息”功能、创建并通过审核模板。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `app_id` | ✅ | 公众号 AppID（开发者 ID） | 无 |
| `app_secret` | ✅ | 公众号 AppSecret（开发者密码） | 无 |
| `template_id` | ✅ | 模板消息 ID（模板库中添加模板后获得） | 无 |
| `default_url` | ❌ | 点击模板消息跳转的默认链接 | 空 |

缺 `app_id`、`app_secret` 或 `template_id` 时 Provider 创建即失败（`wechatmp: app_id is required` 等），启动日志可见。

## 配置示例

```yaml
providers:
  wechatmp:
    type: wechatmp
    enabled: true
    config:
      app_id: "$WECHATMP_APP_ID"
      app_secret: "$WECHATMP_APP_SECRET"
      template_id: "$WECHATMP_TEMPLATE_ID"
      default_url: "https://your-domain.com"   # 可选
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `WECHATMP_APP_ID` | `app_id` | 公众号 AppID |
| `WECHATMP_APP_SECRET` | `app_secret` | 公众号 AppSecret |
| `WECHATMP_TEMPLATE_ID` | `template_id` | 模板消息 ID |

## 消息模板与限制

### 模板字段映射

Herald 自动将通知内容映射到微信模板字段：

| 微信字段 | Herald 内容 | 说明 |
|---------|------------|------|
| `thing1` | `title` | 标题（最多 20 字符，超长截断） |
| `thing2` | `body` | 正文（最多 30 字符，超长截断） |
| `character_string1` | `level` | 级别（如 `info`/`warning`/`error`） |
| `time3` | 当前时间 | 发送时的时间戳（`2006-01-02 15:04:05`） |

> 任务级的 `provider_template.template_id` 若存在，会覆盖 Provider 配置的默认 `template_id`。

### 限制说明

| 限制项 | 说明 |
|--------|------|
| **推送频率** | 单用户每分钟 100 条 |
| **模板数量** | 最多 25 个模板 |
| **模板审核** | 需要微信审核（1-7 天） |
| **行业限制** | 模板需与公众号设置的行业匹配 |
| **关键词** | 内容不能包含敏感词 |
| **字段长度** | `thing1` ≤ 20、`thing2` ≤ 30，超长自动截断 |

### 获取用户 OpenID

用户关注公众号后需要获取 OpenID 才能发送消息：

- **方式 1：用户授权**  
  生成授权链接让用户授权（静默授权 `snsapi_base`）：
  ```
  https://open.weixin.qq.com/connect/oauth2/authorize?appid=APPID&redirect_uri=REDIRECT_URI&response_type=code&scope=snsapi_base#wechat_redirect
  ```
- **方式 2：关注事件**  
  在公众号服务器配置中处理关注事件获取 OpenID。

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `wechatmp: app_id is required` | 未配置 `app_id` |
| `wechatmp: app_secret is required` | 未配置 `app_secret` |
| `wechatmp: template_id is required` | 未配置 `template_id` |
| `wechatmp: at least one target (OpenID) is required` | 发送时 `targets` 为空，需在 `recipients.wechatmp` 指定 OpenID 列表 |
| `wechatmp: empty target found in targets` | `targets` 中存在空字符串 |
| `wechatmp: failed to get access token: …` | AppID/Secret 错误、IP 白名单未配置、或微信侧限流 |
| `wechatmp: API error 40037: template id not exist` | 模板 ID 不存在或未通过审核 |
| `wechatmp: API error 43004: require subscribe` | 用户未关注公众号 |
| `wechatmp: API error 45047: out of freq limit` | 触达频率限制，需降低发送频率 |

错误格式：`wechatmp: API error {errcode}: {errmsg}`；网络/HTTP 错误走统一重试（429/5xx）。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [微信公众号指南](./wechat-official.md) - 公众号注册认证全流程
- [微信个人推送](./wechat.md) - 使用第三方服务的简单方案