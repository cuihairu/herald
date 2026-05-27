import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Card, Row, Col, Button, Switch, Tag, Spin, Typography } from 'antd'
import { SettingOutlined } from '@ant-design/icons'
import { useHeraldStore } from '../stores/herald'

const displayNames: Record<string, string> = {
  log: '日志', telegram: 'Telegram', feishu: '飞书', wecom: '企业微信',
  email: '邮件', webhook: 'Webhook', discord: 'Discord', slack: 'Slack',
  dingtalk: '钉钉', aliyunsms: '阿里云短信', tencentsms: '腾讯云短信', neteasesms: '网易云信短信',
}

function getProviderType(name: string) {
  if (name.includes('sms')) return '短信'
  if (name === 'email') return '邮件'
  if (name === 'webhook') return 'Webhook'
  return '即时通讯'
}

export default function ProvidersPage() {
  const { providers, loading, fetchProviders, enableProvider, disableProvider } = useHeraldStore()
  const navigate = useNavigate()

  useEffect(() => {
    fetchProviders()
  }, [])

  if (loading && providers.length === 0) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />

  return (
    <div>
      <Typography.Title level={4}>Providers</Typography.Title>
      <Row gutter={[16, 16]}>
        {providers.map((p: any) => (
          <Col key={p.name} xs={24} sm={12} lg={8} xl={6}>
            <Card
              title={displayNames[p.name] || p.name}
              extra={<Tag color={p.type === 'builtin' ? 'blue' : 'purple'}>{p.type}</Tag>}
            >
              <div style={{ marginBottom: 8 }}>
                <span>状态: </span>
                <Tag color={p.enabled ? 'green' : 'default'}>{p.enabled ? '已启用' : '已禁用'}</Tag>
              </div>
              <div style={{ marginBottom: 8 }}>
                <span>健康: </span>
                <Tag color={p.status === 'available' ? 'green' : 'red'}>{p.status === 'available' ? '正常' : '异常'}</Tag>
              </div>
              <div style={{ marginBottom: 8 }}>
                <span>类型: {getProviderType(p.name)}</span>
              </div>
              {p.since && (
                <div style={{ marginBottom: 8, fontSize: 12, color: '#999' }}>
                  启动时间: {new Date(p.since).toLocaleString('zh-CN')}
                </div>
              )}
              <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
                <Button icon={<SettingOutlined />} onClick={() => navigate(`/providers/${p.name}/config`)}>
                  配置
                </Button>
                <Switch
                  checked={p.enabled}
                  checkedChildren="已启用"
                  unCheckedChildren="已禁用"
                  onChange={async (checked) => {
                    try {
                      if (checked) await enableProvider(p.name)
                      else await disableProvider(p.name)
                    } catch {}
                  }}
                />
              </div>
            </Card>
          </Col>
        ))}
      </Row>
    </div>
  )
}
