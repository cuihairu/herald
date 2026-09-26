import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import ProviderConfigPage from './ProviderConfigPage'

function jsonResponse(body: unknown) {
  return { ok: true, json: async () => body }
}

function stubFetch(routes: Record<string, unknown>) {
  const fetchMock = vi.fn((url: string, init?: RequestInit) => {
    const path = url.split('?')[0]
    const body = routes[path] ?? { code: 1, message: 'no route stubbed' }
    return Promise.resolve(jsonResponse(body))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const user = userEvent.setup()

function renderPage() {
  render(
    <MemoryRouter initialEntries={['/providers/feishu/config']}>
      <Routes>
        <Route path="/providers/:name/config" element={<ProviderConfigPage />} />
      </Routes>
    </MemoryRouter>
  )
}

describe('ProviderConfigPage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  beforeEach(() => {
    localStorage.setItem('herald_token', 't')
  })

  it('loads the config, serializes object fields and renders typed inputs', async () => {
    const fetchMock = stubFetch({
      '/api/v1/config/feishu': {
        code: 0,
        data: {
          type: 'builtin',
          enabled: true,
          schema: { webhook: 'string', 'token-type': 'string', retries: 'number', headers: 'object' },
          config: {
            webhook: 'https://example.com',
            'token-type': 'secret',
            retries: 3,
            headers: { 'X-Extra': '1' }, // object 字段回显时序列化成 JSON 文本
          },
        },
      },
    })
    renderPage()

    // fetch 带 token
    expect((fetchMock.mock.calls[0][1] as RequestInit).headers).toEqual({
      Authorization: 'Bearer t',
    })
    expect(await screen.findByText('配置项')).toBeInTheDocument()
    expect(screen.getByText('builtin')).toBeInTheDocument()
    // 三种字段类型各渲染一个输入控件
    expect(screen.getByPlaceholderText('输入 webhook')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('输入 retries')).toBeInTheDocument()
    const headersArea = screen.getByPlaceholderText(/输入 headers/) as HTMLTextAreaElement
    expect(headersArea.value).toBe(JSON.stringify({ 'X-Extra': '1' }, null, 2))
  })

  it('shows the empty-schema notice when nothing is configurable', async () => {
    stubFetch({
      '/api/v1/config/feishu': { code: 0, data: { type: 'builtin', enabled: true, schema: {}, config: {} } },
    })
    renderPage()
    expect(await screen.findByText('此 Provider 暂无可配置项')).toBeInTheDocument()
  })

  it('reports the provider as missing when the API rejects', async () => {
    stubFetch({
      '/api/v1/config/feishu': { code: 1, message: 'provider not found' },
    })
    renderPage()
    // code != 0 → config 保持 null → 加载结束后显示错误 Alert
    expect(await screen.findByText('Provider 不存在')).toBeInTheDocument()
  })

  it('survives a network failure on load', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('offline')))
    )
    renderPage()
    expect(await screen.findByText('Provider 不存在')).toBeInTheDocument()
  })

  it('parses object fields back into JSON on save', async () => {
    stubFetch({
      '/api/v1/config/feishu': {
        code: 0,
        data: {
          type: 'builtin',
          enabled: true,
          schema: { webhook: 'string', headers: 'object' },
          config: { webhook: '', headers: {} },
        },
      },
    })
    renderPage()
    await screen.findByText('配置项')

    const headersArea = screen.getByPlaceholderText(/输入 headers/) as HTMLTextAreaElement
    // user.type 会把 '{' 当键盘描述符解析（如 {Enter}），JSON 文本必须走 fireEvent
    fireEvent.change(headersArea, { target: { value: '{"a": 1}' } })
    await user.click(screen.getByRole('button', { name: /保\s*存\s*配\s*置/ }))

    await vi.waitFor(async () => {
      expect(screen.getByText('配置保存成功，Provider 已重启')).toBeInTheDocument()
    })
    // 校验 PUT body：object 字段已从文本解析回对象
    const globalFetch = vi.mocked(globalThis.fetch)
    const putCall = globalFetch.mock.calls.find(([u, init]) => String(u) === '/api/v1/config/feishu' && (init as RequestInit).method === 'PUT')!
    expect(JSON.parse((putCall[1] as RequestInit).body as string)).toEqual({
      config: { webhook: '', headers: { a: 1 } },
    })
  })

  it('refuses to save an invalid JSON object field', async () => {
    stubFetch({
      '/api/v1/config/feishu': {
        code: 0,
        data: {
          type: 'builtin',
          enabled: true,
          schema: { headers: 'object' },
          config: { headers: {} },
        },
      },
    })
    renderPage()
    await screen.findByText('配置项')

    const headersArea = screen.getByPlaceholderText(/输入 headers/) as HTMLTextAreaElement
    fireEvent.change(headersArea, { target: { value: '{broken' } })
    await user.click(screen.getByRole('button', { name: /保\s*存\s*配\s*置/ }))

    expect(await screen.findByText(/配置项 headers 不是合法的 JSON 对象/)).toBeInTheDocument()
    // 未发出任何 PUT
    expect(
      vi.mocked(globalThis.fetch).mock.calls.filter(([, init]) => (init as RequestInit)?.method === 'PUT').length
    ).toBe(0)
  })

  it('surfaces the API error message when the save is rejected', async () => {
    stubFetch({
      '/api/v1/config/feishu': {
        code: 0,
        data: { type: 'builtin', enabled: true, schema: { webhook: 'string' }, config: { webhook: '' } },
      },
    })
    renderPage()
    await screen.findByText('配置项')
    // 保存前的 stub 在点击时被替换为失败响应
    vi.mocked(globalThis.fetch).mockImplementation(
      (() => Promise.resolve(jsonResponse({ code: 1, message: '配置校验失败' }))) as never
    )
    await user.click(screen.getByRole('button', { name: /保\s*存\s*配\s*置/ }))
    expect(await screen.findByText('配置校验失败')).toBeInTheDocument()
  })

  it('sends the test notification through the test button', async () => {
    stubFetch({
      '/api/v1/config/feishu': {
        code: 0,
        data: { type: 'builtin', enabled: true, schema: { webhook: 'string' }, config: { webhook: '' } },
      },
      '/api/v1/notify': { code: 0, data: {} },
    })
    renderPage()
    await screen.findByText('配置项')

    await user.click(screen.getByRole('button', { name: /测\s*试\s*连\s*接/ }))
    expect(await screen.findByText('测试消息已发送，请检查是否收到', {}, { timeout: 5000 })).toBeInTheDocument()

    const post = vi.mocked(globalThis.fetch).mock.calls.find(
      ([u, init]) => String(u) === '/api/v1/notify' && (init as RequestInit).method === 'POST'
    )!
    const body = JSON.parse((post[1] as RequestInit).body as string)
    expect(body).toMatchObject({ title: 'Herald 测试消息', level: 'info', channels: ['feishu'] })
  })

  it('reports test failures and network errors', async () => {
    // 「测试连接」按钮在表单区（空 schema 只渲染 Alert），stub 必须给非空 schema
    stubFetch({
      '/api/v1/config/feishu': {
        code: 0,
        data: { type: 'builtin', enabled: true, schema: { webhook: 'string' }, config: { webhook: '' } },
      },
      '/api/v1/notify': { code: 1, message: '测试失败' },
    })
    renderPage()
    await screen.findByText('配置项')

    await user.click(screen.getByRole('button', { name: /测\s*试\s*连\s*接/ }))
    expect(await screen.findByText('测试失败', {}, { timeout: 5000 })).toBeInTheDocument()

    vi.mocked(globalThis.fetch).mockImplementation(
      (() => Promise.reject(new TypeError('offline'))) as never
    )
    await user.click(screen.getByRole('button', { name: /测\s*试\s*连\s*接/ }))
    expect(await screen.findByText('offline', {}, { timeout: 5000 })).toBeInTheDocument()
  })

  it('reports a save failure when the network dies', async () => {
    stubFetch({
      '/api/v1/config/feishu': {
        code: 0,
        data: { type: 'builtin', enabled: true, schema: { webhook: 'string' }, config: { webhook: '' } },
      },
    })
    renderPage()
    await screen.findByText('配置项')
    vi.mocked(globalThis.fetch).mockImplementation(
      (() => Promise.reject(new TypeError('offline'))) as never
    )
    await user.click(screen.getByRole('button', { name: /保\s*存\s*配\s*置/ }))
    expect(await screen.findByText('offline', {}, { timeout: 5000 })).toBeInTheDocument()
  })

  it('edits string and number fields through their onChange handlers', async () => {
    stubFetch({
      '/api/v1/config/feishu': {
        code: 0,
        data: {
          type: 'builtin',
          enabled: true,
          schema: { webhook: 'string', retries: 'number' },
          config: { webhook: '', retries: 3 },
        },
      },
    })
    renderPage()
    await screen.findByText('配置项')

    await user.type(screen.getByPlaceholderText('输入 webhook'), 'https://example.com')
    // InputNumber 用 fireEvent 直改值（user.type 对数字步进组件的按键序列不可控）
    fireEvent.change(screen.getByPlaceholderText('输入 retries'), { target: { value: '5' } })
    await user.click(screen.getByRole('button', { name: /保\s*存\s*配\s*置/ }))

    expect(await screen.findByText('配置保存成功，Provider 已重启', {}, { timeout: 5000 })).toBeInTheDocument()
    const putCall = vi.mocked(globalThis.fetch).mock.calls.find(
      ([u, init]) => String(u) === '/api/v1/config/feishu' && (init as RequestInit).method === 'PUT'
    )!
    expect(JSON.parse((putCall[1] as RequestInit).body as string)).toEqual({
      config: { webhook: 'https://example.com', retries: 5 },
    })
  })

  it('the back button returns to the previous route', async () => {
    stubFetch({
      '/api/v1/config/feishu': {
        code: 0,
        data: { type: 'builtin', enabled: true, schema: {}, config: {} },
      },
    })
    const { container } = render(
      <MemoryRouter initialEntries={['/providers', '/providers/feishu/config']} initialIndex={1}>
        <Routes>
          <Route path="/providers" element={<div>providers-list</div>} />
          <Route path="/providers/:name/config" element={<ProviderConfigPage />} />
        </Routes>
      </MemoryRouter>
    )
    await screen.findByText('配置项')
    // 头部的 icon-only 返回按钮
    const back = container.querySelector('button.ant-btn-icon-only') as HTMLButtonElement
    expect(back).toBeInTheDocument()
    await user.click(back)
    expect(await screen.findByText('providers-list')).toBeInTheDocument()
  })

  // 页面里三处 fetch 共用一个 token 分支：未登录时 headers 必须
  // 是空对象（不能是 { Authorization: 'Bearer null' }）。
  it('omits the authorization header when there is no token', async () => {
    localStorage.removeItem('herald_token')
    const fetchMock = stubFetch({
      '/api/v1/config/feishu': {
        code: 0,
        data: { type: 'builtin', enabled: true, schema: { url: 'string' }, config: { url: '' } },
      },
      '/api/v1/notify': { code: 0, message: 'ok' },
    })
    renderPage()
    await screen.findByText('配置项')
    await vi.waitFor(async () => {
      for (const call of vi.mocked(globalThis.fetch).mock.calls as unknown as [string, RequestInit][]) {
        expect(call[1].headers).toEqual({})
      }
    })
  })

  // schema 为空时走 alert 分支：渲染"暂无可配置项"，不渲染配置卡片。
  it('shows the empty-schema notice when the API omits the schema field', async () => {
    stubFetch({
      '/api/v1/config/feishu': {
        code: 0,
        data: { type: 'builtin', enabled: false, schema: {}, config: {} },
      },
    })
    renderPage()
    // 卡片里的 Tag 在 schema 为空时不渲染，所以一个 .ant-tag 都没有。
    expect(document.querySelectorAll('.ant-tag')).toHaveLength(0)
    expect(await screen.findByText('此 Provider 暂无可配置项')).toBeInTheDocument()
  })

  // 已禁用的 Provider 渲染「已禁用」灰徽标（需非空 schema，
  // 否则卡片不渲染，Tag 也就无从谈起）。
  it('shows the disabled tag for an enabled: false provider', async () => {
    stubFetch({
      '/api/v1/config/feishu': {
        code: 0,
        data: { type: 'builtin', enabled: false, schema: { url: 'string' }, config: { url: '' } },
      },
    })
    renderPage()
    await screen.findByText('配置项')
    const tags = document.querySelectorAll('.ant-tag')
    expect(tags).toHaveLength(2)
    expect(tags[1].className).toContain('ant-tag-default')
  })

  // 保存返回非零 code：走 else 支，弹出 data.message。
  // 配置必须先加载成功（否则直接停在 Alert 上），且 schema 非空，
  // 之后再让 PUT 报 422。
  it('surfaces the API message when a save is rejected', async () => {
    let turn = 0
    const fetchMock = vi.fn((url: string) => {
      const path = url.split('?')[0]
      const first = path === '/api/v1/config/feishu' && turn++ === 0
      return Promise.resolve(jsonResponse(
        first ? { code: 0, data: { type: 'builtin', enabled: true, schema: { url: 'string' }, config: { url: '' } } }
          : path === '/api/v1/config/feishu'
            ? { code: 422, message: '字段 schema 与后端约束不符' }
            : { code: 0, message: 'ok' }
      ))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    // 配置加载成功：卡片已渲染。
    await screen.findByText('配置项')
    await user.click(screen.getByRole('button', { name: /保\s*存\s*配\s*置/ }))
    expect(await screen.findByText('字段 schema 与后端约束不符')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /保\s*存\s*配\s*置/ })).toBeInTheDocument()
  })

  // 保存时的网络异常：catch 支，err.message 或兜底文案。
  it('labels a save network failure with the fallback wording', async () => {
    let loaded = false
    const fetchMock = vi.fn((url: string) => {
      const path = url.split('?')[0]
      if (path === '/api/v1/config/feishu' && !loaded) { loaded = true; return Promise.resolve(jsonResponse({ code: 0, data: { type: 'builtin', enabled: true, schema: { url: 'string' }, config: { url: '' } } })) }
      return Promise.reject(new TypeError('ECONNRESET'))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await screen.findByText('配置项')
    await user.click(screen.getByRole('button', { name: /保\s*存\s*配\s*置/ }))
    expect(await screen.findByText('ECONNRESET')).toBeInTheDocument()
  })

  // 测试通知返回非零 code：走 else 支，弹出 data.message。
  it('surfaces the test-notification API message', async () => {
    let loaded = false
    const fetchMock = vi.fn((url: string) => {
      const path = url.split('?')[0]
      if (path === '/api/v1/config/feishu' && !loaded) { loaded = true; return Promise.resolve(jsonResponse({ code: 0, data: { type: 'builtin', enabled: true, schema: { url: 'string' }, config: { url: '' } } })) }
      return Promise.resolve(jsonResponse(
        path === '/api/v1/notify' ? { code: 500, message: '通知渠道未就绪' } : { code: 0, message: 'ok' }
      ))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await screen.findByText('配置项')
    await user.click(screen.getByRole('button', { name: /测\s*试\s*连\s*接/ }))
    expect(await screen.findByText('通知渠道未就绪')).toBeInTheDocument()
  })

  // data.message 为空串时，|| 兜底文案「保存失败」。
  it('shows the save-failure fallback when the API returns an empty message', async () => {
    let loaded = false
    const fetchMock = vi.fn((url: string) => {
      const path = url.split('?')[0]
      if (path === '/api/v1/config/feishu' && !loaded) { loaded = true; return Promise.resolve(jsonResponse({ code: 0, data: { type: 'builtin', enabled: true, schema: { url: 'string' }, config: { url: '' } } })) }
      return Promise.resolve(jsonResponse(path === '/api/v1/config/feishu' ? { code: 422, message: '' } : { code: 0, message: 'ok' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await screen.findByText('配置项')
    await user.click(screen.getByRole('button', { name: /保\s*存\s*配\s*置/ }))
    // message 为空串，|| 走到右边 '保存失败'。
    expect(await screen.findByText('保存失败')).toBeInTheDocument()
  })

  // err.message 为空串时，|| 兜底文案「保存失败」。
  it('shows the save-failure fallback when the error has no message', async () => {
    let loaded = false
    const fetchMock = vi.fn((url: string) => {
      const path = url.split('?')[0]
      if (path === '/api/v1/config/feishu' && !loaded) { loaded = true; return Promise.resolve(jsonResponse({ code: 0, data: { type: 'builtin', enabled: true, schema: { url: 'string' }, config: { url: '' } } })) }
      return Promise.reject(new TypeError(''))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await screen.findByText('配置项')
    await user.click(screen.getByRole('button', { name: /保\s*存\s*配\s*置/ }))
    // TypeError('') 的 message 是空串，|| 走到右边 '保存失败'。
    expect(await screen.findByText('保存失败')).toBeInTheDocument()
  })

  // data.message 为空串时，|| 兜底文案「测试失败」。
  it('shows the test-failure fallback when the notification returns an empty message', async () => {
    let loaded = false
    const fetchMock = vi.fn((url: string) => {
      const path = url.split('?')[0]
      if (path === '/api/v1/config/feishu' && !loaded) { loaded = true; return Promise.resolve(jsonResponse({ code: 0, data: { type: 'builtin', enabled: true, schema: { url: 'string' }, config: { url: '' } } })) }
      return Promise.resolve(jsonResponse(path === '/api/v1/notify' ? { code: 500, message: '' } : { code: 0, message: 'ok' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await screen.findByText('配置项')
    await user.click(screen.getByRole('button', { name: /测\s*试\s*连\s*接/ }))
    expect(await screen.findByText('测试失败')).toBeInTheDocument()
  })

  // err.message 为空串时，|| 兜底文案「测试失败」。
  it('shows the test-failure fallback when the notification error has no message', async () => {
    let loaded = false
    const fetchMock = vi.fn((url: string) => {
      const path = url.split('?')[0]
      if (path === '/api/v1/config/feishu' && !loaded) { loaded = true; return Promise.resolve(jsonResponse({ code: 0, data: { type: 'builtin', enabled: true, schema: { url: 'string' }, config: { url: '' } } })) }
      return Promise.reject(new TypeError(''))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()
    await screen.findByText('配置项')
    await user.click(screen.getByRole('button', { name: /测\s*试\s*连\s*接/ }))
    expect(await screen.findByText('测试失败')).toBeInTheDocument()
  })
})
