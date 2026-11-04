import { useEffect, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Card, Col, Row, Segmented, Space, Spin, Statistic, Table, Tag, Typography, theme } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useHeraldStore } from '../stores/herald'
import { chartPalette } from '../theme/themes'
import { useThemeMode } from '../theme/ThemeModeProvider'
import ChartLine from '../components/ChartLine'

const displayNames: Record<string, string> = {
  log: '日志', telegram: 'Telegram', feishu: '飞书', wecom: '企业微信',
  email: '邮件', webhook: 'Webhook', discord: 'Discord', slack: 'Slack',
  dingtalk: '钉钉', aliyunsms: '阿里云短信', tencentsms: '腾讯云短信', neteasesms: '网易云信短信',
}

// 阈值口径：成功率 <80% 报错色，<95% 警示色；队列深度 >100 警示色。
export function rateTone(rate: number, total: number): 'default' | 'warning' | 'error' {
  if (total === 0) return 'default'
  if (rate < 80) return 'error'
  if (rate < 95) return 'warning'
  return 'default'
}

export function queueTone(depth: number): 'default' | 'warning' {
  return depth > 100 ? 'warning' : 'default'
}

export function rssGiB(bytes: number): number {
  // Return a numeric GiB value so ECharts' value axis can plot it; the
  // axis formatter renders the "GiB" suffix.
  return bytes / 1024 ** 3
}

const WINDOWS = ['1h', '6h', '24h']

function timeLabel(iso: string): string {
  return new Date(iso).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
}

const gridOption = { left: 48, right: 16, top: 32, bottom: 28 }

