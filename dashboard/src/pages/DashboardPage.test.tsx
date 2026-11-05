import { describe, it, expect, vi, beforeEach } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'

vi.mock('../api', () => ({
  heraldApi: {
    getOverview: vi.fn().mockResolvedValue({
      data: {
        providers: { enabled: 2, total: 3 },
        queue: { depth: 42 },
        delivery: { window: '24h', success: 40, failed: 2, total: 42, success_rate: 95.2 },
        workers: { online: 1, total: 1 },
      },
    }),
    getMetricsHistory: vi.fn().mockResolvedValue({
      data: {
        interval_seconds: 10,
        samples: [
          { time: '2026-11-05T10:00:00Z', cpu_percent: 12.5, rss_bytes: 2147483648, goroutines: 30, workers: 1 },
          { time: '2026-11-05T10:01:00Z', cpu_percent: 44.5, rss_bytes: 3221225472, goroutines: 32, workers: 2 },
        ],
      },
    }),
    getLogsTimeseries: vi.fn().mockResolvedValue({
      data: {
        bucket: '15m0s',
        points: [
          { time: '2026-11-05T10:00:00Z', total: 5, success: 4, failed: 1 },
          { time: '2026-11-05T10:15:00Z', total: 3, success: 3, failed: 0 },
        ],
      },
    }),
    getProviders: vi.fn().mockResolvedValue({
      data: { providers: [{ name: 'feishu', type: 'builtin', enabled: true, status: 'available', since: '2026-11-05T08:00:00Z' }] },
    }),
    getStatus: vi.fn().mockResolvedValue({ data: { status: 'running' } }),
    getLogsStats: vi.fn().mockResolvedValue({ data: { total: 0 } }),
    getWorkers: vi.fn().mockResolvedValue({ data: { workers: [] } }),
    getQueue: vi.fn().mockResolvedValue({ data: { size: 0 } }),
    enableProvider: vi.fn(),
    disableProvider: vi.fn(),
  },
}))

// jsdom 没有 canvas，echarts 初始化必然失败：图表组件在页面用例里替身，
// 真实渲染路径由 ChartLine 自己的用例覆盖。替身把 yAxis 轴标签 formatter
// 真实执行一次并把结果投到 DOM 上——echarts 内部才会调用的 formatter
// 在页面用例里只有经此才能验证（data-ylabel 断言）。
vi.mock('../components/ChartLine', () => ({
  default: ({ option }: { option: Record<string, unknown> }) => {
    const o = option as any
    const fmt = o?.yAxis?.axisLabel?.formatter
    return (
      <div
        data-testid="chart"
        data-series={JSON.stringify(o?.series?.length ?? 0)}
        data-ylabel={typeof fmt === 'function' ? fmt(12.5) : ''}
      />
    )
  },
}))

import DashboardPage, { rateTone, queueTone, rssGiB } from './DashboardPage'
import { heraldApi } from '../api'
import { useHeraldStore } from '../stores/herald'
import { resetStore } from '../test/store'

const api = vi.mocked(heraldApi, true)

function renderPage(initial = '/') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/providers/:name/config" element={<div>config-page</div>} />
      </Routes>
    </MemoryRouter>
  )
}

describe('threshold helpers', () => {
  it('grades the delivery rate', () => {
    expect(rateTone(0, 0)).toBe('default')
    expect(rateTone(79, 10)).toBe('error')
    expect(rateTone(90, 10)).toBe('warning')
    expect(rateTone(99, 10)).toBe('default')
  })

  it('grades the queue depth', () => {
    expect(queueTone(0)).toBe('default')
    expect(queueTone(101)).toBe('warning')
  })

  it('formats rss in GiB', () => {
    expect(rssGiB(1073741824)).toBe(1)
    expect(rssGiB(0)).toBe(0)
  })
})

