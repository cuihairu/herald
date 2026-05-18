# SMS Providers

Herald 支持国内主流短信服务商的接入。

## 支持的服务商

| 服务商 | Provider 名称 | 说明 |
|--------|--------------|------|
| 阿里云 | `aliyunsms` | 阿里云短信服务 |
| 腾讯云 | `tencentsms` | 腾讯云短信服务 |
| 网易云信 | `neteasesms` | 网易云信短信服务 |

## 配置

### 阿里云短信

```yaml
providers:
  aliyunsms:
    type: builtin
    enabled: true
    config:
      access_key_id: "${ALIYUN_ACCESS_KEY_ID}"
      access_key_secret: "${ALIYUN_ACCESS_KEY_SECRET}"
      sign_name: "您的签名"
      region: "cn-hangzhou"
```

### 腾讯云短信

```yaml
providers:
  tencentsms:
    type: builtin
    enabled: true
    config:
      secret_id: "${TENCENT_SECRET_ID}"
      secret_key: "${TENCENT_SECRET_KEY}"
      app_id: "您的短信应用ID"
      region: "ap-guangzhou"
```

### 网易云信短信

```yaml
providers:
  neteasesms:
    type: builtin
    enabled: true
    config:
      app_key: "${NETEASE_APP_KEY}"
      app_secret: "${NETEASE_APP_SECRET}"
```

## 使用示例

### 发送模板短信

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "title": "验证码",
    "body": "您的验证码是123456",
    "channels": ["aliyunsms"],
    "level": "info"
  }'
```

### 通过 API 发送指定模板

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "title": "短信通知",
    "channels": ["tencentsms"],
    "target": "+8613800138000",
    "data": {
      "template_id": "123456",
      "template_params": ["123456", "5"]
    }
  }'
```

### 通过 Worker 发送短信

使用 Worker SDK 可以更灵活地发送短信：

```go
task := &core.Task{
    Provider: "aliyunsms",
    Target:   "+8613800138000",
    Data: map[string]interface{}{
        "template_id": "SMS_123456789",
        "template_params": map[string]interface{}{
            "code": "123456",
            "time": "5",
        },
    },
}
```

## 模板参数说明

### 阿里云

- `template_code`: 短信模板CODE
- `template_param`: JSON字符串或模板参数对象

### 腾讯云

- `template_id`: 模板ID
- `template_params`: 模板参数数组

### 网易云信

- `template_id`: 模板ID
- `template_params`: 模板参数数组（逗号分隔）

## 注意事项

1. **手机号格式**：
   - 国内号码：可使用 `13800138000` 或 `+8613800138000`
   - 国际号码：必须使用 `+` 开头，如 `+1234567890`
   - 多个号码：用逗号分隔

2. **模板审核**：
   - 短信模板需要先在服务商平台审核通过
   - 变量参数需要与模板匹配

3. **签名**：
   - 阿里云需要配置签名名称
   - 签名需要审核通过

4. **频率限制**：
   - 建议配置限流器避免超频
   - Herald 默认使用 Token Bucket 限流

5. **启用/禁用**：
   - 可通过 Dashboard 或 API 动态启用/禁用
   - 禁用的 Provider 不会发送短信

## 错误处理

Herald 会自动重试可恢复的错误：

| 错误类型 | 是否重试 | 说明 |
|----------|----------|------|
| 网络超时 | 是 | 自动重试 |
| 频率限制 | 是 | 指数退避重试 |
| 参数错误 | 否 | 需要修正配置 |
| 余额不足 | 否 | 需要充值 |

## 最佳实践

1. **环境变量管理**：使用 `.env` 文件管理敏感信息
2. **多服务商配置**：配置多个 SMS Provider 提高可用性
3. **路由规则**：根据级别选择不同服务商
4. **去重配置**：避免重复发送
