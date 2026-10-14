import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import DeliveriesPage, { renderLevel } from './DeliveriesPage'

// 数据形状刻意覆盖所有 render 分支：
// - status 'weird'（未知颜色 → default）
// - level 缺失（→ '-'）与 'nope'（未知色 → default）
// - last_error 缺失（→ '-'）与 'boom'
// - max_attempts 0（→ '-'）与 3（→ '1/3'）
// - next_retry_at / updated_at 缺失（→ '-'）
const rows = [
  {
    id: 'd1', provider: 'feishu', status: 'delivered', level: 'info',
    retry_count: 1, max_attempts: 3, last_error: '',
    created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:01Z',
    next_retry_at: '2026-10-14T00:01:00Z',
  },
  {
    id: 'd2', provider: 'smtp', status: 'weird', level: 'nope',
    retry_count: 0, max_attempts: 0, last_error: 'boom',
    created_at: '2026-10-14T00:00:00Z', updated_at: '', next_retry_at: '',
  },
]

const stats = { total: 11, by_status: { delivered: 5, failed: 3, dead: 2, retrying: 1 } }
const providers = { code: 0, data: { providers: [{ name: 'feishu' }, { name: 'smtp' }, { name: 'feishu' }] } }

function jsonResponse(body: unknown) {
  return { ok: true, json: async () => body }
}