describe('DashboardPage', () => {
  beforeEach(() => {
    resetStore()
    vi.clearAllMocks()
  })

  it('shows a spinner while the first load is in flight and nothing is cached', () => {
    useHeraldStore.setState({ loading: true, overview: null })
    const { container } = renderPage()
    expect(container.querySelector('.ant-spin')).toBeInTheDocument()
  })

  it('renders the four status cards, the provider table and the charts', async () => {
    renderPage()
    expect(await screen.findByText('启用中 Providers')).toBeInTheDocument()
    expect(screen.getByText('2 / 3')).toBeInTheDocument()
    expect(screen.getByText('队列深度')).toBeInTheDocument()
    expect(screen.getByText('24h 投递成功率')).toBeInTheDocument()
    expect(screen.getByText('95.2%')).toBeInTheDocument()
    expect(screen.getByText('在线 Workers')).toBeInTheDocument()
    expect(screen.getByText('1 / 1')).toBeInTheDocument()
    // provider 表：启用在前、状态标签可扫。
    expect(await screen.findByText('飞书')).toBeInTheDocument()
    expect(screen.getByText('已启用')).toBeInTheDocument()
    // 三张曲线图都已挂载；CPU 图的 yAxis formatter 真实产出 GiB 标签。
    expect(screen.getAllByTestId('chart')).toHaveLength(3)
    expect(screen.getAllByTestId('chart')[1]).toHaveAttribute('data-ylabel', '12.5 GiB')
  })

  it('renders the no-data placeholder when the window has no deliveries', async () => {
    api.getOverview.mockResolvedValueOnce({
      data: {
        providers: { enabled: 0, total: 0 },
        queue: { depth: 0 },
        delivery: { window: '24h', success: 0, failed: 0, total: 0, success_rate: 0 },
        workers: { online: 0, total: 0 },
      },
    } as any)
    renderPage()
    expect(await screen.findByText('暂无投递')).toBeInTheDocument()
  })

  it('colors the rate card when failures spike', async () => {
    api.getOverview.mockResolvedValueOnce({
      data: {
        providers: { enabled: 1, total: 1 },
        queue: { depth: 0 },
        delivery: { window: '24h', success: 1, failed: 9, total: 10, success_rate: 10 },
        workers: { online: 0, total: 0 },
      },
    } as any)
    renderPage()
    expect(await screen.findByText('10%')).toBeInTheDocument()
    expect(screen.getByText('10%')).toHaveStyle({ color: '#ff4d4f' })
  })

  it('switches the series window on demand', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('启用中 Providers')
    await user.click(screen.getByText('1h'))
    expect(api.getMetricsHistory).toHaveBeenLastCalledWith('1h')
    expect(api.getLogsTimeseries).toHaveBeenLastCalledWith('1h')
    expect(useHeraldStore.getState().seriesWindow).toBe('1h')
  })

  it('colors warning states and falls back for unknown providers', async () => {
    api.getOverview.mockResolvedValueOnce({
      data: {
        providers: { enabled: 1, total: 2 },
        queue: { depth: 150 },
        delivery: { window: '24h', success: 88, failed: 12, total: 100, success_rate: 88 },
        workers: { online: 0, total: 1 },
      },
    } as any)
    api.getProviders.mockResolvedValueOnce({
      data: { providers: [{ name: 'slackwall', type: 'builtin', enabled: false, status: 'down' }] },
    } as any)
    renderPage()
    // 成功率落在警示带：卡片数值走 colorWarning；队列深度 >100 同样警示色。
    expect(await screen.findByText('88%')).toBeInTheDocument()
    expect(screen.getByText('88%')).toHaveStyle({ color: '#faad14' })
    expect(screen.getByText('150')).toHaveStyle({ color: '#faad14' })
    // 未知名称走 `|| p.name` 兜底；禁用、不可用与缺失 since 各走兜底臂。
    expect(screen.getByText('slackwall')).toBeInTheDocument()
    expect(screen.getByText('已禁用')).toBeInTheDocument()
    expect(screen.getByText('异常')).toBeInTheDocument()
    expect(screen.getByText('-')).toBeInTheDocument()
  })

  it('navigates to the provider config page', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('飞书')
    await user.click(screen.getByRole('button', { name: /配\s*置/ }))
    expect(await screen.findByText('config-page')).toBeInTheDocument()
  })

  it('polls the overview and current series while mounted', async () => {
    // 伪时钟必须在挂载前接管：setInterval 是 effect 里注册的真实定时器，
    // 挂载后再换时钟就追不到它了。shouldAdvanceTime 让渲染侧的真实
    // 微任务照常冲刷，只有 15s 的轮询间隔由这里手动推进。
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      renderPage()
      await act(async () => {
        await vi.advanceTimersByTimeAsync(15000)
      })
      // 轮询按当前窗口口径重取：新一次 overview + series。
      expect(api.getOverview.mock.calls.length).toBe(2)
      expect(api.getMetricsHistory.mock.calls.length).toBe(2)
      expect(api.getMetricsHistory).toHaveBeenLastCalledWith('6h')
    } finally {
      vi.useRealTimers()
    }
  })
})