export default function DashboardPage() {
  const {
    overview, providers, metrics, sendSeries, seriesWindow, seriesLoading, loading,
    fetchOverview, fetchSeries, fetchProviders,
  } = useHeraldStore()
  const { resolved } = useThemeMode()
  const navigate = useNavigate()
  const { token } = theme.useToken()

  useEffect(() => {
    fetchOverview()
    fetchProviders()
    fetchSeries('6h')
    // Real-time refresh: queue depth, delivery rate and the trend curves
    // update while the page stays open. Polling is cleared on unmount.
    const timer = setInterval(() => {
      fetchOverview()
      fetchSeries(useHeraldStore.getState().seriesWindow)
    }, 15000)
    return () => clearInterval(timer)
  }, [])

  const palette = chartPalette[resolved]

  const sendOption = useMemo(() => ({
    animation: false,
    grid: gridOption,
    legend: { textStyle: { color: palette.textSecondary } },
    tooltip: { trigger: 'axis', backgroundColor: palette.tooltipBg, textStyle: { color: token.colorText } },
    xAxis: { type: 'category', data: sendSeries.map((p: any) => timeLabel(p.time)), axisLabel: { color: palette.textSecondary } },
    yAxis: { type: 'value', axisLabel: { color: palette.textSecondary }, splitLine: { lineStyle: { color: palette.splitLine } } },
    series: [
      { name: '成功', type: 'bar', stack: 'total', data: sendSeries.map((p: any) => p.success), itemStyle: { color: palette.success } },
      { name: '失败', type: 'bar', stack: 'total', data: sendSeries.map((p: any) => p.failed), itemStyle: { color: palette.failed } },
    ],
  }), [sendSeries, palette, token.colorText])

  const cpuOption = useMemo(() => ({
    animation: false,
    grid: gridOption,
    tooltip: { trigger: 'axis', backgroundColor: palette.tooltipBg, textStyle: { color: token.colorText } },
    xAxis: { type: 'category', data: metrics.map((s: any) => timeLabel(s.time)), axisLabel: { color: palette.textSecondary } },
    yAxis: {
      type: 'value',
      axisLabel: {
        color: palette.textSecondary,
        formatter: (v: number) => `${v.toFixed(1)} GiB`,
      },
      splitLine: { lineStyle: { color: palette.splitLine } },
    },
    series: [{ name: 'CPU', type: 'line', data: metrics.map((s: any) => s.cpu_percent), itemStyle: { color: palette.line } }],
  }), [metrics, palette, token.colorText])

  const rssOption = useMemo(() => ({
    animation: false,
    grid: gridOption,
    tooltip: { trigger: 'axis', backgroundColor: palette.tooltipBg, textStyle: { color: token.colorText } },
    xAxis: { type: 'category', data: metrics.map((s: any) => timeLabel(s.time)), axisLabel: { color: palette.textSecondary } },
    yAxis: { type: 'value', axisLabel: { color: palette.textSecondary }, splitLine: { lineStyle: { color: palette.splitLine } } },
    series: [{ name: 'RSS', type: 'line', data: metrics.map((s: any) => rssGiB(s.rss_bytes)), itemStyle: { color: palette.line } }],
  }), [metrics, palette, token.colorText])

  if (loading && !overview) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />

  const delivery = overview?.delivery
  const tone = rateTone(delivery?.success_rate ?? 0, delivery?.total ?? 0)
  const rateColor = tone === 'error' ? token.colorError : tone === 'warning' ? token.colorWarning : undefined
  const queueColor = queueTone(overview?.queue?.depth ?? 0) === 'warning' ? token.colorWarning : undefined

  const columns: ColumnsType<any> = [
    { title: '名称', dataIndex: 'name', render: (_: any, p: any) => displayNames[p.name] || p.name },
    { title: '类型', dataIndex: 'type', render: (t: string) => <Tag>{t}</Tag> },
    {
      title: '启用', dataIndex: 'enabled',
      render: (enabled: boolean) => (enabled ? <Tag color="green">已启用</Tag> : <Tag>已禁用</Tag>),
    },
    {
      title: '健康', dataIndex: 'status',
      render: (status: string) => (status === 'available' ? <Tag color="green">正常</Tag> : <Tag color="red">异常</Tag>),
    },
    {
      title: '启动时间', dataIndex: 'since',
      render: (since: string) => (since ? new Date(since).toLocaleString('zh-CN') : '-'),
    },
    {
      title: '操作', dataIndex: 'name',
      render: (name: string) => (
        <Button size="small" onClick={() => navigate(`/providers/${name}/config`)}>配置</Button>
      ),
    },
  ]

  return (
    <div>
      <Typography.Title level={4}>概览</Typography.Title>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} md={6}>
          <Card size="small">
            <Statistic title="启用中 Providers" value={overview ? `${overview.providers.enabled} / ${overview.providers.total}` : '-'} />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card size="small">
            <Statistic title="队列深度" value={overview ? overview.queue.depth : '-'} styles={{ content: queueColor ? { color: queueColor } : undefined }} />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card size="small">
            <Statistic
              title="24h 投递成功率"
              value={delivery && delivery.total > 0 ? `${delivery.success_rate}%` : '暂无投递'}
              styles={{ content: rateColor ? { color: rateColor } : undefined }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card size="small">
            <Statistic title="在线 Workers" value={overview ? `${overview.workers.online} / ${overview.workers.total}` : '-'} />
          </Card>
        </Col>
      </Row>

      <Space style={{ margin: '24px 0 8px', width: '100%', justifyContent: 'space-between' }}>
        <Typography.Title level={5} style={{ margin: 0 }}>趋势</Typography.Title>
        <Segmented size="small" options={WINDOWS} value={seriesWindow} onChange={(v) => fetchSeries(v as string)} />
      </Space>
      <Spin spinning={seriesLoading}>
        <Row gutter={[16, 16]}>
          <Col xs={24} lg={8}>
            <Card size="small" title="发送量">
              <ChartLine option={sendOption} />
            </Card>
          </Col>
          <Col xs={24} lg={8}>
            <Card size="small" title="CPU">
              <ChartLine option={cpuOption} />
            </Card>
          </Col>
          <Col xs={24} lg={8}>
            <Card size="small" title="内存">
              <ChartLine option={rssOption} />
            </Card>
          </Col>
        </Row>
      </Spin>

      <Typography.Title level={5} style={{ margin: '24px 0 8px' }}>Providers</Typography.Title>
      <Table
        rowKey="name"
        size="small"
        pagination={false}
        dataSource={providers}
        columns={columns}
      />
    </div>
  )
}
