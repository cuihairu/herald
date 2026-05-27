import { useEffect, useState, useCallback } from 'react'
import { Table, Tag, Select, Space, Card, Statistic, Row, Col, Spin, Typography, Button } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'

interface LogEntry {
  id: string
  provider: string
  status: string
  title: string
  body: string
  error: string
  level: string
  duration: number
  created_at: string
}

export default function LogsPage() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [stats, setStats] = useState<any>(null)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [pagination, setPagination] = useState({ current: 1, pageSize: 50 })
  const [filters, setFilters] = useState({ status: '', provider: '', level: '', since: '', until: '' })
  const [providers, setProviders] = useState<string[]>([])
  const [refreshTimer, setRefreshTimer] = useState<ReturnType<typeof setInterval> | null>(null)

  const fetchLogs = useCallback(async () => {
    setLoading(true)
    try {
      const token = localStorage.getItem('herald_token')
      const params = new URLSearchParams({
        offset: String((pagination.current - 1) * pagination.pageSize),
        limit: String(pagination.pageSize),
      })
      Object.entries(filters).forEach(([k, v]) => { if (v) params.set(k, v) })

      const res = await fetch(`/api/v1/logs?${params}`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      })
      const data = await res.json()
      if (data.code === 0) {
        setLogs(data.data.logs || [])
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
      const res = await fetch('/api/v1/logs/stats', {
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
    fetchLogs()
  }, [pagination.current, pagination.pageSize, filters])

  useEffect(() => {
    const timer = setInterval(() => { fetchLogs(); fetchStats() }, 10000)
    return () => clearInterval(timer)
  }, [fetchLogs, fetchStats])

  const statusColors: Record<string, string> = { success: 'green', failed: 'red', pending: 'orange' }
  const levelColors: Record<string, string> = { info: 'blue', warning: 'orange', error: 'red', critical: 'red' }

  const columns: ColumnsType<LogEntry> = [
    { title: 'Provider', dataIndex: 'provider', key: 'provider', width: 120 },
    {
      title: '状态', dataIndex: 'status', key: 'status', width: 80,
      render: (s: string) => <Tag color={statusColors[s] || 'default'}>{s}</Tag>,
    },
    { title: '标题', dataIndex: 'title', key: 'title', ellipsis: true },
    {
      title: '级别', dataIndex: 'level', key: 'level', width: 80,
      render: (l: string) => l ? <Tag color={levelColors[l] || 'default'}>{l}</Tag> : '-',
    },
    { title: '错误', dataIndex: 'error', key: 'error', ellipsis: true, render: (e: string) => e || '-' },
    { title: '耗时', dataIndex: 'duration', key: 'duration', width: 80, render: (d: number) => d != null ? `${d}ms` : '-' },
    {
      title: '时间', dataIndex: 'created_at', key: 'created_at', width: 180,
      render: (t: string) => t ? new Date(t).toLocaleString('zh-CN') : '-',
    },
  ]

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>任务日志</Typography.Title>
        <Button icon={<ReloadOutlined />} onClick={() => { fetchLogs(); fetchStats() }} loading={loading}>
          刷新
        </Button>
      </div>

      {stats && (
        <Row gutter={16} style={{ marginBottom: 16 }}>
          <Col><Card size="small"><Statistic title="总数" value={stats.total} /></Card></Col>
          <Col><Card size="small"><Statistic title="成功" value={stats.by_status?.success || 0} valueStyle={{ color: '#22c55e' }} /></Card></Col>
          <Col><Card size="small"><Statistic title="失败" value={stats.by_status?.failed || 0} valueStyle={{ color: '#ef4444' }} /></Card></Col>
          <Col><Card size="small"><Statistic title="进行中" value={stats.by_status?.pending || 0} valueStyle={{ color: '#f59e0b' }} /></Card></Col>
        </Row>
      )}

      <Card style={{ marginBottom: 16 }}>
        <Space wrap>
          <Select
            style={{ width: 120 }} placeholder="状态" allowClear
            value={filters.status || undefined}
            onChange={(v) => setFilters({ ...filters, status: v || '' })}
            options={[{ value: 'success', label: '成功' }, { value: 'failed', label: '失败' }, { value: 'pending', label: '进行中' }]}
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
        dataSource={logs}
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
