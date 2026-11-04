import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import DashboardLayout from './DashboardLayout'
import ThemeModeProvider from '../theme/ThemeModeProvider'
import { THEME_STORAGE_KEY } from '../theme/themes'

vi.mock('../api', () => ({
  heraldApi: {
    getStatus: vi.fn().mockResolvedValue({ data: { status: 'running' } }),
    getProviders: vi.fn().mockResolvedValue({ data: { providers: [] } }),
    getWorkers: vi.fn().mockResolvedValue({ data: { workers: [] } }),
    getQueue: vi.fn().mockResolvedValue({ data: { size: 0 } }),
    getLogsStats: vi.fn().mockResolvedValue({ data: { total: 0 } }),
  },
}))

function renderLayout(initial = '/') {
  return render(
    <ThemeModeProvider>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route path="/login" element={<div>login-page</div>} />
          <Route path="/" element={<DashboardLayout />}>
            <Route index element={<div>home-outlet</div>} />
            <Route path="providers" element={<div>providers-route</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </ThemeModeProvider>
  )
}

const user = userEvent.setup()

describe('DashboardLayout', () => {
  beforeEach(() => {
    localStorage.setItem('herald_token', 't')
    localStorage.setItem('herald_user', '{"name":"admin"}')
  })
  afterEach(() => {
    cleanup()
    localStorage.clear()
  })

  it('shows the brand, the theme switcher, the menu and the outlet content', () => {
    renderLayout()
    expect(screen.getByAltText('Herald logo')).toBeInTheDocument()
    expect(screen.getAllByText('Herald').length).toBeGreaterThan(0)
    expect(screen.getByText('跟随系统')).toBeInTheDocument()
    expect(screen.getByText('亮色')).toBeInTheDocument()
    expect(screen.getByText('暗色')).toBeInTheDocument()
    expect(screen.getByText('午夜')).toBeInTheDocument()
    expect(screen.getByText('概览')).toBeInTheDocument()
    expect(screen.getByText('Providers')).toBeInTheDocument()
    expect(screen.getByText('通知规则')).toBeInTheDocument()
    expect(screen.getByText('通知群组')).toBeInTheDocument()
    expect(screen.getByText('Workers')).toBeInTheDocument()
    // antd 会给恰好两个汉字的文本插入排版空格。
    expect(screen.getByText(/日\s*志/)).toBeInTheDocument()
    expect(screen.getByText('发送消息')).toBeInTheDocument()
    expect(screen.getByText('home-outlet')).toBeInTheDocument()
  })

  it('navigates when a menu item is clicked', async () => {
    renderLayout()
    await user.click(screen.getByText('Providers'))
    expect(await screen.findByText('providers-route')).toBeInTheDocument()
  })

  it('logout clears credentials and returns to /login', async () => {
    renderLayout()
    await user.click(screen.getByText(/登\s*出/))
    expect(await screen.findByText('login-page')).toBeInTheDocument()
    expect(localStorage.getItem('herald_token')).toBeNull()
    expect(localStorage.getItem('herald_user')).toBeNull()
  })

  it('collapses the sider and keeps the header brand', async () => {
    renderLayout()
    // antd Sider 的 collapsible 触发器位于底部 trigger 条。
    const trigger = document.querySelector('.ant-layout-sider-trigger')!
    await user.click(trigger)
    // 品牌区在顶栏，折叠侧栏后仍可见。
    expect(await screen.findByAltText('Herald logo')).toBeInTheDocument()
    expect(screen.getAllByText('Herald').length).toBeGreaterThan(0)
  })

  it('switching the theme persists the manual pick', async () => {
    renderLayout()
    await user.click(screen.getByText('午夜'))
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('midnight')
    await user.click(screen.getByText('跟随系统'))
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('system')
  })
})
