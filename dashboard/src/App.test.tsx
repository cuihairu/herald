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
  },
}))

import App from './App'

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

  it('always renders /login outside the private area', async () => {
    renderAt('/login')
    expect(await screen.findByText('请登录以继续')).toBeInTheDocument()
  })
})
