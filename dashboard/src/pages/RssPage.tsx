import { useEffect, useState } from 'react'
import {
  Table, Typography, Tag, Space, Button, message,
} from 'antd'
import { CopyOutlined } from '@ant-design/icons'
import { heraldApi } from '../api'
import { copyText, errMsg } from './ApiKeysPage'

export function feedUrlOrigin(): string {
  return window.location.origin
}

export default function RssPage() {
  const [publicFeeds, setPublicFeeds] = useState<any[]>([])
  const [privateFeeds, setPrivateFeeds] = useState<any[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    fetchFeeds()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function fetchFeeds() {
    setLoading(true)
    try {
      const res: any = await heraldApi.getFeeds()
      setPublicFeeds(res.data.public || [])
      setPrivateFeeds(res.data.private || [])
    } catch (err: any) {
      message.error(errMsg(err, '加载 feed 列表失败'))
    } finally {
      setLoading(false)
    }
  }

  async function handleCopy(url: string) {
    if (await copyText(feedUrlOrigin() + url)) {
      message.success('已复制 feed 地址')
    } else {
      message.error('复制失败')
    }
  }

  const columns = [
    {
      title: '名称', key: 'name',
      render: (_: any, feed: any) => feed.name || feed.audience,
    },
    {
      title: '地址', dataIndex: 'url', key: 'url',
      render: (url: string) => <Typography.Text code>{feedUrlOrigin() + url}</Typography.Text>,
    },
    { title: '条目数', dataIndex: 'total', key: 'total' },
    {
      title: '最近条目', dataIndex: 'recent', key: 'recent',
      render: (recent: any[]) =>
        (recent || []).map((it) => (
          <Tag key={it.id}>{it.title}</Tag>
        )),
    },
    {
      title: '操作', key: 'actions',
      render: (_: any, feed: any) => (
        <Button size="small" icon={<CopyOutlined />} onClick={() => handleCopy(feed.url)}>复制地址</Button>
      ),
    },
  ]

  return (
    <div>
      <Typography.Title level={4}>RSS Feeds</Typography.Title>
      <Typography.Paragraph type="secondary">
        公开 feed 按品类聚合；私密 feed 按受众发放，地址中的 token 即凭据，请勿外传。
      </Typography.Paragraph>
      <Typography.Title level={5}>公开 Feed</Typography.Title>
      <Table
        rowKey="url"
        columns={columns as any}
        dataSource={publicFeeds}
        loading={loading}
        scroll={{ x: 'max-content' }}
        pagination={false}
      />
      <Typography.Title level={5} style={{ marginTop: 24 }}>私密 Feed</Typography.Title>
      <Table
        rowKey="url"
        columns={columns as any}
        dataSource={privateFeeds}
        scroll={{ x: 'max-content' }}
        pagination={false}
      />
    </div>
  )
}
