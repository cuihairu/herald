import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('../api', () => ({
  heraldApi: {
    getStatus: vi.fn(),
    getProviders: vi.fn(),
    getWorkers: vi.fn(),
    getQueue: vi.fn(),
    getLogsStats: vi.fn(),
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
