import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { InternalAxiosRequestConfig, AxiosResponse } from 'axios'
import { api, heraldApi } from './index'

// 用自定义 adapter 截获请求：拦截器（注入 Authorization、解包 data）在真实
// axios 管线里执行，adapter 只负责按用例返回响应或抛错。
let seen: InternalAxiosRequestConfig | null = null

function useAdapter(handler: (config: InternalAxiosRequestConfig) => Promise<Partial<AxiosResponse>> | never) {
  api.defaults.adapter = (async (config: InternalAxiosRequestConfig) => {
    seen = config
    const out = await handler(config)
    return {
      data: out.data,
      status: out.status ?? 200,
      statusText: 'OK',
      headers: {},
      config,
    } as AxiosResponse
  }) as never
}

const ok = (data: any) => async () => ({ data })

describe('api', () => {
  beforeEach(() => {
    seen = null
    localStorage.clear()
  })

  it('is created with the fixed base URL and timeout', () => {
    expect(api.defaults.baseURL).toBe('/api/v1')
    expect(api.defaults.timeout).toBe(10000)
  })

  describe('request interceptor', () => {
    it('attaches the Bearer token when one is stored', async () => {
      localStorage.setItem('herald_token', 't-123')
      useAdapter(ok({ code: 0 }))
      await heraldApi.getStatus()
      expect((seen as InternalAxiosRequestConfig).headers.Authorization).toBe('Bearer t-123')
    })

    it('sends no Authorization header without a token', async () => {
      useAdapter(ok({ code: 0 }))
      await heraldApi.getStatus()
      expect((seen as InternalAxiosRequestConfig).headers.Authorization).toBeUndefined()
    })
  })

  describe('response interceptor', () => {
    it('unwraps res.data on success', async () => {
      useAdapter(ok({ code: 0, data: { status: 'running' } }))
      await expect(heraldApi.getStatus()).resolves.toEqual({ code: 0, data: { status: 'running' } })
    })

    it('logs and re-rejects on failure', async () => {
      const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
      api.defaults.adapter = (async () => {
        throw new Error('boom')
      }) as never
      await expect(heraldApi.getStatus()).rejects.toThrow('boom')
      expect(errSpy).toHaveBeenCalledWith('API Error:', expect.any(Error))
      errSpy.mockRestore()
    })
  })

  it('maps every endpoint onto the underlying instance', async () => {
    useAdapter(ok({ code: 0 }))

    await heraldApi.getStatus()
    expect(seen!.url).toBe('/status')
    expect(seen!.method).toBe('get')

    await heraldApi.getProviders()
    expect(seen!.url).toBe('/providers')

    await heraldApi.getWorkers()
    expect(seen!.url).toBe('/workers')

    await heraldApi.getQueue()
    expect(seen!.url).toBe('/queue')

    await heraldApi.getLogsStats()
    expect(seen!.url).toBe('/logs/stats')

    await heraldApi.getLogs({ page: 2 })
    expect(seen!.url).toBe('/logs')
    expect(seen!.params).toEqual({ page: 2 })

    await heraldApi.getProviderConfig('feishu')
    expect(seen!.url).toBe('/config/feishu')

    await heraldApi.enableProvider('feishu')
    expect(seen!.url).toBe('/providers/feishu/enable')
    expect(seen!.method).toBe('post')

    await heraldApi.disableProvider('feishu')
    expect(seen!.url).toBe('/providers/feishu/disable')

    await heraldApi.sendNotify({ title: 'hi' })
    expect(seen!.url).toBe('/notify')
    expect(seen!.data).toBe(JSON.stringify({ title: 'hi' }))

    await heraldApi.login({ username: 'a', password: 'b' })
    expect(seen!.url).toBe('/auth/login')
    expect(seen!.data).toBe(JSON.stringify({ username: 'a', password: 'b' }))

    await heraldApi.updateProviderConfig('feishu', { key: 'v' })
    expect(seen!.url).toBe('/config/feishu')
    expect(seen!.method).toBe('put')
    expect(seen!.data).toBe(JSON.stringify({ key: 'v' }))

    await heraldApi.getRules()
    expect(seen!.url).toBe('/rules')

    await heraldApi.getRule('p1')
    expect(seen!.url).toBe('/rules/p1')

    await heraldApi.createRule({ id: 'p1' })
    expect(seen!.url).toBe('/rules')
    expect(seen!.method).toBe('post')

    await heraldApi.updateRule('p1', { id: 'p1' })
    expect(seen!.url).toBe('/rules/p1')
    expect(seen!.method).toBe('put')

    await heraldApi.deleteRule('p1')
    expect(seen!.url).toBe('/rules/p1')
    expect(seen!.method).toBe('delete')

    await heraldApi.getGroups()
    expect(seen!.url).toBe('/groups')

    await heraldApi.getGroup('ops')
    expect(seen!.url).toBe('/groups/ops')

    await heraldApi.createGroup({ id: 'ops' })
    expect(seen!.url).toBe('/groups')
    expect(seen!.method).toBe('post')

    await heraldApi.updateGroup('ops', { id: 'ops' })
    expect(seen!.url).toBe('/groups/ops')
    expect(seen!.method).toBe('put')

    await heraldApi.deleteGroup('ops')
    expect(seen!.url).toBe('/groups/ops')
    expect(seen!.method).toBe('delete')
  })
})
