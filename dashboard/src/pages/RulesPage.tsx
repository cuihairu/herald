import { useEffect, useState } from 'react'
import {
  Table, Button, Tag, Typography, Modal, Form, Input, InputNumber,
  Select, Switch, Space, Popconfirm, message, Drawer, Descriptions, Empty,
} from 'antd'
import dayjs from 'dayjs'
import { PlusOutlined, EditOutlined, DeleteOutlined } from '@ant-design/icons'
import { useHeraldStore } from '../stores/herald'
import { heraldApi } from '../api'

// mode 是启停的载体：active 生效、shadow 只观察不动作、off 停用。
const modeTags: Record<string, { color: string; label: string }> = {
  active: { color: 'green', label: '生效' },
  shadow: { color: 'blue', label: '观察' },
  off: { color: 'default', label: '停用' },
}

// action 是决策层的动词：route 改道、allow 放行、suppress 抑制。
const actionLabels: Record<string, string> = {
  route: '改道', allow: '放行', suppress: '抑制',
}

function errMsg(err: any, fallback: string) {
  return err?.response?.data?.message || err?.message || fallback
}

export default function RulesPage() {
  const { rules, loading, fetchRules } = useHeraldStore()
  const [form] = Form.useForm()
  // null = 弹窗关闭；{} = 新建；rule 对象 = 编辑该条。
  const [editing, setEditing] = useState<any>(null)
  const [saving, setSaving] = useState(false)
  // 影子统计详情：detail 是打开详情的规则，detailData 是规则详情接口
  // 的返回（加载中为 null，字段回落 0/空表）。
  const [detail, setDetail] = useState<any>(null)
  const [detailData, setDetailData] = useState<any>(null)

  useEffect(() => {
    fetchRules()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function openCreate() {
    form.resetFields()
    form.setFieldsValue({ mode: 'shadow', action: 'route', priority: 0 })
    setEditing({})
  }

  function openEdit(rule: any) {
    form.resetFields()
    form.setFieldsValue({
      id: rule.id,
      match: rule.match,
      mode: rule.mode || 'shadow',
      action: rule.action || 'route',
      priority: rule.priority ?? 0,
      channels: rule.route?.[0]?.channels || [],
    })
    setEditing(rule)
  }

  async function handleSave() {
    let values: any
    try {
      values = await form.validateFields()
    } catch {
      return // 客户端校验失败：表单内已展示错误，无需弹消息
    }
    const payload: any = {
      id: values.id,
      match: values.match,
      mode: values.mode,
      action: values.action,
      priority: values.priority,
    }
    // 只有改道动作带路由步骤；allow/suppress 不需要渠道。
    if (values.action === 'route') {
      payload.route = [{ channels: values.channels || [] }]
    }
    setSaving(true)
    try {
      if (editing?.id) await heraldApi.updateRule(editing.id, payload)
      else await heraldApi.createRule(payload)
      message.success('规则已保存')
      setEditing(null)
      await fetchRules()
    } catch (err: any) {
      // 保存即编译：表达式编译失败等后端 400 的 message 直接展示。
      message.error(errMsg(err, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  // 打开影子统计详情：窗口计数与最近命中样本都在规则详情读面里。
  async function openDetail(rule: any) {
    setDetail(rule)
    setDetailData(null)
    try {
      const res = await heraldApi.getRule(rule.id)
      setDetailData(res.data.data)
    } catch (err: any) {
      message.error(errMsg(err, '详情加载失败'))
    }
  }

  async function handleDelete(id: string) {
    try {
      await heraldApi.deleteRule(id)
      message.success('规则已删除')
      await fetchRules()
    } catch (err: any) {
      message.error(errMsg(err, '删除失败'))
    }
  }

  // 启停开关：active ↔ shadow。整体替换该规则（PUT 语义），
  // 其余字段原样带回。
  async function toggleMode(rule: any, checked: boolean) {
    const mode = checked ? 'active' : 'shadow'
    try {
      await heraldApi.updateRule(rule.id, { ...rule, mode })
      await fetchRules()
    } catch (err: any) {
      message.error(errMsg(err, '切换失败'))
    }
  }

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id' },
    {
      title: '模式', key: 'mode',
      render: (_: any, rule: any) => {
        const tag = modeTags[rule.mode || 'shadow']
        return (
          <Space>
            <Tag color={tag.color}>{tag.label}</Tag>
            <Switch
              size="small"
              checked={(rule.mode || 'shadow') === 'active'}
              onChange={(checked) => toggleMode(rule, checked)}
            />
          </Space>
        )
      },
    },
    {
      title: '动作', key: 'action',
      render: (_: any, rule: any) => actionLabels[rule.action || 'route'],
    },
    // 影子命中：后端按 (事件种类, 规则) 精确计数，只统计"影子期本会
    // 触发"的次数；进程内累计，重启归零，所以缺失字段回落 0。
    {
      title: '影子命中', key: 'shadow_hits',
      render: (_: any, rule: any) => rule.shadow_hits ?? 0,
    },
    { title: '优先级', dataIndex: 'priority', key: 'priority' },
    {
      title: '匹配表达式', dataIndex: 'match', key: 'match',
      render: (expr: string) => <code>{expr}</code>,
    },
    {
      title: '路由渠道', key: 'channels',
      render: (_: any, rule: any) => (rule.route?.[0]?.channels || []).join(', '),
    },
    {
      title: '操作', key: 'actions',
      render: (_: any, rule: any) => (
        <Space>
          <Button size="small" onClick={() => openDetail(rule)}>详情</Button>
          <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(rule)}>编辑</Button>
          <Popconfirm title="删除该规则？" okText="确认" cancelText="取消" onConfirm={() => handleDelete(rule.id)}>
            <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <Typography.Title level={4}>通知规则</Typography.Title>
      <Button
        type="primary"
        icon={<PlusOutlined />}
        style={{ marginBottom: 16 }}
        onClick={openCreate}
      >
        新建规则
      </Button>
      <Table
        rowKey="id"
        columns={columns as any}
        dataSource={rules}
        loading={loading}
        pagination={false}
      />
      <Modal
        title={editing?.id ? `编辑规则 ${editing.id}` : '新建规则'}
        open={editing !== null}
        onOk={handleSave}
        onCancel={() => setEditing(null)}
        okText="保存"
        cancelText="取消"
        confirmLoading={saving}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item name="id" label="ID" rules={[{ required: true, message: '请输入规则 ID' }]}>
            <Input placeholder="prod-payment-failure" disabled={!!editing?.id} />
          </Form.Item>
          <Form.Item name="match" label="匹配表达式" rules={[{ required: true, message: '请输入匹配表达式' }]}>
            <Input.TextArea rows={2} placeholder={`level == "error" && params.fail_rate > 0.05`} />
          </Form.Item>
          <Space wrap>
            <Form.Item name="mode" label="模式" style={{ minWidth: 120 }}>
              <Select
                options={[
                  { value: 'shadow', label: '观察（只记录）' },
                  { value: 'active', label: '生效' },
                  { value: 'off', label: '停用' },
                ]}
              />
            </Form.Item>
            <Form.Item name="action" label="动作" style={{ minWidth: 120 }}>
              <Select
                options={[
                  { value: 'route', label: '改道' },
                  { value: 'allow', label: '放行' },
                  { value: 'suppress', label: '抑制' },
                ]}
              />
            </Form.Item>
            <Form.Item name="priority" label="优先级">
              <InputNumber />
            </Form.Item>
          </Space>
          <Form.Item noStyle shouldUpdate={(a, b) => a.action !== b.action}>
            {({ getFieldValue }) =>
              getFieldValue('action') === 'route' ? (
                <Form.Item name="channels" label="路由渠道（可填渠道名或 group: 引用）">
                  <Select
                    mode="tags"
                    open={false}
                    tokenSeparators={[',']}
                    placeholder="输入后回车，如 feishu-oncall 或 group:ops"
                  />
                </Form.Item>
              ) : null
            }
          </Form.Item>
        </Form>
      </Modal>
      <Drawer
        title="影子统计详情"
        open={detail !== null}
        onClose={() => setDetail(null)}
        width={640}
      >
        {detail && <ShadowDetail rule={detail} data={detailData} />}
      </Drawer>
    </div>
  )
}

// 影子统计详情：累计 / 近24小时 / 近7天三个口径 + 最近命中样本。
// 数据是后端进程内状态（重启归零）；加载中或后端未上报时字段回落
// 0 与空样本表，规则文档本身不受影响。
function ShadowDetail({ rule, data }: { rule: any; data: any }) {
  const stats = data?.shadow_stats
  const samples = stats?.samples || []
  return (
    <div>
      <Typography.Title level={5}>{rule.id}</Typography.Title>
      <Descriptions column={3} size="small" bordered>
        <Descriptions.Item label="累计">{stats?.total ?? 0}</Descriptions.Item>
        <Descriptions.Item label="近24小时">{stats?.last_24h ?? 0}</Descriptions.Item>
        <Descriptions.Item label="近7天">{stats?.last_7d ?? 0}</Descriptions.Item>
      </Descriptions>
      <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>
        进程内统计，重启归零；窗口按整点小时桶累计，样本为最近命中（最多保留 20 条）。
      </Typography.Paragraph>
      {samples.length > 0 ? (
        <Table
          rowKey="at"
          size="small"
          pagination={false}
          dataSource={samples}
          columns={[
            { title: '时间', dataIndex: 'at', render: (v: string) => dayjs(v).format('MM-DD HH:mm:ss') },
            { title: '类型', dataIndex: 'type' },
            { title: '级别', dataIndex: 'level' },
            { title: '标题', dataIndex: 'title' },
            { title: '命中渠道', key: 'channels', render: (_: any, s: any) => (s.channels || []).join(', ') },
          ] as any}
        />
      ) : (
        <Empty description="暂无命中样本" />
      )}
    </div>
  )
}
