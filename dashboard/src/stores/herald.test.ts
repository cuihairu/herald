import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('../api', () => ({
  heraldApi: {
    getStatus: vi.fn(),
    getOverview: vi.fn(),
    getMetricsHistory: vi.fn(),
    getLogsTimeseries: vi.fn(),
    getProviders: vi.fn(),
    getWorkers: vi.fn(),
    getQueue: vi.fn(),
    getLogsStats: vi.fn(),
    getRules: vi.fn(),
    getGroups: vi.fn(),
    getRosters: vi.fn(),
    sendNotify: vi.fn(),
    enableProvider: vi.fn(),
    disableProvider: vi.fn(),
  },
}))

import { heraldApi } from '../api'
import { useHeraldStore } from './herald'
import { resetStore } from '../test/store'

const api = vi.mocked(heraldApi, true)
const state = () => useHeraldStore.getState()

// 每组用例共享的「成功返回」桩数据。
const okStatus = { data: { status: 'running' } }
const okProviders = { data: { providers: [{ name: 'feishu' }] } }
const okWorkers = { data: { workers: [{ worker_id: 'w1' }] } }
const okQueue = { data: { size: 3 } }
const okStats = { data: { total: 9 } }

describe('herald store', () => {
  beforeEach(() => {
    resetStore()
    vi.clearAllMocks()
  })

  it('starts from a clean snapshot', () => {
    expect(state()).toMatchObject({
      status: null,
      providers: [],
      workers: [],
      queue: { size: 0 },
      logStats: null,
      loading: false,
      error: null,
    })
  })

  it('fetchOverview stores the aggregate payload and reports errors', async () => {
    const okOverview = { data: { providers: { enabled: 1, total: 2 }, queue: { depth: 4 } } }
    api.getOverview.mockResolvedValue(okOverview as any)
    await state().fetchOverview()
    expect(state().overview).toEqual(okOverview.data)
    expect(state().loading).toBe(false)

    api.getOverview.mockRejectedValue({ message: 'overview down' })
    await state().fetchOverview()
    expect(state().error).toBe('overview down')
    expect(state().loading).toBe(false)
  })

  it('fetchSeries loads metrics and send volume for the window', async () => {
    api.getMetricsHistory.mockResolvedValue({ data: { samples: [{ cpu_percent: 5 }] } } as any)
    api.getLogsTimeseries.mockResolvedValue({ data: { points: [{ total: 2 }] } } as any)
    await state().fetchSeries('1h')
    expect(api.getMetricsHistory).toHaveBeenCalledWith('1h')
    expect(api.getLogsTimeseries).toHaveBeenCalledWith('1h')
    expect(state().metrics).toEqual([{ cpu_percent: 5 }])
    expect(state().sendSeries).toEqual([{ total: 2 }])
    expect(state().seriesWindow).toBe('1h')
    expect(state().seriesLoading).toBe(false)

    api.getMetricsHistory.mockRejectedValue({ message: 'series down' })
    await state().fetchSeries('24h')
    expect(state().error).toBe('series down')
    expect(state().seriesLoading).toBe(false)
    expect(state().seriesWindow).toBe('24h')
  })

  it('drops a stale series response once a newer request wins', async () => {
    let resolveSlowMetrics!: (v: any) => void
    let resolveSlowSend!: (v: any) => void
    api.getMetricsHistory.mockImplementation(((w: string) =>
      w === '1h'
        ? new Promise((res) => { resolveSlowMetrics = res })
        : Promise.resolve({ data: { samples: [{ cpu_percent: 1 }] } })) as any)
    api.getLogsTimeseries.mockImplementation(((w: string) =>
      w === '1h'
        ? new Promise((res) => { resolveSlowSend = res })
        : Promise.resolve({ data: { points: [{ total: 1 }] } })) as any)

    const slow = state().fetchSeries('1h')
    await state().fetchSeries('6h')
    expect(state().metrics).toEqual([{ cpu_percent: 1 }])
    expect(state().sendSeries).toEqual([{ total: 1 }])

    // 慢请求迟到：序列号过期，成功臂早退，不得覆盖 6h 的数据。
    resolveSlowMetrics({ data: { samples: [{ cpu_percent: 99 }] } })
    resolveSlowSend({ data: { points: [{ total: 9 }] } })
    await slow
    expect(state().metrics).toEqual([{ cpu_percent: 1 }])
    expect(state().sendSeries).toEqual([{ total: 1 }])
    expect(state().seriesLoading).toBe(false)
  })

  it('drops a stale series error once a newer request wins', async () => {
    let rejectSlowMetrics!: (v: any) => void
    let rejectSlowSend!: (v: any) => void
    api.getMetricsHistory.mockImplementation(((w: string) =>
      w === '1h'
        ? new Promise((_res, rej) => { rejectSlowMetrics = rej })
        : Promise.resolve({ data: { samples: [] } })) as any)
    api.getLogsTimeseries.mockImplementation(((w: string) =>
      w === '1h'
        ? new Promise((_res, rej) => { rejectSlowSend = rej })
        : Promise.resolve({ data: { points: [] } })) as any)

    const slow = state().fetchSeries('1h')
    await state().fetchSeries('6h')
    expect(state().error).toBeNull()

    // 迟到的失败同样过期：错误臂早退，错误状态不被旧请求写入。
    rejectSlowMetrics({ message: 'late failure' })
    rejectSlowSend({ message: 'late failure' })
    await slow
    expect(state().error).toBeNull()
    expect(state().seriesLoading).toBe(false)
  })

  it('defaults missing samples and points to empty arrays', async () => {
    // `samples || []` / `points || []` 兜底臂：成功响应缺字段而不是空数组。
    api.getMetricsHistory.mockResolvedValue({ data: {} } as any)
    api.getLogsTimeseries.mockResolvedValue({ data: {} } as any)
    await state().fetchSeries('1h')
    expect(state().metrics).toEqual([])
    expect(state().sendSeries).toEqual([])
    expect(state().error).toBeNull()
  })

  it('fetchStatus stores the payload on success and reports errors', async () => {
    api.getStatus.mockResolvedValue(okStatus as any)
    await state().fetchStatus()
    expect(state().status).toEqual({ status: 'running' })
    expect(state().loading).toBe(false)

    api.getStatus.mockRejectedValue({ message: 'down' })
    await state().fetchStatus()
    expect(state().error).toBe('down')
    expect(state().loading).toBe(false)
  })

  it('fetchProviders stores the provider list and reports errors', async () => {
    api.getProviders.mockResolvedValue(okProviders as any)
    await state().fetchProviders()
    expect(state().providers).toEqual([{ name: 'feishu' }])

    api.getProviders.mockRejectedValue({ message: 'nope' })
    await state().fetchProviders()
    expect(state().error).toBe('nope')
  })

  it('fetchWorkers defaults to an empty list and reports errors', async () => {
    api.getWorkers.mockResolvedValue({ data: {} } as any)
    await state().fetchWorkers()
    expect(state().workers).toEqual([])

    api.getWorkers.mockResolvedValue(okWorkers as any)
    await state().fetchWorkers()
    expect(state().workers).toEqual([{ worker_id: 'w1' }])

    api.getWorkers.mockRejectedValue({ message: 'gone' })
    await state().fetchWorkers()
    expect(state().error).toBe('gone')
  })

  it('fetchQueue stores the queue and reports errors', async () => {
    api.getQueue.mockResolvedValue(okQueue as any)
    await state().fetchQueue()
    expect(state().queue).toEqual({ size: 3 })

    api.getQueue.mockRejectedValue({ message: 'queue down' })
    await state().fetchQueue()
    expect(state().error).toBe('queue down')
  })

  it('fetchLogStats stores the stats and reports errors', async () => {
    api.getLogsStats.mockResolvedValue(okStats as any)
    await state().fetchLogStats()
    expect(state().logStats).toEqual({ total: 9 })

    api.getLogsStats.mockRejectedValue({ message: 'stats down' })
    await state().fetchLogStats()
    expect(state().error).toBe('stats down')
  })

  it('fetchRules defaults to an empty list and reports errors', async () => {
    api.getRules.mockResolvedValue({ data: {} } as any)
    await state().fetchRules()
    expect(state().rules).toEqual([])

    api.getRules.mockResolvedValue({ data: { rules: [{ id: 'p1' }] } } as any)
    await state().fetchRules()
    expect(state().rules).toEqual([{ id: 'p1' }])

    api.getRules.mockRejectedValue({ message: 'rules down' })
    await state().fetchRules()
    expect(state().error).toBe('rules down')
  })

  it('fetchGroups defaults to an empty list and reports errors', async () => {
    api.getGroups.mockResolvedValue({ data: {} } as any)
    await state().fetchGroups()
    expect(state().groups).toEqual([])

    api.getGroups.mockResolvedValue({ data: { groups: [{ id: 'ops' }] } } as any)
    await state().fetchGroups()
    expect(state().groups).toEqual([{ id: 'ops' }])

    api.getGroups.mockRejectedValue({ message: 'groups down' })
    await state().fetchGroups()
    expect(state().error).toBe('groups down')
  })

  it('fetchRosters defaults to an empty list and reports errors', async () => {
    api.getRosters.mockResolvedValue({ data: {} } as any)
    await state().fetchRosters()
    expect(state().rosters).toEqual([])

    api.getRosters.mockResolvedValue({ data: { rosters: [{ id: 'ops-oncall' }] } } as any)
    await state().fetchRosters()
    expect(state().rosters).toEqual([{ id: 'ops-oncall' }])

    api.getRosters.mockRejectedValue({ message: 'rosters down' })
    await state().fetchRosters()
    expect(state().error).toBe('rosters down')
  })

  it('sendNotify resolves on success', async () => {
    const res = { data: { code: 0 } }
    api.sendNotify.mockResolvedValue(res as any)
    await expect(state().sendNotify({ title: 'hi' })).resolves.toBe(res)
    expect(state().loading).toBe(false)
  })

  it('sendNotify rethrows and records the error', async () => {
    api.sendNotify.mockRejectedValue({ message: 'send failed' })
    await expect(state().sendNotify({ title: 'hi' })).rejects.toEqual({ message: 'send failed' })
    expect(state().error).toBe('send failed')
    expect(state().loading).toBe(false)
  })

  it('enableProvider refreshes the provider list afterwards', async () => {
    api.enableProvider.mockResolvedValue(undefined as any)
    api.getProviders.mockResolvedValue(okProviders as any)
    await state().enableProvider('feishu')
    expect(api.enableProvider).toHaveBeenCalledWith('feishu')
    expect(api.getProviders).toHaveBeenCalled()
    expect(state().loading).toBe(false)

    api.enableProvider.mockRejectedValue({ message: 'enable refused' })
    await expect(state().enableProvider('feishu')).rejects.toEqual({ message: 'enable refused' })
    expect(state().error).toBe('enable refused')
  })

  it('disableProvider refreshes the provider list afterwards', async () => {
    api.disableProvider.mockResolvedValue(undefined as any)
    api.getProviders.mockResolvedValue(okProviders as any)
    await state().disableProvider('feishu')
    expect(api.disableProvider).toHaveBeenCalledWith('feishu')
    expect(api.getProviders).toHaveBeenCalled()

    api.disableProvider.mockRejectedValue({ message: 'disable refused' })
    await expect(state().disableProvider('feishu')).rejects.toEqual({ message: 'disable refused' })
    expect(state().error).toBe('disable refused')
  })
})
