import { useState } from 'react'
import { Card, Form, Input, Button, Typography, message } from 'antd'
import { heraldApi } from '../api'
import { errMsg } from './ApiKeysPage'

export default function SettingsPage() {
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()

  async function handleChange() {
    let values: any
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    setSaving(true)
    try {
      await heraldApi.changePassword({
        old_password: values.old_password,
        new_password: values.new_password,
      })
      message.success('密码已修改')
      form.resetFields()
    } catch (err: any) {
      message.error(errMsg(err, '修改失败'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <Typography.Title level={4}>设置</Typography.Title>
      <Card title="修改密码" style={{ maxWidth: 480 }}>
        <Form form={form} layout="vertical" onFinish={handleChange}>
          <Form.Item name="old_password" label="旧密码" rules={[{ required: true, message: '请输入旧密码' }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item name="new_password" label="新密码" rules={[{ required: true, message: '请输入新密码' }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item
            name="confirm_password"
            label="确认新密码"
            dependencies={['new_password']}
            rules={[
              { required: true, message: '请再次输入新密码' },
              ({ getFieldValue }) => ({
                validator: (_: any, value: string) =>
                  !value || value === getFieldValue('new_password')
                    ? Promise.resolve()
                    : Promise.reject(new Error('两次输入的新密码不一致')),
              }),
            ]}
          >
            <Input.Password />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={saving}>
            修改密码
          </Button>
        </Form>
      </Card>
    </div>
  )
}
