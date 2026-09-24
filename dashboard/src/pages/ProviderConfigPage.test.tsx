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
})
