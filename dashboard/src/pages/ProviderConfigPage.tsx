import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Card, Form, Input, InputNumber, Button, Spin, Tag, message, Typography, Space, Alert } from 'antd'
import { ArrowLeftOutlined } from '@ant-design/icons'

export default function ProviderConfigPage() {
  const { name } = useParams<{ name: string }>()
  const navigate = useNavigate()
  const [config, setConfig] = useState<any>(null)
  const [schema, setSchema] = useState<Record<string, string>>({})
  const [formData, setFormData] = useState<Record<string, any>>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()

  useEffect(() => {
    fetchConfig()
  }, [name])

  async function fetchConfig() {
    setLoading(true)
    try {
      const token = localStorage.getItem('herald_token')
      const res = await fetch(`/api/v1/config/${name}`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      })
      const data = await res.json()
      if (data.code === 0) {
        setConfig(data.data)
        setSchema(data.data.schema || {})
        setFormData({ ...data.data.config })
        form.setFieldsValue(data.data.config)
      } else {
        message.error(data.message)
      }
    } catch (err: any) {
      message.error(err.message)
    } finally {
      setLoading(false)
    }
  }

  async function handleSave() {
    setSaving(true)
    try {
      const token = localStorage.getItem('herald_token')
      const res = await fetch(`/api/v1/config/${name}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({ config: formData }),
      })
      const data = await res.json()
      if (data.code === 0) {
        message.success('配置保存成功，Provider 已重启')
      } else {
        message.error(data.message || '保存失败')
      }
    } catch (err: any) {
      message.error(err.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function handleTest() {
    setSaving(true)
    try {
      const token = localStorage.getItem('herald_token')
      const res = await fetch('/api/v1/notify', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({
          title: 'Herald 测试消息',
          body: '这是一条测试消息，用于验证 Provider 配置是否正确',
          level: 'info',
          channels: [name],
        }),
      })
      const data = await res.json()
      if (data.code === 0) {
        message.success('测试消息已发送，请检查是否收到')
      } else {
        message.error(data.message || '测试失败')
      }
    } catch (err: any) {
      message.error(err.message || '测试失败')
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />
  if (!config) return <Alert type="error" message="Provider 不存在" />

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 24 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate(-1)} />
        <Typography.Title level={4} style={{ margin: 0 }}>Provider 配置</Typography.Title>
      </div>

      <Card style={{ marginBottom: 16 }}>
        <Space size="large">
          <span style={{ fontSize: 18, fontWeight: 600 }}>{name}</span>
          <Tag color="blue">{config.type}</Tag>
          <Tag color={config.enabled ? 'green' : 'default'}>{config.enabled ? '已启用' : '已禁用'}</Tag>
        </Space>
      </Card>

      <Card>
        <Typography.Title level={5}>配置项</Typography.Title>
        {Object.keys(schema).length === 0 ? (
          <Alert type="info" message="此 Provider 暂无可配置项" />
        ) : (
          <Form form={form} layout="vertical" onFinish={handleSave}>
            {Object.entries(schema).map(([fieldName, fieldType]) => (
              <Form.Item key={fieldName} label={fieldName} name={fieldName}>
                {fieldType === 'number' ? (
                  <InputNumber
                    style={{ width: '100%' }}
                    placeholder={`输入 ${fieldName}`}
                    onChange={(val) => setFormData({ ...formData, [fieldName]: val })}
                  />
                ) : (
                  <Input
                    placeholder={`输入 ${fieldName}`}
                    onChange={(e) => setFormData({ ...formData, [fieldName]: e.target.value })}
                  />
                )}
              </Form.Item>
            ))}
            <Form.Item>
              <Space>
                <Button type="primary" htmlType="submit" loading={saving}>保存配置</Button>
                <Button onClick={handleTest} loading={saving}>测试连接</Button>
              </Space>
            </Form.Item>
          </Form>
        )}
      </Card>

      <Card style={{ marginTop: 16 }} size="small">
        <Typography.Text type="secondary">
          <ul style={{ margin: 0, paddingLeft: 20 }}>
            <li>修改配置后，Provider 将自动重启</li>
            <li>敏感信息（如密码、密钥）将显示为占位符</li>
            <li>配置仅保存在内存中，重启服务后将恢复为配置文件中的值</li>
          </ul>
        </Typography.Text>
      </Card>
    </div>
  )
}
