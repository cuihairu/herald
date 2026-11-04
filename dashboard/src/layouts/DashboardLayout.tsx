import { useState } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Layout, Menu, Button, Segmented, theme } from 'antd'
import {
  DashboardOutlined,
  ApiOutlined,
  FilterOutlined,
  UsergroupAddOutlined,
  UserOutlined,
  CalendarOutlined,
  TeamOutlined,
  AlertOutlined,
  FileTextOutlined,
  FileDoneOutlined,
  AppstoreOutlined,
  FieldTimeOutlined,
  ReadOutlined,
  KeyOutlined,
  SettingOutlined,
  SendOutlined,
  LogoutOutlined,
} from '@ant-design/icons'
import BrandLogo from '../components/BrandLogo'
import { useThemeMode } from '../theme/ThemeModeProvider'
import type { ThemeMode } from '../theme/themes'

const { Header, Sider, Content } = Layout

// 菜单按功能域分组（antd Menu 的 group 类型）。分组只改变视觉层级，
// key 仍是路由路径，点击行为与扁平菜单一致。
const menuItems = [
  {
    key: 'grp-overview',
    type: "group" as const,
    label: '总览',
    children: [
      { key: '/', icon: <DashboardOutlined />, label: '概览' },
      { key: '/send', icon: <SendOutlined />, label: '发送' },
    ],
  },
  {
    key: 'grp-providers',
    type: "group" as const,
    label: 'Providers 接入',
    children: [
      { key: '/providers', icon: <ApiOutlined />, label: 'Providers' },
      { key: '/workers', icon: <TeamOutlined />, label: 'Workers' },
    ],
  },
  {
    key: 'grp-audience',
    type: "group" as const,
    label: '受众与订阅',
    children: [
      { key: '/audiences', icon: <UsergroupAddOutlined />, label: '受众' },
      { key: '/groups', icon: <UserOutlined />, label: '通知群组' },
      { key: '/rosters', icon: <CalendarOutlined />, label: '值班表' },
    ],
  },
  {
    key: 'grp-templates',
    type: "group" as const,
    label: '模板',
    children: [
      { key: '/templates', icon: <FileTextOutlined />, label: '通知模板' },
    ],
  },
  {
    key: 'grp-rules',
    type: "group" as const,
    label: '规则与策略',
    children: [
      { key: '/rules', icon: <FilterOutlined />, label: '通知规则' },
    ],
  },
  {
    key: 'grp-apps',
    type: "group" as const,
    label: '集成 Apps',
    children: [
      { key: '/apps', icon: <AppstoreOutlined />, label: 'Apps' },
    ],
  },
  {
    key: 'grp-alerts',
    type: "group" as const,
    label: '告警',
    children: [
      { key: '/incidents', icon: <AlertOutlined />, label: '事件台账' },
    ],
  },
  {
    key: 'grp-delivery',
    type: "group" as const,
    label: '日志与投递',
    children: [
      { key: '/logs', icon: <FileDoneOutlined />, label: '日志' },
      { key: '/deliveries', icon: <FieldTimeOutlined />, label: '投递状态' },
    ],
  },
  {
    key: 'grp-rss',
    type: "group" as const,
    label: 'RSS 订阅',
    children: [
      { key: '/rss', icon: <ReadOutlined />, label: 'RSS' },
    ],
  },
  {
    key: 'grp-keys',
    type: "group" as const,
    label: '密钥管理',
    children: [
      { key: '/keys', icon: <KeyOutlined />, label: 'API Keys' },
    ],
  },
  {
    key: 'grp-settings',
    type: "group" as const,
    label: '系统',
    children: [
      { key: '/settings', icon: <SettingOutlined />, label: '设置' },
    ],
  },
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
