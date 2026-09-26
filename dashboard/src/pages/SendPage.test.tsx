import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('../api', () => ({
  heraldApi: {
    sendNotify: vi.fn(),
    getProviders: vi.fn().mockResolvedValue({ data: { providers: [] } }),
  },
}))

import SendPage from './SendPage'
import { heraldApi } from '../api'
import { useHeraldStore } from '../stores/herald'
import { resetStore } from '../test/store'

const apiMocks = vi.mocked(heraldApi)

const user = userEvent.setup()

describe('SendPage', () => {
  beforeEach(() => {
    resetStore()
    vi.clearAllMocks()
    apiMocks.getProviders.mockResolvedValue({
      data: { providers: [{ name: 'feishu' }, { name: 'telegram' }] },
    })
  })

  it('pulls the provider list when the store is empty and renders the form', async () => {
    render(<SendPage />)
    // Select 关闭时 Option 不进 DOM，改用 fetch 调用断言拉取行为
    await waitFor(() => expect(apiMocks.getProviders).toHaveBeenCalled())
    expect(screen.getByPlaceholderText('输入标题')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('输入内容')).toBeInTheDocument()
    expect(screen.getByText('Channels')).toBeInTheDocument()
  })

  it('sends the notification and clears the form on success', async () => {
    apiMocks.sendNotify.mockResolvedValue({ data: { code: 0 } })
    useHeraldStore.setState({ providers: [{ name: 'feishu' }] })
    render(<SendPage />)

    await user.type(screen.getByPlaceholderText('输入标题'), 'hello')
    await user.type(screen.getByPlaceholderText('输入内容'), 'world')
    await user.click(screen.getByRole('button', { name: /发\s*送/ }))

    expect(await screen.findByText('发送成功')).toBeInTheDocument()
    expect(apiMocks.sendNotify).toHaveBeenCalledWith(
      expect.objectContaining({ title: 'hello', body: 'world' })
    )
    // resetFields：输入框回到空
    await waitFor(() => {
      expect(screen.getByPlaceholderText('输入标题')).toHaveValue('')
    })
  })

  it('shows the failure banner when the send is rejected', async () => {
    apiMocks.sendNotify.mockRejectedValue({ message: '渠道全部失败' })
    useHeraldStore.setState({ providers: [{ name: 'feishu' }] })
    render(<SendPage />)

    await user.type(screen.getByPlaceholderText('输入标题'), 'oops')
    await user.click(screen.getByRole('button', { name: /发\s*送/ }))

    expect(await screen.findByText('渠道全部失败')).toBeInTheDocument()
    // 失败横幅也渲染在表单内
    expect(screen.getAllByText('渠道全部失败').length).toBeGreaterThanOrEqual(1)
  })

  it('blocks submission when the title is missing', async () => {
    useHeraldStore.setState({ providers: [{ name: 'feishu' }] })
    render(<SendPage />)
    await user.click(screen.getByRole('button', { name: /发\s*送/ }))
    expect(await screen.findByText('请输入标题')).toBeInTheDocument()
    expect(apiMocks.sendNotify).not.toHaveBeenCalled()
  })

  // err.message 为空串时，|| 兜底文案「发送失败」。
  it('shows the fallback wording when the error has no message', async () => {
    apiMocks.sendNotify.mockRejectedValue(new TypeError(''))
    useHeraldStore.setState({ providers: [{ name: 'feishu' }] })
    render(<SendPage />)

    await user.type(screen.getByPlaceholderText('输入标题'), 'oops')
    await user.type(screen.getByPlaceholderText('输入内容'), 'body')
    await user.click(screen.getByRole('button', { name: /发\s*送/ }))

    expect(await screen.findByText('发送失败')).toBeInTheDocument()
    expect(screen.getByText('发送失败')).toBeInTheDocument()
  })
})
