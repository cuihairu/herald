import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Form, Input, Button, Card, Checkbox, Typography, message } from 'antd'
import { UserOutlined, LockOutlined } from '@ant-design/icons'
import { heraldApi } from '../api'

export default function LoginPage() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)

  async function handleLogin(values: { username: string; password: string; remember: boolean }) {
    setLoading(true)
    try {
      const res = await heraldApi.login({
        username: values.username,
        password: values.password,
      })
      if (res.token) {
        localStorage.setItem('herald_token', res.token)
        if (values.remember && res.user) {
          localStorage.setItem('herald_user', JSON.stringify(res.user))
        }
        message.success('登录成功')
        navigate('/')
      } else {
        message.error(res.message || '登录失败')
      }
    } catch (err: any) {
      message.error(err.response?.data?.message || '网络错误，请稍后重试')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <Card style={{ width: 400 }}>
        <div style={{ textAlign: 'center', marginBottom: 24 }}>
          <Typography.Title level={2} style={{ color: '#3b82f6', marginBottom: 4 }}>Herald</Typography.Title>
          <Typography.Text type="secondary">请登录以继续</Typography.Text>
        </div>
        <Form onFinish={handleLogin} initialValues={{ remember: true }}>
          <Form.Item name="username" rules={[{ required: true, message: '请输入用户名' }]}>
            <Input prefix={<UserOutlined />} placeholder="用户名" autoComplete="username" />
          </Form.Item>
          <Form.Item name="password" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password prefix={<LockOutlined />} placeholder="密码" autoComplete="current-password" />
          </Form.Item>
          <Form.Item name="remember" valuePropName="checked">
            <Checkbox>记住我</Checkbox>
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={loading} block>
              登录
            </Button>
          </Form.Item>
        </Form>
        <div style={{ textAlign: 'center' }}>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            默认账号: <Typography.Text code>admin</Typography.Text> / <Typography.Text code>admin</Typography.Text>
          </Typography.Text>
        </div>
      </Card>
    </div>
  )
}
