import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import LogsPage from './LogsPage'

// 数据形状刻意覆盖所有 render 分支：
// - status 'weird'（未知颜色 → default）
// - level 缺失（→ '-'）与 'critical'（已知色）
// - error 缺失（→ '-'）
// - duration null（→ '-'）
// - created_at 缺失（→ '-'）
const logRows = [
  {
    id: 'l1', provider: 'feishu', status: 'success', title: '正常一行',
    body: '', error: '', level: 'info', duration: 123,
    created_at: '2026-09-24T10:00:00Z',
  },
  {
    id: 'l2', provider: 'weird-provider', status: 'weird', title: '分支覆盖',
    body: '', error: 'boom', level: 'critical', duration: null,
    created_at: '',
  },
]

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

describe('LogsPage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  beforeEach(() => {
    localStorage.setItem('herald_token', 't')
    stubFetch({
      '/api/v1/logs': { code: 0, data: { logs: logRows, total: 2 } },
      '/api/v1/logs/stats': {
        code: 0,
        data: { total: 9, by_status: { success: 5, failed: 3, pending: 1 } },
      },
      '/api/v1/providers': {
        code: 0,
        data: { providers: [{ name: 'feishu' }, { name: 'log' }, { name: 'feishu' }] },
      },
    })
  })

  it('loads logs, stats and the provider filter list on mount', async () => {
    render(<LogsPage />)
    expect(await screen.findByText('正常一行')).toBeInTheDocument()
    expect(screen.getByText('分支覆盖')).toBeInTheDocument()
    // 统计卡
    expect(screen.getByText('总数')).toBeInTheDocument()
    expect(screen.getByText('进行中')).toBeInTheDocument()
  })

  it('renders every column fallback branch', async () => {
    render(<LogsPage />)
    await screen.findByText('分支覆盖')
    // status 'weird' 原样显示；level 缺失 → '-'；duration null → '-'；created_at 空 → '-'
    expect(screen.getByText('weird')).toBeInTheDocument()
    expect(screen.getByText('critical')).toBeInTheDocument()
    expect(screen.getByText('boom')).toBeInTheDocument()
    // 三个 '-'：l1 的空 error、l2 的 null duration、l2 的空 created_at
    const dashes = screen.getAllByText('-')
    expect(dashes.length).toBe(3)
  })

  it('keeps the table empty when the API answers with a non-zero code', async () => {
    const fetchMock = stubFetch({
      '/api/v1/logs': { code: 1, message: 'denied' },
      '/api/v1/logs/stats': { code: 0, data: { total: 0 } },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<LogsPage />)
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    // code != 0：logs 未写入，统计卡也不出现
    expect(screen.queryByText('正常一行')).not.toBeInTheDocument()
    expect(screen.queryByText('总数')).not.toBeInTheDocument()
  })

  it('survives network failures', async () => {
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('offline')))
    )
    render(<LogsPage />)
    await vi.waitFor(() => expect(errSpy).toHaveBeenCalled())
    errSpy.mockRestore()
  })

  it('refetches with the status filter applied and cleared', async () => {
    const fetchMock = stubFetch({
      '/api/v1/logs': { code: 0, data: { logs: logRows, total: 2 } },
      '/api/v1/logs/stats': { code: 0, data: { total: 0 } },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<LogsPage />)
    await screen.findByText('正常一行')
    const callsAfterMount = fetchMock.mock.calls.length

    // 打开「状态」下拉并选择「成功」。表格列头也叫「状态」，须限定 placeholder；
    // placeholder 自身 pointer-events: none，点击落在 selector 容器上。
    // 选中后 placeholder 节点会被移除，先抓取外层 select 根节点备用。
    const statusPlaceholder = screen.getByText('状态', { selector: '.ant-select-selection-placeholder' })
    const selectNode = statusPlaceholder.closest('.ant-select')!
    await user.click(selectNode.querySelector('.ant-select-selector')!)
    // 统计卡标题也有「成功」，须限定在下拉 option 内容里。
    await user.click(await screen.findByText('成功', { selector: '.ant-select-item-option-content' }))

    const logsWithFilter = fetchMock.mock.calls
      .slice(callsAfterMount)
      .filter(([u]) => String(u).startsWith('/api/v1/logs?'))
    expect(logsWithFilter.length).toBeGreaterThan(0)
    expect(String(logsWithFilter[logsWithFilter.length - 1][0])).toContain('status=success')

    // 清空过滤器（allowClear 的清除按钮 hover 时才挂载）→ status 参数消失
    await user.hover(selectNode)
    const clear = await waitFor(() => {
      const el = selectNode.querySelector('.ant-select-clear')
      expect(el).not.toBeNull()
      return el as HTMLElement
    })
    await user.click(clear)
    await vi.waitFor(() => {
      const recent = fetchMock.mock.calls.filter(([u]) => String(u).startsWith('/api/v1/logs?'))
      expect(String(recent[recent.length - 1][0])).not.toContain('status=')
    })
  })

  it('filters by provider and level', async () => {
    // beforeEach 的 providers 列表有重复名字；这里给下拉一个去重的列表
    const fetchMock = stubFetch({
      '/api/v1/logs': { code: 0, data: { logs: logRows, total: 2 } },
      '/api/v1/logs/stats': { code: 0, data: { total: 0 } },
      '/api/v1/providers': {
        code: 0,
        data: { providers: [{ name: 'feishu' }, { name: 'smtp' }] },
      },
    })
    render(<LogsPage />)
    await screen.findByText('正常一行')

    // Provider 下拉：placeholder 在选择后会被移除，先抓外层根节点
    const providerPlaceholder = screen.getByText('Provider', { selector: '.ant-select-selection-placeholder' })
    const providerSelect = providerPlaceholder.closest('.ant-select')!
    await user.click(providerSelect.querySelector('.ant-select-selector')!)
    await user.click(await screen.findByText('feishu', { selector: '.ant-select-item-option-content' }))

    // 级别下拉
    const levelPlaceholder = screen.getByText('级别', { selector: '.ant-select-selection-placeholder' })
    const levelSelect = levelPlaceholder.closest('.ant-select')!
    await user.click(levelSelect.querySelector('.ant-select-selector')!)
    await user.click(await screen.findByText('信息', { selector: '.ant-select-item-option-content' }))

    await vi.waitFor(() => {
      const logs = fetchMock.mock.calls
        .map(c => String(c[0]))
        .filter(u => u.includes('/api/v1/logs?'))
      expect(logs[logs.length - 1]).toContain('provider=feishu')
      expect(logs[logs.length - 1]).toContain('level=info')
    })
  })

  it('paginates to page 2 with the right offset', async () => {
    const fetchMock = stubFetch({
      '/api/v1/logs': { code: 0, data: { logs: logRows, total: 120 } },
      '/api/v1/logs/stats': { code: 0, data: { total: 120 } },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<LogsPage />)
    await screen.findByText('正常一行')

    await user.click(screen.getByText('2'))
    await vi.waitFor(() => {
      const logs = fetchMock.mock.calls
        .map(c => String(c[0]))
        .filter(u => u.includes('/api/v1/logs?'))
      expect(logs[logs.length - 1]).toContain('offset=50')
      expect(logs[logs.length - 1]).toContain('limit=50')
    })
  })

  it('refetches from the refresh button', async () => {
    const fetchMock = stubFetch({
      '/api/v1/logs': { code: 0, data: { logs: logRows, total: 2 } },
      '/api/v1/logs/stats': { code: 0, data: { total: 0 } },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<LogsPage />)
    await screen.findByText('正常一行')
    const before = fetchMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: /刷\s*新/ }))
    await vi.waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(before))
  })

  it('sends the bearer token with every request', async () => {
    const fetchMock = stubFetch({
      '/api/v1/logs': { code: 0, data: { logs: [], total: 0 } },
      '/api/v1/logs/stats': { code: 0, data: { total: 0 } },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<LogsPage />)
    await vi.waitFor(() => {
      expect(fetchMock.mock.calls.length).toBeGreaterThan(0)
    })
    for (const call of fetchMock.mock.calls as unknown as [string, RequestInit][]) {
      expect((call[1].headers as Record<string, string>).Authorization).toBe('Bearer t')
    }
  })

  // 未登录时三个 fetch 的 headers 走空对象支：不能因为没有 token 就
  // 发出一个带 "Bearer null" 的头，那会让后端 401 的原因更难查。
  it('omits the authorization header entirely when there is no token', async () => {
    localStorage.removeItem('herald_token')
    const fetchMock = stubFetch({
      '/api/v1/logs': { code: 0, data: { logs: logRows, total: 2 } },
      '/api/v1/logs/stats': { code: 0, data: { total: 9 } },
      '/api/v1/providers': { code: 0, data: { providers: ['feishu'] } },
    })
    render(<LogsPage />)
    await screen.findByText('正常一行')
    expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(3)
    for (const call of fetchMock.mock.calls as unknown as [string, RequestInit][]) {
      expect(call[1].headers).toEqual({})
    }
  })

  // data.data 缺字段时回落空值：logs/total 缺失不能让表格崩掉。
  it('falls back to an empty table when the payload omits logs and total', async () => {
    stubFetch({
      '/api/v1/logs': { code: 0, data: {} },
      '/api/v1/logs/stats': { code: 0, data: { total: 0 } },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<LogsPage />)
    // 表格没有数据行：既拿不到 fixture 的标题，也没有分页器。
    await vi.waitFor(() => {
      expect(screen.queryByText('正常一行')).not.toBeInTheDocument()
    })
    expect(screen.queryByText('分支覆盖')).not.toBeInTheDocument()
    expect(document.querySelector('.ant-table-placeholder')).not.toBeNull()
  })

  // 已知 level 才有专属颜色；未知 level 回落 default 而不是渲染空白标签。
  it('colors an unknown level with the default tag', async () => {
    stubFetch({
      '/api/v1/logs': {
        code: 0,
        data: {
          logs: [
            { id: 'lv', provider: 'feishu', status: 'success', title: '未知级别', level: 'nope', duration: 1, created_at: '2026-09-24T10:00:00Z' },
          ],
          total: 1,
        },
      },
      '/api/v1/logs/stats': { code: 0, data: { total: 1 } },
      '/api/v1/providers': { code: 0, data: { providers: [] } },
    })
    render(<LogsPage />)
    expect(await screen.findByText('未知级别')).toBeInTheDocument()
    // 未知 level 走 antd 的 default 色类，而不是专属色。
    expect(screen.getByText('nope').className).toContain('ant-tag-default')
  })

  // allowClear 清除到空：onChange 收到 undefined，必须回落成 '' 才能让
  // URLSearchParams 不带上 "provider=undefined"。
  it('clears the provider and level filters back to unset', async () => {
    const fetchMock = stubFetch({
      '/api/v1/logs': { code: 0, data: { logs: logRows, total: 2 } },
      '/api/v1/logs/stats': { code: 0, data: { total: 9 } },
      // 筛选器吃的是 provider 对象数组（取 .name 并去重），不是字符串。
      '/api/v1/providers': { code: 0, data: { providers: [{ name: 'feishu' }, { name: 'wecom' }] } },
    })
    render(<LogsPage />)
    await screen.findByText('正常一行')

    // 打开「Provider」下拉并选一个值。placeholder 自身 pointer-events: none，
    // 点击要落在 selector 容器上（与状态筛选那条用例同一手法）。
    const pickInFilter = async (placeholderText: string, optionText: string) => {
      const ph = screen.getByText(placeholderText, { selector: '.ant-select-selection-placeholder' })
      const node = ph.closest('.ant-select')!
      await user.click(node.querySelector('.ant-select-selector')!)
      await user.click(await screen.findByText(optionText, { selector: '.ant-select-item-option-content' }))
      return node
    }

    const providerNode = await pickInFilter('Provider', 'feishu')
    await vi.waitFor(() => {
      expect(fetchMock.mock.calls.some((c) => String(c[0]).includes('provider=feishu'))).toBe(true)
    })

    await pickInFilter('级别', '严重')
    await vi.waitFor(() => {
      expect(fetchMock.mock.calls.some((c) => String(c[0]).includes('level=critical'))).toBe(true)
    })

    // allowClear 的清除按钮 hover 时才挂载。
    await user.hover(providerNode)
    const clear = await waitFor(() => {
      const el = providerNode.querySelector('.ant-select-clear')
      expect(el).not.toBeNull()
      return el as Element
    })
    await user.click(clear)
    await vi.waitFor(() => {
      const logsCalls = fetchMock.mock.calls.filter(([u]) => String(u).startsWith('/api/v1/logs?'))
      const last = String(logsCalls[logsCalls.length - 1][0])
      expect(last).not.toContain('provider=')
    })
  })

  // stats / providers 的非零 code 分支：页面必须留在可用状态，不崩。
  it('keeps working when stats and providers fail with a non-zero code', async () => {
    stubFetch({
      '/api/v1/logs': { code: 0, data: { logs: logRows, total: 2 } },
      '/api/v1/logs/stats': { code: 1, message: 'stats unavailable' },
      '/api/v1/providers': { code: 1, message: 'providers unavailable' },
    })
    render(<LogsPage />)
    // 日志照常渲染，统计区与筛选器只是留空。
    expect(await screen.findByText('正常一行')).toBeInTheDocument()
    // 级别筛选器仍然在（providers 拿不到值时是空下拉，不是消失）。
    expect(screen.getByText('级别', { selector: '.ant-select-selection-placeholder' })).toBeInTheDocument()
  })
})