function stubFetch(routes: Record<string, unknown>) {
  const fetchMock = vi.fn((url: string) => {
    const path = url.split('?')[0]
    const body = routes[path] ?? { code: 1, message: 'no route stubbed' }
    return Promise.resolve(jsonResponse(body))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const user = userEvent.setup()

describe('DeliveriesPage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  beforeEach(() => {
    localStorage.setItem('herald_token', 't')
    stubFetch({
      '/api/v1/deliveries': { code: 0, data: { deliveries: rows, total: 2 } },
      '/api/v1/deliveries/stats': { code: 0, data: stats },
      '/api/v1/providers': providers,
    })
  })

  it('renders the exported level cell both ways', () => {
    render(<>{renderLevel('nope')}{renderLevel('')}</>)
    const tag = screen.getByText('nope')
    expect(tag.className).toContain('ant-tag-default')
    expect(screen.getByText('-')).toBeInTheDocument()
  })

  it('loads deliveries, stats and the provider filter list on mount', async () => {
    render(<DeliveriesPage />)
    // 统计卡与筛选器（数据异步加载，先等行渲染出来）
    expect(screen.getByText('投递状态', { selector: 'h4' })).toBeInTheDocument()
    await screen.findByText('boom')
    expect(screen.getByText('总数')).toBeInTheDocument()
    expect(screen.getByText('重试耗尽')).toBeInTheDocument()
    // 已知生命周期与已知级别都渲成标签
    expect(screen.getByText('delivered')).toBeInTheDocument()
    expect(screen.getByText('info')).toBeInTheDocument()
    // 重试进度 1/3
    expect(screen.getByText('1/3')).toBeInTheDocument()
  })

  it('renders every column fallback branch', async () => {
    render(<DeliveriesPage />)
    await screen.findByText('boom')
    // status 'weird' 与 level 'nope' 原样显示且走 default 色
    expect(screen.getByText('weird').className).toContain('ant-tag-default')
    expect(screen.getByText('nope').className).toContain('ant-tag-default')
    // d2：max_attempts 0 → '-'；updated_at 缺失 → '-'；next_retry_at 缺失 → '-'
    // d1：last_error 空 → '-'
    const dashes = screen.getAllByText('-')
    expect(dashes.length).toBeGreaterThanOrEqual(4)
  })

  it('renders a formatted timestamp for rows that have one', async () => {
    render(<DeliveriesPage />)
    // 不硬编码渲染串（CI 跑 UTC，zh-CN 本地化串随时区变）；只验非短横线。
    await waitFor(() => {
      const cells = [...document.querySelectorAll('.ant-table-cell')]
        .map((c) => c.textContent?.trim())
        .filter((t): t is string => t !== undefined)
      expect(cells.some((t) => t !== '-' && /^\d{4}[\/.]/.test(t))).toBe(true)
    })
  })

  it('keeps the table empty when the API answers with a non-zero code', async () => {
    stubFetch({
      '/api/v1/deliveries': { code: 1, message: 'denied' },
      '/api/v1/deliveries/stats': { code: 0, data: { total: 0 } },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<DeliveriesPage />)
    // code != 0：行未写入，占位符渲染；统计卡照常（stats 是 code 0）。
    await waitFor(() => {
      expect(screen.queryByText('delivered')).not.toBeInTheDocument()
      expect(screen.queryByText('boom')).not.toBeInTheDocument()
    })
    expect(screen.getByText('总数')).toBeInTheDocument()
  })

  it('survives network failures', async () => {
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new TypeError('offline'))))
    render(<DeliveriesPage />)
    await vi.waitFor(() => expect(errSpy).toHaveBeenCalled())
    errSpy.mockRestore()
  })

  it('refetches with the status filter applied and cleared', async () => {
    const fetchMock = stubFetch({
      '/api/v1/deliveries': { code: 0, data: { deliveries: rows, total: 2 } },
      '/api/v1/deliveries/stats': { code: 0, data: stats },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<DeliveriesPage />)
    await screen.findByText('boom')
    const callsAfterMount = fetchMock.mock.calls.length

    // 列头与占位符都是「投递状态」，须限定 placeholder 节点。placeholder 自身
    // pointer-events: none，点击落在 selector 容器上；选中后占位符被移除，
    // 先抓外层根节点备用。
    const placeholder = screen.getByText('投递状态', { selector: '.ant-select-placeholder' })
    const selectNode = placeholder.closest('.ant-select')!
    await user.click(selectNode.querySelector('.ant-select-content')!)
    await user.click(await screen.findByText('已投递', { selector: '.ant-select-item-option-content' }))

    const calls = fetchMock.mock.calls.slice(callsAfterMount).filter(([u]) => String(u).startsWith('/api/v1/deliveries?'))
    expect(calls.length).toBeGreaterThan(0)
    expect(String(calls[calls.length - 1][0])).toContain('status=delivered')

    // allowClear 的清除按钮 hover 时才挂载
    await user.hover(selectNode)
    const clear = await waitFor(() => {
      const el = selectNode.querySelector('.ant-select-clear')
      expect(el).not.toBeNull()
      return el as HTMLElement
    })
    await user.click(clear)
    await vi.waitFor(() => {
      const recent = fetchMock.mock.calls.filter(([u]) => String(u).startsWith('/api/v1/deliveries?'))
      expect(String(recent[recent.length - 1][0])).not.toContain('status=')
    })
  })

  it('filters by provider and level', async () => {
    const fetchMock = stubFetch({
      '/api/v1/deliveries': { code: 0, data: { deliveries: rows, total: 2 } },
      '/api/v1/deliveries/stats': { code: 0, data: stats },
      '/api/v1/providers': { code: 0, data: { providers: [{ name: 'feishu' }, { name: 'smtp' }] } },
    })
    render(<DeliveriesPage />)
    await screen.findByText('boom')

    const pickInFilter = async (placeholderText: string, optionText: string) => {
      const ph = screen.getByText(placeholderText, { selector: '.ant-select-placeholder' })
      const node = ph.closest('.ant-select')!
      await user.click(node.querySelector('.ant-select-content')!)
      await user.click(await screen.findByText(optionText, { selector: '.ant-select-item-option-content' }))
      return node
    }

    await pickInFilter('Provider', 'feishu')
    await pickInFilter('级别', '信息')

    await vi.waitFor(() => {
      const urls = fetchMock.mock.calls.map((c) => String(c[0])).filter((u) => u.includes('/api/v1/deliveries?'))
      expect(urls[urls.length - 1]).toContain('provider=feishu')
      expect(urls[urls.length - 1]).toContain('level=info')
    })
  })

  it('paginates to page 2 with the right offset', async () => {
    const fetchMock = stubFetch({
      '/api/v1/deliveries': { code: 0, data: { deliveries: rows, total: 120 } },
      '/api/v1/deliveries/stats': { code: 0, data: { total: 120 } },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<DeliveriesPage />)
    await screen.findByText('boom')

    await user.click(screen.getByText('2'))
    await vi.waitFor(() => {
      const urls = fetchMock.mock.calls.map((c) => String(c[0])).filter((u) => u.includes('/api/v1/deliveries?'))
      expect(urls[urls.length - 1]).toContain('offset=50')
      expect(urls[urls.length - 1]).toContain('limit=50')
    })
  })

  // allowClear 清除到空：onChange 收到 undefined，必须回落成 '' 才能让
  // URLSearchParams 不带上 "provider=undefined"。
  it('clears the provider and level filters back to unset', async () => {
    const fetchMock = stubFetch({
      '/api/v1/deliveries': { code: 0, data: { deliveries: rows, total: 2 } },
      '/api/v1/deliveries/stats': { code: 0, data: stats },
      '/api/v1/providers': { code: 0, data: { providers: [{ name: 'feishu' }] } },
    })
    render(<DeliveriesPage />)
    await screen.findByText('boom')

    const pickInFilter = async (placeholderText: string, optionText: string) => {
      const ph = screen.getByText(placeholderText, { selector: '.ant-select-placeholder' })
      const node = ph.closest('.ant-select')!
      await user.click(node.querySelector('.ant-select-content')!)
      await user.click(await screen.findByText(optionText, { selector: '.ant-select-item-option-content' }))
      return node
    }

    const providerNode = await pickInFilter('Provider', 'feishu')
    await vi.waitFor(() => {
      expect(fetchMock.mock.calls.some((c) => String(c[0]).includes('provider=feishu'))).toBe(true)
    })

    const levelNode = await pickInFilter('级别', '严重')
    await vi.waitFor(() => {
      expect(fetchMock.mock.calls.some((c) => String(c[0]).includes('level=critical'))).toBe(true)
    })

    // allowClear 的清除按钮 hover 时才挂载
    await user.hover(providerNode)
    const clear = await waitFor(() => {
      const el = providerNode.querySelector('.ant-select-clear')
      expect(el).not.toBeNull()
      return el as HTMLElement
    })
    await user.click(clear)
    await vi.waitFor(() => {
      const urls = fetchMock.mock.calls.filter(([u]) => String(u).startsWith('/api/v1/deliveries?'))
      expect(String(urls[urls.length - 1][0])).not.toContain('provider=')
    })

    await user.hover(levelNode)
    const levelClear = await waitFor(() => {
      const el = levelNode.querySelector('.ant-select-clear')
      expect(el).not.toBeNull()
      return el as HTMLElement
    })
    await user.click(levelClear)
    await vi.waitFor(() => {
      const urls = fetchMock.mock.calls.filter(([u]) => String(u).startsWith('/api/v1/deliveries?'))
      expect(String(urls[urls.length - 1][0])).not.toContain('level=')
    })
  })

  it('refetches from the refresh button', async () => {
    const fetchMock = stubFetch({
      '/api/v1/deliveries': { code: 0, data: { deliveries: rows, total: 2 } },
      '/api/v1/deliveries/stats': { code: 0, data: stats },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<DeliveriesPage />)
    await screen.findByText('boom')
    const before = fetchMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: /刷\s*新/ }))
    await vi.waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(before))
  })

  it('sends the bearer token with every request', async () => {
    const fetchMock = stubFetch({
      '/api/v1/deliveries': { code: 0, data: { deliveries: [], total: 0 } },
      '/api/v1/deliveries/stats': { code: 0, data: { total: 0 } },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<DeliveriesPage />)
    await vi.waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(0))
    for (const call of fetchMock.mock.calls as unknown as [string, RequestInit][]) {
      expect((call[1].headers as Record<string, string>).Authorization).toBe('Bearer t')
    }
  })

  it('omits the authorization header entirely when there is no token', async () => {
    localStorage.removeItem('herald_token')
    const fetchMock = stubFetch({
      '/api/v1/deliveries': { code: 0, data: { deliveries: rows, total: 2 } },
      '/api/v1/deliveries/stats': { code: 0, data: stats },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<DeliveriesPage />)
    await screen.findByText('boom')
    expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(3)
    for (const call of fetchMock.mock.calls as unknown as [string, RequestInit][]) {
      expect(call[1].headers).toEqual({})
    }
  })

  it('falls back to an empty table when the payload omits deliveries and total', async () => {
    stubFetch({
      '/api/v1/deliveries': { code: 0, data: {} },
      '/api/v1/deliveries/stats': { code: 0, data: { total: 0 } },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<DeliveriesPage />)
    await waitFor(() => expect(screen.queryByText('boom')).not.toBeInTheDocument())
    expect(document.querySelector('.ant-table-placeholder')).not.toBeNull()
  })

  // data.data.providers 缺失时 || [] 兜底：不能让 setProviders 收到 undefined
  // （后续 .map 会崩）。
  it('falls back to an empty list when providers omits the data field', async () => {
    stubFetch({
      '/api/v1/deliveries': { code: 0, data: { deliveries: rows, total: 2 } },
      '/api/v1/deliveries/stats': { code: 0, data: { total: 1 } },
      '/api/v1/providers': { code: 0, data: {} },
    })
    render(<DeliveriesPage />)
    expect(screen.getByText('Provider', { selector: '.ant-select-placeholder' })).toBeInTheDocument()
  })

  // by_status 缺键时 || 0 兜底：卡片渲染 0 而不是 undefined。
  it('renders zero for missing status buckets', async () => {
    stubFetch({
      '/api/v1/deliveries': { code: 0, data: { deliveries: [], total: 0 } },
      '/api/v1/deliveries/stats': { code: 0, data: { total: 0 } },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<DeliveriesPage />)
    expect(await screen.findByText('总数')).toBeInTheDocument()
    // 五张卡的值全是 0
    expect(screen.getAllByText('0').length).toBe(5)
  })

  // stats / providers 的非零 code 分支：页面必须留在可用状态，不崩；
  // stats 为 null 时统计区整体不渲染。
  it('keeps working when stats and providers fail with a non-zero code', async () => {
    stubFetch({
      '/api/v1/deliveries': { code: 0, data: { deliveries: rows, total: 2 } },
      '/api/v1/deliveries/stats': { code: 1, message: 'stats unavailable' },
      '/api/v1/providers': { code: 1, message: 'providers unavailable' },
    })
    render(<DeliveriesPage />)
    expect(await screen.findByText('boom')).toBeInTheDocument()
    // 统计区没有渲染
    expect(screen.queryByText('总数')).not.toBeInTheDocument()
    // 级别筛选器仍在（providers 拿不到值时是空下拉，不是消失）
    expect(screen.getByText('级别', { selector: '.ant-select-placeholder' })).toBeInTheDocument()
  })

  // 10 秒轮询回调：自动刷新必须同时重拉列表与统计。抓 setInterval 注册的
  // 回调直接调用，不走 fake timer（user-event 与 fake timer 混用会把整页
  // 的 act 收口搅乱）。
  it('polls both the delivery list and the stats on its 10s interval', async () => {
    const fetchMock = stubFetch({
      '/api/v1/deliveries': { code: 0, data: { deliveries: rows, total: 2 } },
      '/api/v1/deliveries/stats': { code: 0, data: stats },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    const timers: Array<{ fn: () => void; ms: number | undefined }> = []
    const realSetInterval = window.setInterval
    vi.spyOn(window, 'setInterval').mockImplementation(((fn: () => void, ms?: number) => {
      timers.push({ fn, ms })
      return 0 as unknown as ReturnType<typeof realSetInterval>
    }) as typeof window.setInterval)

    render(<DeliveriesPage />)
    await screen.findByText('boom')

    const poll = timers.find((t) => t.ms === 10000)
    expect(poll).toBeDefined()
    const before = {
      list: fetchMock.mock.calls.filter(([u]) => String(u).startsWith('/api/v1/deliveries?')).length,
      stats: fetchMock.mock.calls.filter(([u]) => String(u) === '/api/v1/deliveries/stats').length,
    }
    await act(async () => { poll!.fn() })
    await vi.waitFor(() => {
      const list = fetchMock.mock.calls.filter(([u]) => String(u).startsWith('/api/v1/deliveries?')).length
      const st = fetchMock.mock.calls.filter(([u]) => String(u) === '/api/v1/deliveries/stats').length
      expect(list).toBeGreaterThan(before.list)
      expect(st).toBeGreaterThan(before.stats)
    })
  })
})
