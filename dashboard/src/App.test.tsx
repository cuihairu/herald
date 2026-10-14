import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

vi.mock('./api', () => ({
  heraldApi: {
    getStatus: vi.fn().mockResolvedValue({ data: { status: 'running' } }),
    getProviders: vi.fn().mockResolvedValue({ data: { providers: [] } }),
    getWorkers: vi.fn().mockResolvedValue({ data: { workers: [] } }),
    getQueue: vi.fn().mockResolvedValue({ data: { size: 0 } }),
    sendNotify: vi.fn(),
    enableProvider: vi.fn(),
    disableProvider: vi.fn(),
    getLogs: vi.fn(),
    getLogsStats: vi.fn().mockResolvedValue({ data: { total: 0 } }),
    login: vi.fn(),
    getRules: vi.fn().mockResolvedValue({ data: { rules: [] } }),
    getGroups: vi.fn().mockResolvedValue({ data: { groups: [] } }),
    getRosters: vi.fn().mockResolvedValue({ data: { rosters: [] } }),
    getIncidents: vi.fn().mockResolvedValue({ data: { incidents: [] } }),
  },
}))

import App, { PageLoading } from './App'

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>
  )
}

describe('App routing', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('redirects anonymous visitors from / to /login', async () => {
    renderAt('/')
    expect(await screen.findByText('请登录以继续')).toBeInTheDocument()
  })

  it('redirects anonymous visitors trying a protected route', async () => {
    renderAt('/workers')
    expect(await screen.findByText('请登录以继续')).toBeInTheDocument()
  })

  it('renders the dashboard for logged-in users at /', async () => {
    localStorage.setItem('herald_token', 't')
    renderAt('/')
    // 菜单项与页面标题都叫「仪表盘」，findAllByText 返回数组
    expect((await screen.findAllByText('仪表盘')).length).toBeGreaterThan(0)
  })

  it('mounts every protected child route', async () => {
    localStorage.setItem('herald_token', 't')
    renderAt('/providers')
    expect(await screen.findByText('Providers')).toBeInTheDocument()
  })

  it('mounts the rules and groups pages', async () => {
    localStorage.setItem('herald_token', 't')
    renderAt('/rules')
    // 菜单项与页面标题都叫「通知规则」，findAllByText 返回数组。
    expect((await screen.findAllByText('通知规则')).length).toBeGreaterThan(0)
  })

  it('mounts the rosters page', async () => {
    localStorage.setItem('herald_token', 't')
    renderAt('/rosters')
    // 菜单项与页面标题都叫「值班表」，findAllByText 返回数组。
    expect((await screen.findAllByText('值班表')).length).toBeGreaterThan(0)
  })

  it('mounts the incidents page', async () => {
    localStorage.setItem('herald_token', 't')
    renderAt('/incidents')
    // 菜单项与页面标题都叫「事件台账」，findAllByText 返回数组。
    expect((await screen.findAllByText('事件台账')).length).toBeGreaterThan(0)
  })

  it('mounts the groups page', async () => {
    localStorage.setItem('herald_token', 't')
    renderAt('/groups')
    expect(await screen.findByText('新建群组')).toBeInTheDocument()
  })

  it('mounts the logs page', async () => {
    localStorage.setItem('herald_token', 't')
    renderAt('/logs')
    expect(await screen.findByText('任务日志')).toBeInTheDocument()
  })

  it('mounts the deliveries page', async () => {
    localStorage.setItem('herald_token', 't')
    // DeliveriesPage 用原生 fetch 读投递状态机，不经 heraldApi mock
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve({ ok: true, json: async () => ({ code: 0, data: {} }) }))
    )
    renderAt('/deliveries')
    // 菜单项与页面标题都叫「投递状态」，限定 h4 只取页面标题
    expect(await screen.findByText('投递状态', { selector: 'h4' })).toBeInTheDocument()
    vi.unstubAllGlobals()
  })

  it('mounts the send page', async () => {
    localStorage.setItem('herald_token', 't')
    renderAt('/send')
    // 菜单项与页面标题都叫「发送消息」，findAllByText 返回数组
    expect((await screen.findAllByText('发送消息')).length).toBeGreaterThan(0)
  })

  it('mounts the provider config page', async () => {
    localStorage.setItem('herald_token', 't')
    // ProviderConfigPage 用原生 fetch 读配置，不经 heraldApi mock；
    // 非空 schema 才渲染表单与「测试连接」按钮
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve({
          ok: true,
          json: async () => ({
            code: 0,
            data: { config: { token: '' }, schema: { token: 'string' } },
          }),
        })
      )
    )
    try {
      renderAt('/providers/feishu/config')
      expect(await screen.findByText('测试连接')).toBeInTheDocument()
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('mounts the workers page for logged-in users', async () => {
    localStorage.setItem('herald_token', 't')
    renderAt('/workers')
    expect(await screen.findByText('连接数量')).toBeInTheDocument()
  })

  it('always renders /login outside the private area', async () => {
    renderAt('/login')
    expect(await screen.findByText('请登录以继续')).toBeInTheDocument()
  })

  it('renders the loading fallback for lazy routes', () => {
    // 直测 fallback 组件：vitest 里 lazy 导入同步完成，路由测试中
    // Suspense fallback 不会真正渲染（仓库约定：行为断言优先）。
    render(<PageLoading />)
    expect(document.querySelector('.ant-spin')).toBeInTheDocument()
  })
})
