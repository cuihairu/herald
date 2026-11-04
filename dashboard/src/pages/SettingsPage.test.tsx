import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('../api', () => ({
  heraldApi: {
    changePassword: vi.fn(),
  },
}))

import SettingsPage from './SettingsPage'
import { heraldApi } from '../api'

const apiMocks = vi.mocked(heraldApi)
const user = userEvent.setup()

function fill(passwords: [string, string, string]) {
  // antd 的 Input.Password 渲染为 type=password 的 input，按 DOM 顺序稳定取用。
  const inputs = document.querySelectorAll('input[type="password"]')
  return user.type(inputs[0] as HTMLElement, passwords[0])
    .then(() => user.type(inputs[1] as HTMLElement, passwords[1]))
    .then(() => user.type(inputs[2] as HTMLElement, passwords[2]))
}

describe('SettingsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.changePassword.mockResolvedValue({ data: { code: 0 } })
  })

  it('changes the password with a matched confirmation', async () => {
    render(<SettingsPage />)
    await fill(['oldpw', 'newpw', 'newpw'])
    await user.click(screen.getByRole('button', { name: /修\s*改\s*密\s*码/ }))
    await screen.findByText('密码已修改')
    expect(apiMocks.changePassword).toHaveBeenCalledWith({
      old_password: 'oldpw',
      new_password: 'newpw',
    })
  })

  it('rejects a mismatched confirmation without calling the api', async () => {
    render(<SettingsPage />)
    await fill(['oldpw', 'newpw', 'other'])
    await user.click(screen.getByRole('button', { name: /修\s*改\s*密\s*码/ }))
    expect(await screen.findByText('两次输入的新密码不一致')).toBeInTheDocument()
    expect(apiMocks.changePassword).not.toHaveBeenCalled()
  })

  it('keeps the form when the required fields are missing', async () => {
    render(<SettingsPage />)
    await user.click(screen.getByRole('button', { name: /修\s*改\s*密\s*码/ }))
    expect(await screen.findByText('请输入旧密码')).toBeInTheDocument()
    expect(apiMocks.changePassword).not.toHaveBeenCalled()
  })

  it('reports an api failure', async () => {
    apiMocks.changePassword.mockRejectedValue({ response: { data: { message: 'invalid old password' } } })
    render(<SettingsPage />)
    await fill(['oldpw', 'newpw', 'newpw'])
    await user.click(screen.getByRole('button', { name: /修\s*改\s*密\s*码/ }))
    expect(await screen.findByText('invalid old password')).toBeInTheDocument()
  })
})
