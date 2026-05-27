import { useState } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Layout, Menu, Button, theme } from 'antd'
import {
  DashboardOutlined,
  ApiOutlined,
  TeamOutlined,
  FileTextOutlined,
  SendOutlined,
  LogoutOutlined,
} from '@ant-design/icons'

const { Header, Sider, Content } = Layout

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
  { key: '/providers', icon: <ApiOutlined />, label: 'Providers' },
  { key: '/workers', icon: <TeamOutlined />, label: 'Workers' },
  { key: '/logs', icon: <FileTextOutlined />, label: '日志' },
  { key: '/send', icon: <SendOutlined />, label: '发送消息' },
]

export default function DashboardLayout() {
  const [collapsed, setCollapsed] = useState(false)
  const navigate = useNavigate()
  const location = useLocation()
  const { token: themeToken } = theme.useToken()

  function handleLogout() {
    localStorage.removeItem('herald_token')
    localStorage.removeItem('herald_user')
    navigate('/login')
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider collapsible collapsed={collapsed} onCollapse={setCollapsed}>
        <div
          style={{
            height: 32,
            margin: 16,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: themeToken.colorPrimary,
            fontWeight: 700,
            fontSize: collapsed ? 16 : 20,
          }}
        >
          {collapsed ? 'H' : 'Herald'}
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[location.pathname]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            padding: '0 24px',
            display: 'flex',
            justifyContent: 'flex-end',
            alignItems: 'center',
            background: themeToken.colorBgContainer,
          }}
        >
          <Button
            type="text"
            icon={<LogoutOutlined />}
            onClick={handleLogout}
          >
            登出
          </Button>
        </Header>
        <Content style={{ margin: 24 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  )
}
