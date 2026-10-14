import { useEffect, useState, useCallback } from 'react'
import { Table, Tag, Select, Space, Card, Statistic, Row, Col, Spin, Typography, Button } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'

interface DeliveryEntry {
  id: string
  provider: string
  status: string
  level: string
  retry_count: number
  max_attempts: number
  last_error: string
  created_at: string
  updated_at: string
  next_retry_at: string
}

const levelColors: Record<string, string> = { info: 'blue', warning: 'orange', error: 'red', critical: 'red' }

// Lifecycle vocabulary: the state machine the delivery store owns, as
// opposed to the log page's factual event outcome.
const statusColors: Record<string, string> = {
  accepted: 'blue',
  queued: 'default',
  delivering: 'orange',
  retrying: 'gold',
  delivered: 'green',
  failed: 'red',
  dead: 'volcano',
}

export function renderLevel(l: string) {
  return l ? <Tag color={levelColors[l] || 'default'}>{l}</Tag> : '-'
}

export default function DeliveriesPage() {
  const [deliveries, setDeliveries] = useState<DeliveryEntry[]>([])
  const [stats, setStats] = useState<any>(null)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [pagination, setPagination] = useState({ current: 1, pageSize: 50 })
  const [filters, setFilters] = useState({ status: '', provider: '', level: '', since: '', until: '' })
  const [providers, setProviders] = useState<string[]>([])

  const fetchDeliveries = useCallback(async () => {
    setLoading(true)
    try {
      const token = localStorage.getItem('herald_token')
      const params = new URLSearchParams({
        offset: String((pagination.current - 1) * pagination.pageSize),
        limit: String(pagination.pageSize),
      })
      Object.entries(filters).forEach(([k, v]) => { if (v) params.set(k, v) })

      const res = await fetch(`/api/v1/deliveries?${params}`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      })
      const data = await res.json()
      if (data.code === 0) {
        setDeliveries(data.data.deliveries || [])
        setTotal(data.data.total || 0)
      }
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
    }
  }, [pagination, filters])

  const fetchStats = useCallback(async () => {
    try {
      const token = localStorage.getItem('herald_token')
      const res = await fetch('/api/v1/deliveries/stats', {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      })
      const data = await res.json()
      if (data.code === 0) setStats(data.data)
    } catch {}
  }, [])

  const fetchProviders = useCallback(async () => {
    try {
      const token = localStorage.getItem('herald_token')
      const res = await fetch('/api/v1/providers', {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      })
      const data = await res.json()
      if (data.code === 0) {
        const names = [...new Set((data.data.providers || []).map((p: any) => p.name))] as string[]
        setProviders(names)
      }
    } catch {}
  }, [])

  useEffect(() => {
    fetchProviders()
    fetchStats()
  }, [])

  useEffect(() => {
    fetchDeliveries()
  }, [pagination.current, pagination.pageSize, filters])

  useEffect(() => {
    const timer = setInterval(() => { fetchDeliveries(); fetchStats() }, 10000)
    return () => clearInterval(timer)
  }, [fetchDeliveries, fetchStats])

  const columns: ColumnsType<DeliveryEntry> = [
    { title: 'Provider', dataIndex: 'provider', key: 'provider', width: 120 },
    {
      title: '投递状态', dataIndex: 'status', key: 'status', width: 100,
      render: (s: string) => <Tag color={statusColors[s] || 'default'}>{s}</Tag>,
    },
    {
      title: '重试', key: 'retry', width: 100,
      render: (_: unknown, d: DeliveryEntry) =>
        d.max_attempts ? `${d.retry_count}/${d.max_attempts}` : '-',
    },
    {
      title: '级别', dataIndex: 'level', key: 'level', width: 80,
      render: renderLevel,
    },
    { title: '最后错误', dataIndex: 'last_error', key: 'last_error', ellipsis: true, render: (e: string) => e || '-' },
    { title: '下次重试', dataIndex: 'next_retry_at', key: 'next_retry_at', width: 180, render: (t: string) => t ? new Date(t).toLocaleString('zh-CN') : '-' },
    {
      title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', width: 180,
      render: (t: string) => t ? new Date(t).toLocaleString('zh-CN') : '-',
    },
  ]

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>投递状态</Typography.Title>
        <Button icon={<ReloadOutlined />} onClick={() => { fetchDeliveries(); fetchStats() }} loading={loading}>
          刷新
        </Button>
      </div>

      {stats && (
        <Row gutter={16} style={{ marginBottom: 16 }}>
          <Col><Card size="small"><Statistic title="总数" value={stats.total} /></Card></Col>
          <Col><Card size="small"><Statistic title="已投递" value={stats.by_status?.delivered || 0} valueStyle={{ color: '#22c55e' }} /></Card></Col>
          <Col><Card size="small"><Statistic title="失败" value={stats.by_status?.failed || 0} valueStyle={{ color: '#ef4444' }} /></Card></Col>
          <Col><Card size="small"><Statistic title="重试耗尽" value={stats.by_status?.dead || 0} valueStyle={{ color: '#dc2626' }} /></Card></Col>
          <Col><Card size="small"><Statistic title="重试中" value={stats.by_status?.retrying || 0} valueStyle={{ color: '#f59e0b' }} /></Card></Col>
        </Row>
      )}

      <Card style={{ marginBottom: 16 }}>
        <Space wrap>
          <Select
            style={{ width: 130 }} placeholder="投递状态" allowClear
            value={filters.status || undefined}
            onChange={(v) => setFilters({ ...filters, status: v || '' })}
            options={[
              { value: 'accepted', label: '已受理' },
              { value: 'queued', label: '排队中' },
              { value: 'delivering', label: '投递中' },
              { value: 'retrying', label: '重试中' },
              { value: 'delivered', label: '已投递' },
              { value: 'failed', label: '失败' },
              { value: 'dead', label: '重试耗尽' },
            ]}
          />
          <Select
            style={{ width: 140 }} placeholder="Provider" allowClear
            value={filters.provider || undefined}
            onChange={(v) => setFilters({ ...filters, provider: v || '' })}
            options={providers.map((p) => ({ value: p, label: p }))}
          />
          <Select
            style={{ width: 120 }} placeholder="级别" allowClear
            value={filters.level || undefined}
            onChange={(v) => setFilters({ ...filters, level: v || '' })}
            options={[
              { value: 'info', label: '信息' }, { value: 'warning', label: '警告' },
              { value: 'error', label: '错误' }, { value: 'critical', label: '严重' },
            ]}
          />
        </Space>
      </Card>

      <Table
        dataSource={deliveries}
        columns={columns}
        rowKey="id"
        loading={loading}
        pagination={{
          current: pagination.current,
          pageSize: pagination.pageSize,
          total,
          showSizeChanger: false,
          onChange: (page) => setPagination({ ...pagination, current: page }),
        }}
        size="small"
      />
    </div>
  )
}
