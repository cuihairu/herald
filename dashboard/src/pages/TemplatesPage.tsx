import { useEffect, useState } from 'react'
import {
  Table, Typography, Tag, Button, Modal, Form, Input, Select,
  Popconfirm, message, Space,
} from 'antd'
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons'
import { heraldApi } from '../api'
import { errMsg } from './ApiKeysPage'

// 模板表单值 → 后端负载：字段列表按逗号/空白拆分，空项丢弃。
export function toTemplatePayload(values: any) {
  const fields = (values.fields || '')
    .split(/[\s,]+/)
    .filter((f: string) => f !== '')
  return { id: values.id, name: values.name, title: values.title, level: values.level, fields }
}

export default function TemplatesPage() {
  const [templates, setTemplates] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()

  useEffect(() => {
    fetchTemplates()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function fetchTemplates() {
    setLoading(true)
    try {
      const res: any = await heraldApi.getTemplates()
      setTemplates(res.data.templates || [])
    } catch (err: any) {
      message.error(errMsg(err, '加载模板失败'))
    } finally {
      setLoading(false)
    }
  }

  async function handleCreate() {
    let values: any
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    setSaving(true)
    try {
      await heraldApi.createTemplate(toTemplatePayload(values))
      message.success('模板已创建')
      setCreating(false)
      await fetchTemplates()
    } catch (err: any) {
      message.error(errMsg(err, '创建失败'))
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id: string) {
    try {
      await heraldApi.deleteTemplate(id)
      message.success('模板已删除')
      await fetchTemplates()
    } catch (err: any) {
      message.error(errMsg(err, '删除失败'))
    }
  }

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id' },
    { title: '名称', dataIndex: 'name', key: 'name', render: (n: string) => n || '—' },
    {
      title: '级别', dataIndex: 'level', key: 'level',
      render: (l: string) => <Tag>{l || 'default'}</Tag>,
    },
    {
      title: '字段', key: 'fields',
      render: (_: any, t: any) => (
        <Space wrap>{(t.fields || []).map((f: any) => <Tag key={typeof f === 'string' ? f : f.name}>{typeof f === 'string' ? f : f.name}</Tag>)}</Space>
      ),
    },
    {
      title: '操作', key: 'actions',
      render: (_: any, t: any) => (
        <Popconfirm title={`删除模板 ${t.id}？`} okText="删除" cancelText="取消" onConfirm={() => handleDelete(t.id)}>
          <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
        </Popconfirm>
      ),
    },
  ]

  return (
    <div>
      <Typography.Title level={4}>通知模板</Typography.Title>
      <Button
        type="primary"
        icon={<PlusOutlined />}
        style={{ marginBottom: 16 }}
        onClick={() => {
          form.resetFields()
          setCreating(true)
        }}
      >
        新建模板
      </Button>
      <Table
        rowKey="id"
        columns={columns as any}
        dataSource={templates}
        loading={loading}
        scroll={{ x: 'max-content' }}
        pagination={false}
      />

      <Modal
        title="新建模板"
        open={creating}
        onOk={handleCreate}
        onCancel={() => setCreating(false)}
        okText="创建"
        cancelText="取消"
        confirmLoading={saving}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item name="id" label="ID" rules={[{ required: true, message: '请输入模板 ID' }]}>
            <Input placeholder="deploy-notice" />
          </Form.Item>
          <Form.Item name="name" label="名称">
            <Input placeholder="部署通知" />
          </Form.Item>
          <Form.Item name="title" label="标题">
            <Input placeholder="{{app}} 部署完成" />
          </Form.Item>
          <Form.Item name="level" label="级别" initialValue="info">
            <Select options={[{ value: 'info', label: 'info' }, { value: 'warning', label: 'warning' }, { value: 'error', label: 'error' }]} />
          </Form.Item>
          <Form.Item name="fields" label="字段（逗号或空格分隔）">
            <Input placeholder="app, env, version" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
