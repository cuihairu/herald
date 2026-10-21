# 阿里云短信

通过阿里云短信服务发送国内/国际短信，适合验证码、通知、营销等场景。

## 作用

`aliyunsms` Builtin Provider 直接调用阿里云短信 API（`dysmsapi.aliyuncs.com/SendSms`），使用 RPC 签名机制（HMAC-SHA1）发送模板短信。支持多号码批量发送、模板参数映射、签名配置。

> ⚠️ **前置要求**：需开通阿里云短信服务、完成实名认证、申请短信签名与模板并通过审核。

## 申请凭据

短信是国内强审核渠道，四步走（全程在阿里云控制台）：

1. **开通服务**：控制台搜「短信服务」开通；账号需完成实名认证
2. **申请签名**：国内消息 → 签名管理 → 添加签名（个人可用 App/公众号名等，企业资质通过更快），审核通过前不能发
3. **申请模板**：模板管理 → 添加模板（验证码/通知类），得到 **模板 CODE**（形如 `SMS_123456789`），模板里的占位符 `${code}` 与发送参数对应
4. **AccessKey**：RAM 访问控制 → 创建用户 → 授权 `AliyunDysmsFullAccess` → 生成 AccessKey ID / Secret

## 发第一条消息

短信**必须走模板**（本 Provider 不支持直连 title/body），配置模板 + 参数后发送：

```yaml
# config.yaml 追加
templates:
  verify_code:
    name: "验证码"
    level: "info"
    template_code: "SMS_123456789"
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
    "channels": ["aliyunsms"],
    "recipients": {"aliyunsms": ["13800000000"]}
  }'
```

手机收到 `【你的签名】您的验证码884275...`（文案=你的模板）。没收到？签名/模板审核状态、余额、错误码见[常见错误](#常见错误)与[排错指南](/guide/troubleshooting)。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `access_key_id` | ✅ | 阿里云 AccessKey ID | 无 |
| `access_key_secret` | ✅ | 阿里云 AccessKey Secret | 无 |
| `sign_name` | ✅ | 短信签名名称（需在阿里云控制台审核通过） | 无 |
| `region` | ❌ | 地域 ID | `cn-hangzhou` |
| `endpoint` | ❌ | API 端点 | `dysmsapi.aliyuncs.com` |
| `enabled` | ❌ | 是否启用 | `true` |

缺 `access_key_id`、`access_key_secret` 或 `sign_name` 时 Provider 创建即失败（`aliyunsms: access_key_id is required` 等），启动日志可见。

## 配置示例

```yaml
providers:
  aliyunsms:
    type: aliyunsms
    enabled: true
    config:
      access_key_id: "$ALIYUN_ACCESS_KEY_ID"
      access_key_secret: "$ALIYUN_ACCESS_KEY_SECRET"
      sign_name: "您的签名"
      region: "cn-hangzhou"
      endpoint: "dysmsapi.aliyuncs.com"
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `ALIYUN_ACCESS_KEY_ID` | `access_key_id` | 阿里云 AccessKey ID |
| `ALIYUN_ACCESS_KEY_SECRET` | `access_key_secret` | 阿里云 AccessKey Secret |

## 消息模板与限制

### 发送模式

仅支持 **Provider Template** 模式（`PayloadKind: ProviderTemplate`），需在任务或模板 Binding 中提供：

- `template_code`：阿里云短信模板 CODE（如 `SMS_123456789`）
- `params`：命名参数映射（`map[string]string`，字段标签 → 模板变量）

### 手机号格式

- 号码原样发给阿里云，Herald 不做前缀补齐（腾讯云 Provider 才会自动补 `+86`）
- 多号码：任务 `targets` 字段传入多个，内部以逗号拼接

### 能力声明

- `PayloadKinds`: `provider_template`
- `SupportsTemplate`: `true`

### 限制说明

| 限制项 | 说明 |
|--------|------|
| **模板审核** | 模板需在阿里云控制台审核通过 |
| **签名审核** | 签名需审核通过，发送时必须指定 |
| **频率限制** | 默认单号码 1 条/分钟、5 条/小时、10 条/天（视套餐而定） |
| **内容长度** | 单条 ≤ 70 字（含签名），长短信按 67 字/条计费 |
| **重试** | 网络层错误走统一重试；`isv.BUSINESS_LIMIT_CONTROL` 等业务码失败返回裸错误，不重试 |

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `aliyunsms: access_key_id is required` | 未配置 `access_key_id` |
| `aliyunsms: access_key_secret is required` | 未配置 `access_key_secret` |
| `aliyunsms: sign_name is required` | 未配置 `sign_name` |
| `phone numbers are required` | 发送时 `targets` 为空 |
| `aliyun SMS error: isp.RAM_PERMISSION_DENY` | AccessKey 无短信权限，需授权 `AliyunDysmsapiFullAccess` |
| `aliyun SMS error: isv.SMS_SIGN_ILLEGAL` | 签名未通过审核或不存在 |
| `aliyun SMS error: isv.SMS_TEMPLATE_ILLEGAL` | 模板 CODE 错误或未通过审核 |
| `aliyun SMS error: isv.MOBILE_NUMBER_ILLEGAL` | 手机号格式错误 |
| `aliyun SMS error: isv.DAY_LIMIT_CONTROL` | 触达日发送限额 |
| `aliyun SMS error: isv.BUSINESS_LIMIT_CONTROL` | 触达频率限制，需降低发送频率 |

错误格式：`aliyun SMS error: {Code} - {Message}`；网络/HTTP 错误走统一重试（429/5xx）。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [腾讯云短信](./tencentsms.md) - 腾讯云短信通道
- [网易云信短信](./neteasesms.md) - 网易云信短信通道
- [SMS Providers](./sms.md) - 短信 Provider 综合对比