import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'

const loginMock = vi.fn()
vi.mock('../api', () => ({ heraldApi: { login: (...a: unknown[]) => loginMock(...a) } }))

import LoginPage from './LoginPage'

function renderLogin() {
  return render(
    <MemoryRouter initialEntries={['/login']}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/" element={<div>home-after-login</div>} />
      </Routes>
    </MemoryRouter>
  )
}

const user = userEvent.setup()

async function submitForm(username: string, password: string) {
  await user.type(screen.getByPlaceholderText('用户名'), username)
  await user.type(screen.getByPlaceholderText('密码'), password)
  await user.click(screen.getByRole('button', { name: /登\s*录/ }))
}

describe('LoginPage', () => {
  beforeEach(() => {
    localStorage.clear()
  })
  afterEach(() => {
    cleanup()
  })

  it('renders the login card with the default-account hint', () => {
    renderLogin()
    expect(screen.getByText('Herald')).toBeInTheDocument()
    expect(screen.getByText('请登录以继续')).toBeInTheDocument()
    expect(screen.getAllByText('admin').length).toBe(2)
  })

  it('blocks submission when fields are empty', async () => {
    renderLogin()
    await user.click(screen.getByRole('button', { name: /登\s*录/ }))
    expect(await screen.findByText('请输入用户名')).toBeInTheDocument()
    expect(await screen.findByText('请输入密码')).toBeInTheDocument()
    expect(loginMock).not.toHaveBeenCalled()
  })

  it('stores the token and navigates home on success', async () => {
    loginMock.mockResolvedValue({ token: 'tok-1', user: { name: 'admin' } })
    renderLogin()
    await submitForm('admin', 'admin')
    expect(await screen.findByText('home-after-login')).toBeInTheDocument()
    expect(await screen.findByText('登录成功')).toBeInTheDocument()
    expect(localStorage.getItem('herald_token')).toBe('tok-1')
    // initialValues 里 remember 默认为 true，user 一并落盘。
    expect(JSON.parse(localStorage.getItem('herald_user')!)).toEqual({ name: 'admin' })
  })

  it('keeps a response without a token on the login page', async () => {
    loginMock.mockResolvedValue({ message: '用户名或密码错误' })
    renderLogin()
    await submitForm('admin', 'bad')
    expect(await screen.findByText('用户名或密码错误')).toBeInTheDocument()
    expect(localStorage.getItem('herald_token')).toBeNull()
  })

  it('shows a generic failure message for a tokenless empty response', async () => {
    loginMock.mockResolvedValue({})
    renderLogin()
    await submitForm('admin', 'bad')
    expect(await screen.findByText('登录失败')).toBeInTheDocument()
  })

  it('falls back to a network error hint when the request throws', async () => {
    loginMock.mockRejectedValue({ response: { data: { message: '账号被锁定' } } })
    renderLogin()
    await submitForm('admin', 'x')
    expect(await screen.findByText('账号被锁定')).toBeInTheDocument()

    loginMock.mockRejectedValue(new TypeError('network down'))
    await submitForm('admin', 'x')
    expect(await screen.findByText('网络错误，请稍后重试')).toBeInTheDocument()
  })
})
