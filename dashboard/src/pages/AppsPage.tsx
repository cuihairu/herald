import { useEffect, useState } from 'react'
import { Table, Typography, Button, Tag, Space, message } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { heraldApi } from '../api'
import { errMsg } from './ApiKeysPage'

export default function AppsPage() {
  const [apps, setApps] = useState<string[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    fetchApps()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function fetchApps() {
    setLoading(true)
    try {
      const res: any = await heraldApi.getApps()
      setApps(res.data.apps || [])
    } catch (err: any) {
      message.error(errMsg(err, '加载 Apps 失败'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div>
      <Space style={{ marginBottom: 16 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>Apps 集成命名空间</Typography.Title>
        <Button size="small" icon={<ReloadOutlined />} onClick={fetchApps}>刷新</Button>
      </Space>
      <Typography.Paragraph type="secondary">
        集成方以 app token 调用 /api/v1/apps/{'{app}'} 系列接口；命名空间与 token 在配置文件 seeds 里管理。
      </Typography.Paragraph>
      <Table
        rowKey={(name: string) => name}
        loading={loading}
        pagination={false}
        columns={[
          { title: '命名空间', key: 'name', render: (_: any, name: string) => <Typography.Text code>{name}</Typography.Text> },
          { title: '状态', key: 'status', render: () => <Tag color="green">已配置</Tag> },
        ]}
        dataSource={apps}
      />
    </div>
  )
}
