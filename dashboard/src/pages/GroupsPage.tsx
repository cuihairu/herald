import { useEffect, useState } from 'react'
import {
  Table, Button, Typography, Modal, Form, Input, Select,
  Space, Popconfirm, message, Tag,
} from 'antd'
import { PlusOutlined, EditOutlined, DeleteOutlined } from '@ant-design/icons'
import { useHeraldStore } from '../stores/herald'
import { heraldApi } from '../api'

function errMsg(err: any, fallback: string) {
  return err?.response?.data?.message || err?.message || fallback
}

export default function GroupsPage() {
  const { groups, loading, fetchGroups } = useHeraldStore()
  const [form] = Form.useForm()
  // null = 弹窗关闭；{} = 新建；group 对象 = 编辑该群组。
  const [editing, setEditing] = useState<any>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    fetchGroups()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function openCreate() {
    form.resetFields()
    form.setFieldsValue({ members: [] })
    setEditing({})
  }

  function openEdit(group: any) {
    form.resetFields()
    form.setFieldsValue({
      id: group.id,
      description: group.description,
      members: (group.members || []).map((m: any) => ({
        channel: m.channel,
        recipients: m.recipients || [],
      })),
    })
    setEditing(group)
  }

  async function handleSave() {
    let values: any
    try {
      values = await form.validateFields()
    } catch {
      return // 客户端校验失败：表单内已展示错误
    }
    // 收件人钉选是可选字段：留空时不要给后端发空数组以外的噪音。
    // values 是 validateFields 的 any；antd 的 Form.List 未增行时给的是 []
    // （空数组本身为真，所以这个兜底当下走不到）。保留它是防御 antd 哪天
    // 把未增行的 Form.List 改回 undefined——那会让整页崩在 .map 上。
    const members = (values.members || []).map((m: any) =>
      m.recipients && m.recipients.length > 0
        ? { channel: m.channel, recipients: m.recipients }
        : { channel: m.channel }
    )
    const payload = { id: values.id, description: values.description, members }
    setSaving(true)
    try {
      if (editing?.id) await heraldApi.updateGroup(editing.id, payload)
      else await heraldApi.createGroup(payload)
      message.success('群组已保存')
      setEditing(null)
      await fetchGroups()
    } catch (err: any) {
      // 重复 id 的 409 与校验失败 400 的 message 直接展示。
      message.error(errMsg(err, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id: string) {
    try {
      await heraldApi.deleteGroup(id)
      message.success('群组已删除')
      await fetchGroups()
    } catch (err: any) {
      message.error(errMsg(err, '删除失败'))
    }
  }

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id' },
    {
      title: '描述', dataIndex: 'description', key: 'description',
      render: (d: string) => d || '—',
    },
    {
      title: '成员', key: 'members',
      render: (_: any, group: any) => (
        <Space wrap>
          {(group.members || []).map((m: any) => (
            <Tag key={m.channel}>
              {m.channel}
              {m.recipients?.length ? ` (${m.recipients.join(', ')})` : ''}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '操作', key: 'actions',
      render: (_: any, group: any) => (
        <Space>
          <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(group)}>编辑</Button>
          <Popconfirm title="删除该群组？" okText="确认" cancelText="取消" onConfirm={() => handleDelete(group.id)}>
            <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <Typography.Title level={4}>通知群组</Typography.Title>
      <Button
        type="primary"
        icon={<PlusOutlined />}
        style={{ marginBottom: 16 }}
        onClick={openCreate}
      >
        新建群组
      </Button>
      <Table
        rowKey="id"
        columns={columns as any}
        dataSource={groups}
        loading={loading}
        pagination={false}
      />
      <Modal
        title={editing?.id ? `编辑群组 ${editing.id}` : '新建群组'}
        open={editing !== null}
        onOk={handleSave}
        onCancel={() => setEditing(null)}
        okText="保存"
        cancelText="取消"
        confirmLoading={saving}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item name="id" label="ID" rules={[{ required: true, message: '请输入群组 ID' }]}>
            <Input placeholder="ops-oncall" disabled={!!editing?.id} />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input placeholder="值班花名册" />
          </Form.Item>
          <Form.Item label="成员（渠道 + 可选收件人钉选）">
            <Form.List name="members">
              {(fields, { add, remove }) => (
                <>
                  {fields.map(({ key, name, ...restField }) => (
                    <Space key={key} align="baseline" style={{ display: 'flex', marginBottom: 4 }}>
                      <Form.Item
                        name={[name, 'channel']}
                        rules={[{ required: true, message: '渠道必填' }]}
                        {...restField}
                      >
                        <Input placeholder="feishu-oncall" style={{ width: 220 }} />
                      </Form.Item>
                      <Form.Item name={[name, 'recipients']} {...restField}>
                        <Select
                          mode="tags"
                          open={false}
                          tokenSeparators={[',']}
                          placeholder="收件人（可选）"
                          style={{ width: 180 }}
                        />
                      </Form.Item>
                      <Button
                        type="text"
                        danger
                        icon={<DeleteOutlined />}
                        aria-label="移除成员"
                        onClick={() => remove(name)}
                      />
                    </Space>
                  ))}
                  <Button type="dashed" icon={<PlusOutlined />} onClick={() => add()}>
                    添加成员
                  </Button>
                </>
              )}
            </Form.List>
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
