import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'

vi.mock('../api', () => ({
  heraldApi: {
    getProviders: vi.fn().mockResolvedValue({ data: { providers: [] } }),
    enableProvider: vi.fn().mockResolvedValue(undefined),
    disableProvider: vi.fn().mockResolvedValue(undefined),
  },
}))

import ProvidersPage from './ProvidersPage'
import { heraldApi } from '../api'
import { useHeraldStore } from '../stores/herald'
import { resetStore } from '../test/store'

const apiMocks = vi.mocked(heraldApi)

// 覆盖 getProviderType 的四个分支：sms / email / webhook / 其他
const providers = [
  { name: 'aliyunsms', status: 'available', enabled: true, type: 'builtin' },
  { name: 'email', status: 'available', enabled: false, type: 'plugin', since: '2026-09-01T08:00:00Z' },
  { name: 'webhook', status: 'down', enabled: true, type: 'builtin' },
  { name: 'feishu', status: 'down', enabled: false, type: 'plugin' },
]

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/providers']}>
      <Routes>
        <Route path="/" element={<div>elsewhere</div>} />
        <Route path="/providers" element={<ProvidersPage />} />
        <Route path="/providers/:name/config" element={<div>config-route</div>} />
      </Routes>
    </MemoryRouter>
  )
}

const user = userEvent.setup()

describe('ProvidersPage', () => {
  beforeEach(() => {
    resetStore()
    vi.clearAllMocks()
    apiMocks.getProviders.mockResolvedValue({ data: { providers } })
  })

  it('shows a spinner on the very first load', () => {
    apiMocks.getProviders.mockReturnValue(new Promise(() => {}))
    useHeraldStore.setState({ loading: true, providers: [] })
    const { container } = renderPage()
    expect(container.querySelector('.ant-spin')).toBeInTheDocument()
  })

  it('renders every provider card with its type bucket', async () => {
    renderPage()
    expect(await screen.findByText('阿里云短信')).toBeInTheDocument()
    expect(screen.getByText('类型: 短信')).toBeInTheDocument()
    expect(screen.getByText('类型: 邮件')).toBeInTheDocument()
    expect(screen.getByText('类型: Webhook')).toBeInTheDocument()
    expect(screen.getByText('类型: 即时通讯')).toBeInTheDocument()
    expect(screen.getByText('飞书')).toBeInTheDocument()
  })

  it('shows the launch time only when the provider reports one', async () => {
    renderPage()
    // 只有 email 带 since 字段
    expect(await screen.findByText(/启动时间/)).toBeInTheDocument()
    expect(screen.getAllByText(/启动时间/).length).toBe(1)
  })

  it('toggles a disabled provider on through the switch', async () => {
    renderPage()
    const switches = await screen.findAllByRole('switch')
    expect(switches.length).toBe(4)
    // email 卡片当前 enabled: false → 点击后走 enableProvider
    await user.click(switches[1])
    await waitFor(() => expect(apiMocks.enableProvider).toHaveBeenCalledWith('email'))
  })

  it('toggles an enabled provider off through the switch', async () => {
    renderPage()
    const switches = await screen.findAllByRole('switch')
    // aliyunsms 卡片当前 enabled: true → 点击后走 disableProvider
    await user.click(switches[0])
    await waitFor(() => expect(apiMocks.disableProvider).toHaveBeenCalledWith('aliyunsms'))
  })

  it('swallows toggle failures without crashing', async () => {
    apiMocks.disableProvider.mockRejectedValue({ message: 'refused' })
    renderPage()
    const switches = await screen.findAllByRole('switch')
    await user.click(switches[0])
    await waitFor(() => expect(apiMocks.disableProvider).toHaveBeenCalledWith('aliyunsms'))
    // 空 catch：卡片照常渲染
    expect(screen.getByText('阿里云短信')).toBeInTheDocument()
  })

  it('navigates to the config route from the settings button', async () => {
    renderPage()
    const buttons = await screen.findAllByRole('button', { name: /配\s*置/ })
    await user.click(buttons[2])
    expect(await screen.findByText('config-route')).toBeInTheDocument()
  })
})
