# 微信公众号模板消息

通过微信公众号模板消息接口向关注用户发送通知。

## 作用

`wechatmp` Builtin Provider 使用微信公众号模板消息 API（需服务号+认证），将 Herald 通知推送到用户微信。适合正式业务通知、订单状态、告警触达等场景。

## 申请凭据

模板消息对公众号资质有硬性要求，申请链路较长，按顺序：

1. **认证服务号**：模板消息仅对**已认证的服务号**开放（个人订阅号不行）。没有的话先在 [微信公众平台](https://mp.weixin.qq.com/) 注册服务号并完成微信认证
2. **AppID / AppSecret**：公众平台 → **设置与开发** → **基本配置**，成为开发者后可见；**把 Herald 服务器的出口 IP 加入 IP 白名单**，否则取 access_token 会被拒
3. **模板**：**广告与服务** → **模板消息** → 从模板库选用或申请新模板（标题 + 关键词，需审核），得到 **模板 ID**（形如 `AtE-xxxx`）
4. **OpenID**：关注公众号的用户，通过「获取用户列表」接口或业务侧授权拿到，作为投递目标

## 发第一条消息

配置好（见下）并 `heraldd serve --config config.yaml` 启动后（**必须带 OpenID 收件人**）：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "order.notice",
    "level": "info",
    "title": "订单已发货",
    "body": "您的订单 SF123456 已发出",
    "channels": ["wechatmp"],
    "recipients": {"wechatmp": ["用户的OpenID"]}
  }'
```

微信「服务通知」里收到模板卡片（标题/正文按模板关键词位填充，见下）。没收到？查[排错指南](/guide/troubleshooting)。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `app_id` | ✅ | 公众号 AppID | 无 |
| `app_secret` | ✅ | 公众号 AppSecret | 无 |
| `template_id` | ✅ | 模板消息 ID（公众号后台模板库获取） | 无 |
| `default_url` | ❌ | 点击模板消息跳转的默认 URL | 空 |

缺 `app_id`、`app_secret` 或 `template_id` 时 Provider 创建即失败（`wechatmp: app_id is required` / `wechatmp: app_secret is required` / `wechatmp: template_id is required`），启动日志可见。

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
      default_url: "$WECHATMP_DEFAULT_URL"  # 可选
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `WECHATMP_APP_ID` | `app_id` | 公众号 AppID（`.env.example` 惯用名） |
| `WECHATMP_APP_SECRET` | `app_secret` | 公众号 AppSecret |
| `WECHATMP_TEMPLATE_ID` | `template_id` | 模板消息 ID |
| `WECHATMP_DEFAULT_URL` | `default_url` | 点击跳转默认 URL（可选） |

## 消息模板与限制

### 模板字段映射

Herald 自动将内容映射到微信模板字段：

| 微信字段 | Herald 内容 | 说明 |
|---------|------------|------|
| `thing1` | `title` | 标题（最多 20 字符，超长截断） |
| `thing2` | `body` | 正文（最多 30 字符，超长截断） |
| `character_string1` | `level` | 消息级别（如 `error`/`warning`/`info`） |
| `time3` | 当前时间 | 时间戳 `2006-01-02 15:04:05` |

> ⚠️ 模板需在公众号后台「模板消息」中创建/选择，**字段名必须为** `thing1`、`thing2`、`character_string1`、`time3`（或同类型对应字段），否则投递会报错。

### 消息限制

- 单用户每分钟 ≤ 100 条
- 公众号最多 25 个模板
- 模板需微信审核（1-7 天），审核通过才能使用
- 模板行业需与公众号认证行业匹配
- 内容不能包含敏感词

### 能力声明

- `PayloadKinds`: `ProviderTemplate`、`Content`
- `SupportsTemplate`: `true`

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `wechatmp: app_id is required` | 未配置 `app_id` |
| `wechatmp: app_secret is required` | 未配置 `app_secret` |
| `wechatmp: template_id is required` | 未配置 `template_id` |
| `wechatmp: at least one target (OpenID) is required` | 发送时未指定接收用户 OpenID |
| `wechatmp: empty target found in targets` | targets 列表中有空字符串 |
| `wechatmp: failed to get access token: …` | 获取 access_token 失败（AppID/Secret 错误、网络不通、频率限制） |
| `wechatmp: API error {errcode}: {errmsg}` | 微信侧返回错误码，常见：<br/>`40037` template_id 无效<br/>`45015` 用户未关注 / OpenID 错误<br/>`45009` 接口频率超限<br/>`40003` AppSecret 错误 |

错误格式说明：API 返回 `errcode != 0` 时返回 `wechatmp: API error {errcode}: {errmsg}`；网络/解析错误按统一 HTTP 错误格式返回。

## 获取用户 OpenID

用户关注公众号后需要 OpenID 才能发送消息：

### 方式 1: 用户授权（网页授权）

生成授权链接让用户授权（静默授权 `snsapi_base`）：

```
https://open.weixin.qq.com/connect/oauth2/authorize?appid=APPID&redirect_uri=REDIRECT_URI&response_type=code&scope=snsapi_base#wechat_redirect
```

回调拿 `code` 换 `access_token` + `openid`。

### 方式 2: 关注事件

在公众号服务器配置处理 `subscribe` 事件，直接获取 `FromUserName`（即 OpenID）。

## 前置要求（官方限制）

- **企业资质**：营业执照、对公账户、300 元/年认证费、审核 7-30 个工作日
- **服务号**：必须是服务号（订阅号无模板消息权限）
- **已认证**：通过微信认证
- **开通模板消息**：后台「功能」→「模板消息」开启

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [微信个人推送](./wechat.md) - 使用第三方服务的简单方案
- [微信公众号指南](./wechat-official.md) - 完整申请流程与限制说明