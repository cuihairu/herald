# 腾讯云短信

通过腾讯云短信服务发送国内/国际短信，适合验证码、通知、营销等场景。

## 作用

`tencentsms` Builtin Provider 直接调用腾讯云短信 API（`sms.tencentcloudapi.com/SendSms`），使用 TC3-HMAC-SHA256 签名机制（API v3）发送模板短信。支持多号码批量发送、有序参数数组、应用 ID 与签名配置。

> ⚠️ **前置要求**：需开通腾讯云短信服务、完成实名认证、创建短信应用、申请短信签名与模板并通过审核。

## 申请凭据

四步走（腾讯云控制台）：

1. **开通服务**：控制台搜「短信」开通；账号需完成实名认证
2. **创建应用**：应用管理 → 创建应用，得到 **SDKAppID**（即配置里的 `app_id`，形如 `1400000000`）
3. **签名与正文模板**：国内短信 → 签名管理 / 正文模板管理，各自提交审核（签名需资质佐证），通过后记下**正文模板 ID**（形如 `1234567`）
4. **API 密钥**：访问管理 CAM → API 密钥管理 → 新建密钥，得到 SecretId / SecretKey

## 发第一条消息

短信**必须走模板**（本 Provider 不支持直连 title/body），配置模板 + 参数后发送：

```yaml
# config.yaml 追加
templates:
  verify_code:
    name: "验证码"
    level: "info"
    template_id: "1234567"
    fields:
      - label: "code"
        value: "{{.Code}}"
```

`heraldd serve --config config.yaml` 启动后：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "user.verify",
    "level": "info",
    "template": "verify_code",
    "params": {"Code": "884275"},
    "channels": ["tencentsms"],
    "recipients": {"tencentsms": ["13800000000"]}
  }'
```

手机收到 `【你的签名】您的验证码884275...`。号码自动补 `+86` 前缀；没收到？签名/模板审核状态、错误码见[常见错误](#常见错误)与[排错指南](/guide/troubleshooting)。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `secret_id` | ✅ | 腾讯云 SecretId | 无 |
| `secret_key` | ✅ | 腾讯云 SecretKey | 无 |
| `app_id` | ✅ | 短信应用 ID（SDK AppID） | 无 |
| `sign_name` | ❌ | 短信签名内容（需在控制台审核通过） | 空 |
| `region` | ❌ | 地域 | `ap-guangzhou` |
| `endpoint` | ❌ | API 端点 | `sms.tencentcloudapi.com` |
| `enabled` | ❌ | 是否启用 | `true` |

缺 `secret_id`、`secret_key` 或 `app_id` 时 Provider 创建即失败（`tencentsms: secret_id is required` 等），启动日志可见。

## 配置示例

```yaml
providers:
  tencentsms:
    type: tencentsms
    enabled: true
    config:
      secret_id: "$TENCENT_SECRET_ID"
      secret_key: "$TENCENT_SECRET_KEY"
      app_id: "1400000000"
      sign_name: "您的签名"
      region: "ap-guangzhou"
      endpoint: "sms.tencentcloudapi.com"
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `TENCENT_SECRET_ID` | `secret_id` | 腾讯云 SecretId |
| `TENCENT_SECRET_KEY` | `secret_key` | 腾讯云 SecretKey |

## 消息模板与限制

### 发送模式

仅支持 **Provider Template** 模式（`PayloadKind: ProviderTemplate`），需在任务或模板 Binding 中提供：

- `template_id`：腾讯云短信模板 ID（数字，如 `1234567`）
- `params` / `param_order`：有序参数数组（`[]string`，按模板变量顺序填充）

### 手机号格式

- 国内号码：`13800138000` 或 `+8613800138000`（自动补齐 `+86` 前缀）
- 国际号码：必须使用 `+` 开头，如 `+1234567890`
- 多号码：任务 `targets` 字段传入多个

### 能力声明

- `PayloadKinds`: `provider_template`
- `SupportsTemplate`: `true`

### 限制说明

| 限制项 | 说明 |
|--------|------|
| **模板审核** | 模板需在腾讯云控制台审核通过 |
| **签名审核** | 签名需审核通过，`sign_name` 填签名内容而非 ID |
| **频率限制** | 默认单号码 1 条/30秒、10 条/小时、30 条/天（视套餐而定） |
| **内容长度** | 单条 ≤ 70 字（含签名），长短信按 67 字/条计费 |
| **重试** | 网络错误/频率限制走统一重试（429/5xx 等价语义） |

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `tencentsms: secret_id is required` | 未配置 `secret_id` |
| `tencentsms: secret_key is required` | 未配置 `secret_key` |
| `tencentsms: app_id is required` | 未配置 `app_id` |
| `phone numbers are required` | 发送时 `targets` 为空 |
| `template_id is required` | 任务/模板未提供 `template_id` |
| `tencent SMS error: FailedOperation.SignNotPass` | 签名未通过审核 |
| `tencent SMS error: FailedOperation.TemplateNotPass` | 模板未通过审核 |
| `tencent SMS error: FailedOperation.MobileNumberNotExist` | 手机号格式错误或为空号 |
| `tencent SMS error: FailedOperation.FrequencyLimit` | 触达频率限制，需降低发送频率 |
| `tencent SMS error: FailedOperation.InsufficientBalance` | 短信余额不足，需充值 |
| `tencent SMS error: AuthFailure.SecretIdNotFound` | SecretId 不存在或已禁用 |
| `tencent SMS error: AuthFailure.SignatureFailure` | 签名计算失败，检查 SecretKey 与系统时间 |

错误格式：`tencent SMS error: {Code} - {Message}`（顶层 Error）或 `tencent SMS error for {phone}: {Code} - {Message}`（单号码状态）；网络/HTTP 错误走统一重试（429/5xx）。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [阿里云短信](./aliyunsms.md) - 阿里云短信通道
- [网易云信短信](./neteasesms.md) - 网易云信短信通道
- [SMS Providers](./sms.md) - 短信 Provider 综合对比