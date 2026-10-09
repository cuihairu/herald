import { useCallback, useEffect, useState } from 'react'
import dayjs from 'dayjs'
import {
  Table, Button, Typography, Modal, Tag, Space, Select, Popconfirm, message, Descriptions,
} from 'antd'
import { ReloadOutlined, CheckOutlined, EyeOutlined } from '@ant-design/icons'
import { heraldApi } from '../api'

function errMsg(err: any, fallback: string) {
  return err?.response?.data?.message || err?.message || fallback
}

const levelColors: Record<string, string> = { info: 'blue', warning: 'orange', error: 'red', critical: 'red' }
const statusColors: Record<string, string> = { open: 'red', acked: 'orange', resolved: 'green' }

// 状态派生与后端 core/incident 的 Status() 同序：恢复 > 已确认 > 处理中。
// 列表响应里没有现成 status 字段，前端按时间戳自行归类。
export function incidentStatus(inc: any): string {
  if (inc.resolved_at) return 'resolved'
  if (inc.acked_at) return 'acked'
  return 'open'
}

// 确认人取当前登录用户；登录态缺失时（理论上进不了本页）兜底 admin，
// 后端对空 acked_by 也接受，这里给个可读的名字比发空串好。
export function currentUsername(): string {
  try {
    const raw = localStorage.getItem('herald_user')
    const u = raw ? JSON.parse(raw) : null
    return u?.username || 'admin'
  } catch {
    return 'admin'
  }
}

export default function IncidentsPage() {
  const [incidents, setIncidents] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [status, setStatus] = useState('all')
  const [detail, setDetail] = useState<any>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  const load = useCallback(async (statusFilter: string) => {
    setLoading(true)
    try {
      const res = await heraldApi.getIncidents({ status: statusFilter })
      setIncidents(res.data.incidents || [])
    } catch (err: any) {
      message.error(errMsg(err, '加载事件失败'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load('all')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function openDetail(inc: any) {
    setDetail(inc)
    setDetailLoading(true)
    try {
      const res = await heraldApi.getIncident(inc.id)
      setDetail(res.data)
    } catch {
      // 详情拉取失败就展示行内快照：列表行已有大部分字段，不打断查看。
    } finally {
      setDetailLoading(false)
    }
  }

  async function handleAck(inc: any) {
    try {
      await heraldApi.ackAlert(inc.alert_id, { acked_by: currentUsername() })
      message.success('已确认')
      await load(status)
    } catch (err: any) {
      message.error(errMsg(err, '确认失败'))
    }
  }

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id' },
    {
      title: '标题', dataIndex: 'title', key: 'title',
      render: (t: string) => t || '—',
    },
    {
      title: '级别', dataIndex: 'level', key: 'level',
      render: (l: string) => (l ? <Tag color={levelColors[l] || 'default'}>{l}</Tag> : '—'),
    },
    {
      title: '状态', key: 'status',
      render: (_: any, inc: any) => {
        const s = incidentStatus(inc)
        return <Tag color={statusColors[s]}>{s}</Tag>
      },
    },
    {
      title: '打开时间', dataIndex: 'opened_at', key: 'opened_at',
      render: (v: string) => dayjs(v).format('MM-DD HH:mm:ss'),
    },
    {
      title: '操作', key: 'actions',
      render: (_: any, inc: any) => (
        <Space>
          <Button size="small" icon={<EyeOutlined />} onClick={() => openDetail(inc)}>详情</Button>
          {inc.alert_id && incidentStatus(inc) === 'open' && (
            <Popconfirm title="确认该告警？" okText="确认" cancelText="取消" onConfirm={() => handleAck(inc)}>
              <Button size="small" type="primary" ghost icon={<CheckOutlined />}>确认告警</Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ]

  return (
    <div>
      <Typography.Title level={4}>事件台账</Typography.Title>
      <Space style={{ marginBottom: 16 }}>
        <Select
          value={status}
          style={{ width: 140 }}
          onChange={(v: string) => {
            setStatus(v)
            load(v)
          }}
          options={[
            { value: 'all', label: '全部' },
            { value: 'open', label: '处理中' },
            { value: 'acked', label: '已确认' },
            { value: 'resolved', label: '已恢复' },
          ]}
        />
        <Button icon={<ReloadOutlined />} onClick={() => load(status)}>刷新</Button>
      </Space>
      <Table
        rowKey="id"
        columns={columns as any}
        dataSource={incidents}
        loading={loading}
        pagination={false}
      />
      <Modal
        title={detail ? `事件 ${detail.id}` : ''}
        open={detail !== null}
        footer={null}
        onCancel={() => setDetail(null)}
        width={640}
      >
        {detail && (
          <>
            <Descriptions column={2} size="small" bordered>
              <Descriptions.Item label="规则">{detail.rule_id || '—'}</Descriptions.Item>
              <Descriptions.Item label="告警 ID">{detail.alert_id || '—'}</Descriptions.Item>
              <Descriptions.Item label="分组">{detail.group_key || '—'}</Descriptions.Item>
              <Descriptions.Item label="级别">{detail.level || '—'}</Descriptions.Item>
              <Descriptions.Item label="打开时间">
                {dayjs(detail.opened_at).format('YYYY-MM-DD HH:mm:ss')}
              </Descriptions.Item>
              <Descriptions.Item label="确认">
                {detail.acked_at
                  ? `${dayjs(detail.acked_at).format('MM-DD HH:mm:ss')} by ${detail.acked_by || '?'}`
                  : '未确认'}
              </Descriptions.Item>
              <Descriptions.Item label="恢复">
                {detail.resolved_at ? dayjs(detail.resolved_at).format('MM-DD HH:mm:ss') : '未恢复'}
              </Descriptions.Item>
              <Descriptions.Item label="事件数">{detail.events ?? 0}</Descriptions.Item>
            </Descriptions>
            <Typography.Title level={5} style={{ marginTop: 16 }}>时间线</Typography.Title>
            {(detail.timeline || []).length === 0 ? (
              <Typography.Text type="secondary">无</Typography.Text>
            ) : (
              <Space direction="vertical" size={4}>
                {detail.timeline.map((e: any, i: number) => (
                  <Typography.Text key={i}>
                    [{dayjs(e.at).format('MM-DD HH:mm:ss')}] {e.kind}{e.detail ? `: ${e.detail}` : ''}
                  </Typography.Text>
                ))}
              </Space>
            )}
          </>
        )}
      </Modal>
    </div>
  )
}
