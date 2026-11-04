import { useState } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Layout, Menu, Button, Segmented, theme } from 'antd'
import {
  DashboardOutlined,
  ApiOutlined,
  FilterOutlined,
  UsergroupAddOutlined,
  CalendarOutlined,
  TeamOutlined,
  AlertOutlined,
  FileTextOutlined,
  FieldTimeOutlined,
  SendOutlined,
  LogoutOutlined,
} from '@ant-design/icons'
import BrandLogo from '../components/BrandLogo'
import { useThemeMode } from '../theme/ThemeModeProvider'
import type { ThemeMode } from '../theme/themes'

const { Header, Sider, Content } = Layout

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: '概览' },
  { key: '/providers', icon: <ApiOutlined />, label: 'Providers' },
  { key: '/rules', icon: <FilterOutlined />, label: '通知规则' },
  { key: '/groups', icon: <UsergroupAddOutlined />, label: '通知群组' },
  { key: '/rosters', icon: <CalendarOutlined />, label: '值班表' },
  { key: '/incidents', icon: <AlertOutlined />, label: '事件台账' },
  { key: '/workers', icon: <TeamOutlined />, label: 'Workers' },
  { key: '/logs', icon: <FileTextOutlined />, label: '日志' },
  { key: '/deliveries', icon: <FieldTimeOutlined />, label: '投递状态' },
  { key: '/send', icon: <SendOutlined />, label: '发送消息' },
]

const themeOptions: { label: string; value: ThemeMode }[] = [
  { label: '跟随系统', value: 'system' },
  { label: '亮色', value: 'light' },
  { label: '暗色', value: 'dark' },
  { label: '午夜', value: 'midnight' },
]

export default function DashboardLayout() {
  const [collapsed, setCollapsed] = useState(false)
  const navigate = useNavigate()
  const location = useLocation()
  const { token: themeToken } = theme.useToken()
  const { mode, setMode } = useThemeMode()

  function handleLogout() {
    localStorage.removeItem('herald_token')
    localStorage.removeItem('herald_user')
    navigate('/login')
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        collapsible
        collapsed={collapsed}
        onCollapse={setCollapsed}
        breakpoint="lg"
        collapsedWidth={64}
      >
        <Menu
          mode="inline"
          selectedKeys={[location.pathname]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
          style={{ marginTop: 8, borderInlineEnd: 'none' }}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            padding: '0 24px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            background: themeToken.colorBgContainer,
            borderBottom: `1px solid ${themeToken.colorBorderSecondary}`,
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <BrandLogo size={26} />
            <span style={{ fontWeight: 600, fontSize: 16, color: themeToken.colorText }}>
              Herald
            </span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
            <Segmented
              size="small"
              options={themeOptions}
              value={mode}
              onChange={(value) => setMode(value as ThemeMode)}
            />
            <Button type="text" icon={<LogoutOutlined />} onClick={handleLogout}>
              登出
            </Button>
          </div>
        </Header>
        <Content style={{ margin: 24 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  )
}
