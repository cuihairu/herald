# 网易云信短信

通过网易云信（NetEase YunXin）短信服务发送国内/国际短信，适合验证码、通知、营销等场景。

## 作用

`neteasesms` Builtin Provider 直接调用网易云信短信 API（`api.netease.im/v1/sms/sendTemplateSms`），使用 SHA1 校验和机制（`sha1(appSecret + nonce + curTime)`）发送模板短信。支持模板短信、验证码发送与校验，适合需要验证码场景的业务。

> ⚠️ **前置要求**：需开通网易云信短信服务、创建应用、申请短信模板并通过审核。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `app_key` | ✅ | 网易云信 AppKey | 无 |
| `app_secret` | ✅ | 网易云信 AppSecret | 无 |
| `endpoint` | ❌ | API 端点 | `api.netease.im` |
| `enabled` | ❌ | 是否启用 | `true` |

缺 `app_key` 或 `app_secret` 时 Provider 创建即失败（`neteasesms: app_key is required` / `neteasesms: app_secret is required`），启动日志可见。

## 配置示例

```yaml
providers:
  neteasesms:
    type: builtin
    enabled: true
    config:
      app_key: "$NETEASE_APP_KEY"
      app_secret: "$NETEASE_APP_SECRET"
      endpoint: "api.netease.im"
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `NETEASE_APP_KEY` | `app_key` | 网易云信 AppKey |
| `NETEASE_APP_SECRET` | `app_secret` | 网易云信 AppSecret |

## 消息模板与限制

### 发送模式

仅支持 **Provider Template** 模式（`PayloadKind: ProviderTemplate`），需在任务或模板 Binding 中提供：

- `template_id` / `template_code`：网易云信短信模板 ID（优先读 `template_id`，回退 `template_code`）
- `params` / `param_order`：有序参数数组（`[]string`，内部以逗号拼接）

### 手机号格式

- 国内号码：`13800138000` 或 `+8613800138000`（保持原样，不自动加前缀）
- 国际号码：必须使用 `+` 开头，如 `+1234567890`
- 多号码：任务 `targets` 字段传入多个，内部以逗号拼接

### 扩展能力

除标准 `Deliver` 外，Provider 还暴露两个方法供 Worker/高级用法调用：

- `SendCode(ctx, mobile, authCode, deviceID)` - 发送验证码
- `VerifyCode(ctx, mobile, code)` - 校验验证码

### 能力声明

- `PayloadKinds`: `provider_template`
- `SupportsTemplate`: `true`

### 限制说明

| 限制项 | 说明 |
|--------|------|
| **模板审核** | 模板需在网易云信控制台审核通过 |
| **频率限制** | 视套餐而定，建议配置限流器 |
| **内容长度** | 单条 ≤ 70 字，长短信按 67 字/条计费 |
| **重试** | 网络错误/频率限制走统一重试（429/5xx 等价语义） |

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `neteasesms: app_key is required` | 未配置 `app_key` |
| `neteasesms: app_secret is required` | 未配置 `app_secret` |
| `phone numbers are required` | 发送时 `targets` 为空 |
| `no valid phone numbers` | `targets` 全为空或空白字符串 |
| `template_id is required` | 任务/模板未提供 `template_id` |
| `neteasesms error: 403 - Forbidden` | AppKey/AppSecret 错误或应用被禁用 |
| `neteasesms error: 414 - 参数错误` | 手机号格式错误、模板 ID 不存在或参数不匹配 |
| `neteasesms error: 416 - 频率限制` | 触达频率限制，需降低发送频率 |
| `neteasesms error: 417 - 余额不足` | 短信余额不足，需充值 |
| `neteasesms error: 420 - 模板未通过审核` | 模板未审核或审核未通过 |

错误格式：`neteasesms error: {code} - {msg}`；网络/HTTP 错误走统一重试（429/5xx）。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [阿里云短信](./aliyunsms.md) - 阿里云短信通道
- [腾讯云短信](./tencentsms.md) - 腾讯云短信通道
- [SMS Providers](./sms.md) - 短信 Provider 综合对比