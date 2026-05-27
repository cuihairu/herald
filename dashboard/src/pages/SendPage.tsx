import { useState } from 'react'
import { Form, Input, Select, Button, Card, message, Typography } from 'antd'
import { SendOutlined } from '@ant-design/icons'
import { useHeraldStore } from '../stores/herald'

export default function SendPage() {
  const { providers, loading, sendNotify, fetchProviders } = useHeraldStore()
  const [form] = Form.useForm()
  const [result, setResult] = useState<{ success: boolean; message: string } | null>(null)

  // Fetch providers if not loaded
  if (providers.length === 0) fetchProviders()

  async function handleSubmit(values: any) {
    setResult(null)
    try {
      await sendNotify(values)
      message.success('发送成功')
      setResult({ success: true, message: '发送成功' })
      form.resetFields()
    } catch (err: any) {
      message.error(err.message || '发送失败')
      setResult({ success: false, message: err.message || '发送失败' })
    }
  }

  return (
    <div>
      <Typography.Title level={4}>发送消息</Typography.Title>
      <Card style={{ maxWidth: 600 }}>
        <Form form={form} layout="vertical" onFinish={handleSubmit} initialValues={{ level: '', channels: [] }}>
          <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入标题' }]}>
            <Input placeholder="输入标题" />
          </Form.Item>
          <Form.Item name="body" label="内容">
            <Input.TextArea rows={4} placeholder="输入内容" />
          </Form.Item>
          <Form.Item name="level" label="级别">
            <Select placeholder="选择级别" allowClear>
              <Select.Option value="info">信息</Select.Option>
              <Select.Option value="warning">警告</Select.Option>
              <Select.Option value="error">错误</Select.Option>
              <Select.Option value="critical">严重</Select.Option>
            </Select>
          </Form.Item>
          <Form.Item name="channels" label="Channels">
            <Select mode="multiple" placeholder="选择渠道（不选则根据路由规则）">
              {providers.map((p: any) => (
                <Select.Option key={p.name} value={p.name}>{p.name}</Select.Option>
              ))}
            </Select>
          </Form.Item>
          {result && (
            <div style={{
              padding: '8px 12px', borderRadius: 6, marginBottom: 16,
              background: result.success ? '#059669' : '#dc2626', color: '#fff',
            }}>
              {result.message}
            </div>
          )}
          <Form.Item>
            <Button type="primary" htmlType="submit" icon={<SendOutlined />} loading={loading} block>
              发送
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  )
}
