import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('../api', () => ({
  heraldApi: {
    getKeys: vi.fn(),
    createKey: vi.fn(),
    revokeKey: vi.fn(),
  },
}))

import ApiKeysPage, { copyText, errMsg } from './ApiKeysPage'
import { heraldApi } from '../api'

const apiMocks = vi.mocked(heraldApi)
const user = userEvent.setup()

// navigator.clipboard 是 getter-only，测试里用 defineProperty 换实现。
function stubClipboard(write: () => Promise<void>) {
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: vi.fn(write) },
    configurable: true,
  })
}

const keys = [
  { id: 'k1', name: 'CI 发布机', prefix: 'wm_abcdef12', created_at: '2026-11-01T10:00:00Z', revoked: false },
  { id: 'k2', name: '旧机器', prefix: 'wm_ghijkl34', created_at: '2026-10-01T10:00:00Z', revoked: true },
]

async function openCreate() {
  await user.click(await screen.findByRole('button', { name: /生\s*成\s*新\s*Key/ }))
  return await screen.findByRole('dialog')
}

describe('errMsg', () => {
  it('prefers the api message, then err.message, then fallback', () => {
    expect(errMsg({ response: { data: { message: 'boom' } } }, 'fb')).toBe('boom')
    expect(errMsg({ message: 'net down' }, 'fb')).toBe('net down')
    expect(errMsg(undefined, 'fb')).toBe('fb')
  })
})

describe('copyText', () => {
  it('reports success and failure', async () => {
    stubClipboard(() => Promise.resolve())
    await expect(copyText('x')).resolves.toBe(true)
    stubClipboard(() => Promise.reject(new Error('denied')))
    await expect(copyText('x')).resolves.toBe(false)
  })
})

describe('ApiKeysPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stubClipboard(() => Promise.resolve())
    apiMocks.getKeys.mockResolvedValue({ data: { keys, count: keys.length } })
  })

  it('lists masked keys with their status', async () => {
    render(<ApiKeysPage />)
    expect(await screen.findByText('CI 发布机')).toBeInTheDocument()
    expect(screen.getByText('已吊销')).toBeInTheDocument()
    expect(screen.getByText('活跃')).toBeInTheDocument()
    // 已吊销的行不出现吊销按钮。
    expect(screen.getAllByRole('button', { name: /吊\s*销/ })).toHaveLength(1)
  })

  it('reports a load failure', async () => {
    apiMocks.getKeys.mockRejectedValue({ response: { data: { message: 'store down' } } })
    render(<ApiKeysPage />)
    expect(await screen.findByText('store down')).toBeInTheDocument()
  })

  it('mints a key and shows the plain value exactly once', async () => {
    apiMocks.createKey.mockResolvedValue({
      data: { key: 'wm_newkey123', info: { id: 'k3', name: '新机器', prefix: 'wm_newkey12', revoked: false } },
    })
    render(<ApiKeysPage />)
    await openCreate()
    await user.type(screen.getByPlaceholderText('CI 发布机'), '新机器')
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: /生\s*成/ }))
    // 弹窗展示完整 key。
    expect(await screen.findByDisplayValue('wm_newkey123')).toBeInTheDocument()
    await waitFor(() => expect(apiMocks.getKeys).toHaveBeenCalledTimes(2))

    // 复制成功提示。
    await user.click(screen.getByRole('button', { name: /复\s*制/ }))
    expect(await screen.findByText('已复制到剪贴板')).toBeInTheDocument()

    // 关闭后明文不再可见（antd 关闭动画保留隐藏的 DOM）。
    // 创建弹窗的退场动画会与新弹窗短暂同存，点击要以明文所在弹窗为作用域。
    const keyDialog = screen.getByDisplayValue('wm_newkey123').closest('[role="dialog"]') as HTMLElement
    await user.click(within(keyDialog).getByRole('button', { name: /保\s*存/ }))
    // 关闭已发起即可：jsdom 不实现带伪元素的 getComputedStyle，rc-motion 的
    // zoom 退场链读不到时长、永不完成，「不可见」在测试环境里观测不到。
    await waitFor(() => {
      expect(keyDialog.className).toMatch(/-leave/)
    })
  })

  it('keeps the create modal open on validation and api failures', async () => {
    render(<ApiKeysPage />)
    await openCreate()
    // 空备注：客户端校验失败，弹窗不关。
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: /生\s*成/ }))
    expect(await screen.findByText('请输入备注')).toBeInTheDocument()
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    // 后端拒绝：弹窗也不关，错误提示可见。
    apiMocks.createKey.mockRejectedValue({ response: { data: { message: 'store down' } } })
    await user.type(screen.getByPlaceholderText('CI 发布机'), 'x')
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: /生\s*成/ }))
    expect(await screen.findByText('store down')).toBeInTheDocument()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('revokes an active key after confirm', async () => {
    apiMocks.revokeKey.mockResolvedValue({ data: { code: 0 } })
    render(<ApiKeysPage />)
    await user.click(await screen.findByRole('button', { name: /吊\s*销/ }))
    await user.click(await screen.findByRole('button', { name: /确\s*认\s*吊\s*销/ }))
    await waitFor(() => expect(apiMocks.revokeKey).toHaveBeenCalledWith('k1'))
    await waitFor(() => expect(apiMocks.getKeys).toHaveBeenCalledTimes(2))
    // 提示与状态标签都出现「已吊销」。
    await waitFor(() => expect(screen.getAllByText('已吊销').length).toBeGreaterThan(1))
  })

  it('reports a revoke failure', async () => {
    apiMocks.revokeKey.mockRejectedValue(new Error('network down'))
    render(<ApiKeysPage />)
    await user.click(await screen.findByRole('button', { name: /吊\s*销/ }))
    await user.click(await screen.findByRole('button', { name: /确\s*认\s*吊\s*销/ }))
    expect(await screen.findByText('network down')).toBeInTheDocument()
  })
})
