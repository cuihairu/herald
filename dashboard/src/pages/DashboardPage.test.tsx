import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'

vi.mock('../api', () => ({
  heraldApi: {
    getStatus: vi.fn().mockResolvedValue({ data: { status: 'running' } }),
    getProviders: vi.fn().mockResolvedValue({
      data: { providers: [{ name: 'feishu', status: 'available' }, { name: 'weird-one', status: 'down' }] },
    }),
    getLogsStats: vi.fn().mockResolvedValue({
      data: { total: 42, by_status: { success: 40, failed: 2 } },
    }),
    getWorkers: vi.fn().mockResolvedValue({ data: { workers: [] } }),
    getQueue: vi.fn().mockResolvedValue({ data: { size: 0 } }),
  },
}))

import DashboardPage from './DashboardPage'
import { heraldApi } from '../api'
import { useHeraldStore } from '../stores/herald'
import { resetStore } from '../test/store'

describe('DashboardPage', () => {
  beforeEach(() => {
    resetStore()
    vi.clearAllMocks()
  })

  it('shows a spinner while the first load is in flight and nothing is cached', () => {
    useHeraldStore.setState({ loading: true, status: null })
    const { container } = render(<DashboardPage />)
    expect(container.querySelector('.ant-spin')).toBeInTheDocument()
  })

  it('renders the six statistic cards and the provider list', async () => {
    render(<DashboardPage />)
    expect(await screen.findByText('running')).toBeInTheDocument()
    expect(screen.getByText('系统状态')).toBeInTheDocument()
    // Provider 数量 / 在线 Provider（available 只有一个）/ 通知总数 / 成功 / 失败
    expect(screen.getByText('Provider 数量')).toBeInTheDocument()
    expect(screen.getByText('在线 Provider')).toBeInTheDocument()
    expect(screen.getByText('通知总数')).toBeInTheDocument()
    expect(screen.getByText('成功')).toBeInTheDocument()
    expect(screen.getByText('失败')).toBeInTheDocument()
  })

  it('maps known provider names to Chinese labels and keeps unknown ones', async () => {
    render(<DashboardPage />)
    expect(await screen.findByText('飞书')).toBeInTheDocument()
    expect(screen.getByText('weird-one')).toBeInTheDocument()
  })

  it('marks unavailable providers with their raw status', async () => {
    render(<DashboardPage />)
    // 非 available 的 provider 显示原始状态文本（红色块）
    expect(await screen.findByText('down')).toBeInTheDocument()
  })

  it('falls back to "unknown" and zero stats when the loads fail', async () => {
    // fetch 失败后 loading 归位、status 仍为 null：未知状态 + 零值统计
    vi.mocked(heraldApi.getStatus).mockRejectedValueOnce({ message: 'x' })
    vi.mocked(heraldApi.getProviders).mockRejectedValueOnce({ message: 'x' })
    vi.mocked(heraldApi.getLogsStats).mockRejectedValueOnce({ message: 'x' })
    render(<DashboardPage />)
    expect(await screen.findByText('unknown')).toBeInTheDocument()
    const zeros = screen.getAllByText('0')
    expect(zeros.length).toBeGreaterThanOrEqual(3)
  })
})
