import { useEffect, useState } from 'react'
import {
  Table, Typography, Modal, Tabs, Tag, Space, Button, message,
} from 'antd'
import { CopyOutlined } from '@ant-design/icons'
import { heraldApi } from '../api'
import { copyText, errMsg } from './ApiKeysPage'

// 关系类型 → 展示色。type 字段来自 /relations 的 wire 契约（subscription 等）。
const typeColors: Record<string, string> = { subscription: 'green', must_deliver: 'red' }

export default function AudiencesPage() {
  const [audiences, setAudiences] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [detail, setDetail] = useState<any>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  useEffect(() => {
    fetchAudiences()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function fetchAudiences() {
    setLoading(true)
    try {
      const res: any = await heraldApi.getAudiences()
      setAudiences(res.data.audiences || [])
    } catch (err: any) {
      message.error(errMsg(err, '加载受众失败'))
    } finally {
      setLoading(false)
    }
  }

  async function openDetail(aud: any) {
    setDetail(aud)
    setDetailLoading(true)
    try {
      const [surfaces, relations]: any[] = await Promise.all([
        heraldApi.getAudienceSurfaces(aud.id).catch((err: any) => ({ error: err })),
        heraldApi.getAudienceRelations(aud.id).catch((err: any) => ({ error: err })),
      ])
      setDetail({
        ...aud,
        surfaces: surfaces.error ? null : surfaces.data.surfaces || [],
        rssToken: surfaces.error ? '' : surfaces.data.rss_token || '',
        relations: relations.error ? null : relations.data.relations || [],
      })
    } finally {
      setDetailLoading(false)
    }
  }

  async function handleCopyToken() {
    if (await copyText(detail.rssToken)) {
      message.success('已复制私密 feed token')
    } else {
      message.error('复制失败')
    }
  }

  const columns = [
    { title: '受众 ID', dataIndex: 'id', key: 'id' },
    {
      title: '收件人', dataIndex: 'recipients', key: 'recipients',
      render: (recipients: string[]) => (recipients || []).join(', ') || '—',
    },
    {
      title: '操作', key: 'actions',
      render: (_: any, aud: any) => (
        <Button size="small" onClick={() => openDetail(aud)}>查看详情</Button>
      ),
    },
  ]

  return (
    <div>
      <Typography.Title level={4}>受众与订阅</Typography.Title>
      <Table
        rowKey="id"
        columns={columns as any}
        dataSource={audiences}
        loading={loading}
        scroll={{ x: 'max-content' }}
        pagination={false}
      />

      <Modal
        title={detail ? `受众 ${detail.id}` : ''}
        open={detail !== null}
        onCancel={() => setDetail(null)}
        onOk={() => setDetail(null)}
        okText="关闭"
        footer={null}
        destroyOnHidden
      >
        <Tabs
          items={[
            {
              key: 'relations',
              label: '订阅关系',
              children: (
                <Table
                  rowKey={(r: any) => `${r.category}-${r.channel}`}
                  loading={detailLoading}
                  size="small"
                  pagination={false}
                  columns={[
                    { title: '品类', dataIndex: 'category', key: 'category' },
                    { title: '渠道', dataIndex: 'channel', key: 'channel' },
                    {
                      title: '类型', dataIndex: 'type', key: 'type',
                      render: (t: string) => <Tag color={typeColors[t]}>{t}</Tag>,
                    },
                    { title: '来源', dataIndex: 'source', key: 'source' },
                  ]}
                  dataSource={detail?.relations ?? []}
                />
              ),
            },
            {
              key: 'surfaces',
              label: '联系面',
              children: (
                <div>
                  <Table
                    rowKey={(r: any) => `${r.channel}-${r.target}`}
                    loading={detailLoading}
                    size="small"
                    pagination={false}
                    columns={[
                      { title: '渠道', dataIndex: 'channel', key: 'channel' },
                      { title: '目标', dataIndex: 'target', key: 'target' },
                      {
                        title: '状态', dataIndex: 'status', key: 'status',
                        render: (s: string) => <Tag>{s}</Tag>,
                      },
                    ]}
                    dataSource={detail?.surfaces ?? []}
                  />
                  {detail?.rssToken ? (
                    <Space style={{ marginTop: 12 }}>
                      <Typography.Text type="secondary">私密 RSS feed token：</Typography.Text>
                      <Typography.Text code>{detail.rssToken}</Typography.Text>
                      <Button size="small" icon={<CopyOutlined />} onClick={handleCopyToken}>复制</Button>
                    </Space>
                  ) : null}
                </div>
              ),
            },
          ]}
        />
      </Modal>
    </div>
  )
}
