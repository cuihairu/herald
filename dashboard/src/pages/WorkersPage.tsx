import { useEffect, useState } from 'react'
import { Card, Descriptions, Tag, Spin, Typography, Badge, Statistic, Row, Col } from 'antd'
import { useHeraldStore } from '../stores/herald'

export default function WorkersPage() {
  const { workers, queue, loading, fetchWorkers, fetchQueue } = useHeraldStore()
  const [refreshTimer, setRefreshTimer] = useState<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    const refresh = async () => {
      await Promise.all([fetchWorkers(), fetchQueue()])
    }
    refresh()
    const timer = setInterval(refresh, 5000)
    setRefreshTimer(timer)
    return () => clearInterval(timer)
  }, [])

  if (loading && workers.length === 0) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />

  return (
    <div>
      <Typography.Title level={4}>Workers</Typography.Title>
      <Row gutter={16} style={{ marginBottom: 24 }}>
        <Col span={8}>
          <Card><Statistic title="连接数量" value={workers.length} /></Card>
        </Col>
        <Col span={8}>
          <Card><Statistic title="队列大小" value={queue.size} /></Card>
        </Col>
      </Row>

      <Typography.Title level={5}>已连接 Workers</Typography.Title>
      {workers.length === 0 ? (
        <Card><div style={{ textAlign: 'center', padding: 32, color: '#999' }}>暂无连接的 Workers</div></Card>
      ) : (
        <Row gutter={[16, 16]}>
          {workers.map((w: any) => {
            const isStale = w.last_heartbeat && Date.now() - new Date(w.last_heartbeat).getTime() > 60000
            return (
              <Col key={w.worker_id} xs={24} md={12} lg={8}>
                <Card title={w.worker_id} extra={<Tag color="blue">{w.platform}</Tag>}>
                  <Descriptions column={1} size="small">
                    <Descriptions.Item label="版本">{w.version}</Descriptions.Item>
                    <Descriptions.Item label="能力">
                      {(w.capabilities || []).map((cap: string) => (
                        <Tag key={cap}>{cap}</Tag>
                      ))}
                    </Descriptions.Item>
                    <Descriptions.Item label="连接时间">
                      {new Date(w.connected_at).toLocaleString('zh-CN')}
                    </Descriptions.Item>
                    <Descriptions.Item label="最后心跳">
                      <Badge
                        status={isStale ? 'error' : 'success'}
                        text={new Date(w.last_heartbeat).toLocaleString('zh-CN')}
                      />
                    </Descriptions.Item>
                    {w.status && Object.keys(w.status).length > 0 && (
                      <Descriptions.Item label="状态">
                        {Object.entries(w.status).map(([k, v]) => (
                          <Tag key={k}>{k}: {String(v)}</Tag>
                        ))}
                      </Descriptions.Item>
                    )}
                  </Descriptions>
                </Card>
              </Col>
            )
          })}
        </Row>
      )}
    </div>
  )
}
