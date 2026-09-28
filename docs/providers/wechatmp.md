# 微信公众号模板消息（wechatmp）

`wechatmp` Builtin Provider 通过微信公众号模板消息接口，把通知点对点推送到关注用户的微信「服务通知」。

申请公众号、创建模板、获取用户 OpenID 的完整流程见[「微信公众号」指南](./wechat-official.md)；本页是该 Provider 的配置参考。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `app_id` | ✅ | 公众号 AppID | 无 |
| `app_secret` | ✅ | 公众号 AppSecret | 无 |
| `template_id` | ✅ | 模板消息 ID（模板库创建后获得） | 无 |
| `default_url` | ❌ | 点击模板卡片跳转的 URL | 空 |

三者缺一即 Provider 创建失败：`wechatmp: app_id is required` / `wechatmp: app_secret is required` / `wechatmp: template_id is required`。

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

发送时通过 `recipients` 指定目标用户的 OpenID（必填，可多个，逐个投递）：

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "order",
    "title": "订单通知",
    "body": "您的订单已发货",
    "level": "info",
    "channels": ["wechatmp"],
    "recipients": { "wechatmp": ["OpenID-1", "OpenID-2"] }
  }'
```

## 环境变量

provider config 里以 `$` 开头的字符串值会在加载时展开为同名环境变量的值（`$VAR` 写法；`"${VAR}"` 带花括号不会被展开）。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `WECHATMP_APP_ID` | `app_id` | 公众号 AppID（`.env.example` 惯用名） |
| `WECHATMP_APP_SECRET` | `app_secret` | 公众号 AppSecret |
| `WECHATMP_TEMPLATE_ID` | `template_id` | 模板消息 ID |
| `WECHATMP_DEFAULT_URL` | `default_url` | 默认跳转 URL |

## 模板字段映射

Herald 把通知内容映射到模板关键词（关键词名需与你的模板一致，映射关系固定）：

| 模板字段 | 取值 | 截断 |
|----------|------|------|
| `thing1` | 通知标题 | 20 字符 |
| `thing2` | 通知正文 | 30 字符 |
| `character_string1` | 级别（`level` 非空时才携带） | 不截断 |
| `time3` | 发送时间（`2006-01-02 15:04:05`） | — |

截断按字符（rune）计，中文不会截出半个字。

**模板级覆盖**：单条通知可通过 payload 的 `ProviderTemplate.TemplateID` 临时指定其他模板 ID，未指定时用配置的 `template_id`。

## Access Token 管理

- Provider 自动获取并缓存 Access Token，无需手动处理
- 默认有效期 7200s，提前 300s 刷新；并发请求只触发一次实际拉取
- Token 拉取失败报 `wechatmp: token error {code}: {errmsg}`（如 `40013` appid 无效、`40125` secret 无效）

## 限制

- **多目标逐个投递**：多个 OpenID 逐个调用接口，任一失败时整体返回聚合错误，成功的照常送达
- 微信侧限制：模板消息需**认证服务号**；单模板字段长度受微信校验（thing 类字段一般 20 字符以内）；全天接口调用配额与账号绑定
- 请求超时 30s；HTTP 408/429/5xx 会被包装为可重试错误，走统一重试

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `wechatmp: app_id is required`（及 app_secret / template_id） | 配置缺必填项 |
| `wechatmp: at least one target (OpenID) is required` | 请求未带 `recipients.wechatmp` |
| `wechatmp: empty target found in targets` | recipients 列表里有空字符串 |
| `wechatmp: token error 40125: invalid appsecret` | AppSecret 错误或已被重置，公众号后台重置后更新配置 |
| `wechatmp: API error 40003: invalid openid` | OpenID 错误，或不属于本公众号（用户未关注 / OpenID 抄错） |
| `wechatmp: API error 43004: require subscribe` | 用户已取消关注，重新关注后才能收到模板消息 |
| `wechatmp: API error 47001: data format error` | 模板数据与模板关键词不匹配（确认模板关键词名为 thing1/thing2/character_string1/time3） |
| `wechatmp: 2/3 succeeded - failed: …` | 聚合错误：括号里是每个失败 OpenID 及其具体原因 |

## 下一步

- [微信公众号指南](./wechat-official.md) - 公众号申请、模板创建、OpenID 获取全流程
- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [微信个人推送](./wechat.md) - 无企业资质的轻量替代方案
