import { useEffect, useState } from 'react'
import dayjs from 'dayjs'
import {
  Table, Button, Typography, Modal, Form, Input, Space, Popconfirm, message,
} from 'antd'
import { PlusOutlined, EditOutlined, DeleteOutlined } from '@ant-design/icons'
import { useHeraldStore } from '../stores/herald'
import { heraldApi } from '../api'

function errMsg(err: any, fallback: string) {
  return err?.response?.data?.message || err?.message || fallback
}

export interface RosterPayload {
  id: string
  description?: string
  periods: Array<{ start: string; end: string }>
}

// 表单值 → 后端负载。时段行整行留白（点了「添加时段」又没填）不发后端，
// 免得后端按校验失败整表拒绝。periods 的 || 兜底防御的是「表单值里缺
// periods 字段」：Form.List 未增行时给 []（空数组为真，走不到右支），但若
// antd 哪天改回 undefined、或表单未挂载就被提交，没有这层兜底整页会崩在
// .map 上——右支由 toRosterPayload 的直测固定住语义。
export function toRosterPayload(values: any): RosterPayload {
  const periods = (values.periods || [])
    .map((p: any) => ({ start: (p.start || '').trim(), end: (p.end || '').trim() }))
    .filter((p: any) => p.start !== '' || p.end !== '')
  return { id: values.id, description: values.description, periods }
}

export default function RostersPage() {
  const { rosters, loading, fetchRosters } = useHeraldStore()
  const [form] = Form.useForm()
  // null = 弹窗关闭；{} = 新建；roster 对象 = 编辑该值班表。
  const [editing, setEditing] = useState<any>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    fetchRosters()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function openCreate() {
    form.resetFields()
    form.setFieldsValue({ periods: [] })
    setEditing({})
  }

  function openEdit(roster: any) {
    form.resetFields()
    form.setFieldsValue({
      id: roster.id,
      description: roster.description,
      periods: roster.periods || [],
    })
    setEditing(roster)
  }

  async function handleSave() {
    let values: any
    try {
      values = await form.validateFields()
    } catch {
      return // 客户端校验失败：表单内已展示错误
    }
    // 表单值 → 后端负载的换算（periods 兜底、整行留白降噪）见 toRosterPayload。
    const payload = toRosterPayload(values)
    setSaving(true)
    try {
      if (editing?.id) await heraldApi.updateRoster(editing.id, payload)
      else await heraldApi.createRoster(payload)
      message.success('值班表已保存')
      setEditing(null)
      await fetchRosters()
    } catch (err: any) {
      // 重复 id 的 409 与时段校验失败 400 的 message 直接展示。
      message.error(errMsg(err, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id: string) {
    try {
      await heraldApi.deleteRoster(id)
      message.success('值班表已删除')
      await fetchRosters()
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
      title: '时段', key: 'periods',
      render: (_: any, roster: any) => {
        const periods = roster.periods || []
        if (periods.length === 0) return '—'
        return (
          <Space direction="vertical" size={0}>
            {periods.map((p: any, i: number) => (
              // i 作 key：时段是纯展示行，无增删重排。
              // eslint-disable-next-line react/no-array-index-key
              <Typography.Text key={i}>
                {dayjs(p.start).format('MM-DD HH:mm')} ~ {dayjs(p.end).format('MM-DD HH:mm')}
              </Typography.Text>
            ))}
          </Space>
        )
      },
    },
    {
      title: '操作', key: 'actions',
      render: (_: any, roster: any) => (
        <Space>
          <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(roster)}>编辑</Button>
          <Popconfirm title="删除该值班表？" okText="确认" cancelText="取消" onConfirm={() => handleDelete(roster.id)}>
            <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <Typography.Title level={4}>值班表</Typography.Title>
      <Button
        type="primary"
        icon={<PlusOutlined />}
        style={{ marginBottom: 16 }}
        onClick={openCreate}
      >
        新建值班表
      </Button>
      <Table
        rowKey="id"
        columns={columns as any}
        dataSource={rosters}
        loading={loading}
        pagination={false}
      />
      <Modal
        title={editing?.id ? `编辑值班表 ${editing.id}` : '新建值班表'}
        open={editing !== null}
        onOk={handleSave}
        onCancel={() => setEditing(null)}
        okText="保存"
        cancelText="取消"
        confirmLoading={saving}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item name="id" label="ID" rules={[{ required: true, message: '请输入值班表 ID' }]}>
            <Input placeholder="ops-oncall" disabled={!!editing?.id} />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input placeholder="周末值班" />
          </Form.Item>
          <Form.Item label="时段（RFC3339，如 2026-10-10T09:00:00+08:00）">
            <Form.List name="periods">
              {(fields, { add, remove }) => (
                <>
                  {fields.map(({ key, name, ...restField }) => (
                    <Space key={key} align="baseline" style={{ display: 'flex', marginBottom: 4 }}>
                      <Form.Item
                        name={[name, 'start']}
                        noStyle
                        {...restField}
                      >
                        <Input placeholder="开始 2026-10-10T09:00:00+08:00" style={{ width: 280 }} />
                      </Form.Item>
                      <Form.Item name={[name, 'end']} noStyle {...restField}>
                        <Input placeholder="结束 2026-10-10T18:00:00+08:00" style={{ width: 280 }} />
                      </Form.Item>
                      <Button
                        type="text"
                        danger
                        icon={<DeleteOutlined />}
                        aria-label="移除时段"
                        onClick={() => remove(name)}
                      />
                    </Space>
                  ))}
                  <Button type="dashed" icon={<PlusOutlined />} onClick={() => add()}>
                    添加时段
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
