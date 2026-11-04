import { useEffect, useState } from 'react'
import {
  Table, Button, Typography, Modal, Form, Input, Space, Popconfirm,
  message, Tag, Alert,
} from 'antd'
import { PlusOutlined, DeleteOutlined, CopyOutlined } from '@ant-design/icons'
import { heraldApi } from '../api'

export function errMsg(err: any, fallback: string) {
  return err?.response?.data?.message || err?.message || fallback
}

// 复制走 navigator.clipboard，失败时给出可读提示（jsdom/非安全上下文可能没有该 API）。
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

export default function ApiKeysPage() {
  const [keys, setKeys] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [saving, setSaving] = useState(false)
  // 创建成功后明文只展示一次：这个弹窗关掉就再也看不到完整 key。
  const [plainKey, setPlainKey] = useState('')
  const [form] = Form.useForm()

  useEffect(() => {
    fetchKeys()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function fetchKeys() {
    setLoading(true)
    try {
      const res: any = await heraldApi.getKeys()
      setKeys(res.data.keys || [])
    } catch (err: any) {
      message.error(errMsg(err, '加载 API Keys 失败'))
    } finally {
      setLoading(false)
    }
  }

  async function handleCreate() {
    let values: any
    try {
      values = await form.validateFields()
    } catch {
      return // 客户端校验失败：表单内已展示错误
    }
    setSaving(true)
    try {
      const res: any = await heraldApi.createKey({ name: values.name })
      setCreating(false)
      setPlainKey(res.data.key)
      await fetchKeys()
    } catch (err: any) {
      message.error(errMsg(err, '创建失败'))
    } finally {
      setSaving(false)
    }
  }

  async function handleCopy() {
    if (await copyText(plainKey)) {
      message.success('已复制到剪贴板')
    } else {
      message.error('复制失败，请手动选择复制')
    }
  }

  async function handleRevoke(id: string) {
    try {
      await heraldApi.revokeKey(id)
      message.success('已吊销')
      await fetchKeys()
    } catch (err: any) {
      message.error(errMsg(err, '吊销失败'))
    }
  }

  const columns = [
    { title: '备注', dataIndex: 'name', key: 'name' },
    { title: '前缀', dataIndex: 'prefix', key: 'prefix', code: true },
    {
      title: '创建时间', dataIndex: 'created_at', key: 'created_at',
      render: (v: string) => new Date(v).toLocaleString(),
    },
    {
      title: '状态', dataIndex: 'revoked', key: 'revoked',
      render: (revoked: boolean) =>
        revoked ? <Tag color="red">已吊销</Tag> : <Tag color="green">活跃</Tag>,
    },
    {
      title: '操作', key: 'actions',
      render: (_: any, key: any) =>
        key.revoked ? null : (
          <Popconfirm title="吊销后使用该 key 的调用会立即 401，确认吊销？" okText="确认吊销" cancelText="取消" onConfirm={() => handleRevoke(key.id)}>
            <Button size="small" danger icon={<DeleteOutlined />}>吊销</Button>
          </Popconfirm>
        ),
    },
  ]

  return (
    <div>
      <Typography.Title level={4}>API Keys</Typography.Title>
      <Typography.Paragraph type="secondary">
        Dashboard 调用密钥：仅创建时展示一次完整 key，列表只保留掩码前缀。
      </Typography.Paragraph>
      <Button
        type="primary"
        icon={<PlusOutlined />}
        style={{ marginBottom: 16 }}
        onClick={() => {
          form.resetFields()
          setCreating(true)
        }}
      >
        生成新 Key
      </Button>
      <Table
        rowKey="id"
        columns={columns as any}
        dataSource={keys}
        loading={loading}
        scroll={{ x: 'max-content' }}
        pagination={false}
      />

      <Modal
        title="生成新 Key"
        open={creating}
        onOk={handleCreate}
        onCancel={() => setCreating(false)}
        okText="生成"
        cancelText="取消"
        confirmLoading={saving}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="备注" rules={[{ required: true, message: '请输入备注' }]}>
            <Input placeholder="CI 发布机" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="Key 已生成"
        open={plainKey !== ''}
        onOk={() => setPlainKey('')}
        onCancel={() => setPlainKey('')}
        okText="我已保存"
        destroyOnHidden
      >
        <Alert
          type="warning"
          showIcon
          message="完整 key 仅此一次展示，关闭后无法再次查看"
          style={{ marginBottom: 12 }}
        />
        <Space.Compact style={{ width: '100%' }}>
          <Input readOnly value={plainKey} />
          <Button icon={<CopyOutlined />} onClick={handleCopy}>复制</Button>
        </Space.Compact>
      </Modal>
    </div>
  )
}
