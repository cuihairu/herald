import { useEffect } from 'react'
import { Card, Col, Row, Statistic, Spin, Typography } from 'antd'
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  ApiOutlined,
  NotificationOutlined,
} from '@ant-design/icons'
import { useHeraldStore } from '../stores/herald'

const displayNames: Record<string, string> = {
  log: '日志', telegram: 'Telegram', feishu: '飞书', wecom: '企业微信',
  email: '邮件', webhook: 'Webhook', discord: 'Discord', slack: 'Slack',
  dingtalk: '钉钉', aliyunsms: '阿里云短信', tencentsms: '腾讯云短信', neteasesms: '网易云信短信',
}

export default function DashboardPage() {
  const { status, providers, logStats, loading, fetchStatus, fetchProviders, fetchLogStats } = useHeraldStore()

  useEffect(() => {
    fetchStatus()
    fetchProviders()
    fetchLogStats()
  }, [])

  const onlineCount = providers.filter((p: any) => p.status === 'available').length

  if (loading && !status) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />

  return (
    <div>
      <Typography.Title level={4}>仪表盘</Typography.Title>
      <Row gutter={[16, 16]} style={{ marginBottom: 24 }}>
        <Col xs={12} sm={8} md={4}>
          <Card><Statistic title="系统状态" value={status?.status || 'unknown'} valueStyle={status?.status === 'running' ? { color: '#22c55e' } : undefined} /></Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card><Statistic title="Provider 数量" value={providers.length} prefix={<ApiOutlined />} /></Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card><Statistic title="在线 Provider" value={onlineCount} /></Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card><Statistic title="通知总数" value={logStats?.total || 0} prefix={<NotificationOutlined />} /></Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card><Statistic title="成功" value={logStats?.by_status?.success || 0} valueStyle={{ color: '#22c55e' }} prefix={<CheckCircleOutlined />} /></Card>
        </Col>
        <Col xs={12} sm={8} md={4}>
          <Card><Statistic title="失败" value={logStats?.by_status?.failed || 0} valueStyle={{ color: '#ef4444' }} prefix={<CloseCircleOutlined />} /></Card>
        </Col>
      </Row>

      <Typography.Title level={5}>Providers</Typography.Title>
      <Row gutter={[12, 12]}>
        {providers.map((p: any) => (
          <Col key={p.name} xs={24} sm={12} md={8} lg={6}>
            <Card size="small">
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <span>{displayNames[p.name] || p.name}</span>
                <span style={{
                  padding: '2px 8px', borderRadius: 4, fontSize: 12,
                  background: p.status === 'available' ? '#22c55e' : '#ef4444', color: '#fff',
                }}>
                  {p.status}
                </span>
              </div>
            </Card>
          </Col>
        ))}
      </Row>
    </div>
  )
}
