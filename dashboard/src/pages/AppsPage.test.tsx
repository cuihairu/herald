import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('../api', () => ({
  heraldApi: {
    getApps: vi.fn(),
  },
}))

import AppsPage from './AppsPage'
import { heraldApi } from '../api'

const apiMocks = vi.mocked(heraldApi)
const user = userEvent.setup()

describe('AppsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.getApps.mockResolvedValue({ data: { apps: ['alpha-app', 'zeta-app'], count: 2 } })
  })

  it('lists the configured namespaces', async () => {
    render(<AppsPage />)
    expect(await screen.findByText('alpha-app')).toBeInTheDocument()
    expect(screen.getByText('zeta-app')).toBeInTheDocument()
  })

  it('reloads on demand', async () => {
    render(<AppsPage />)
    await screen.findByText('alpha-app')
    await user.click(screen.getByRole('button', { name: /刷\s*新/ }))
    await vi.waitFor(() => expect(apiMocks.getApps).toHaveBeenCalledTimes(2))
  })

  it('reports a load failure', async () => {
    apiMocks.getApps.mockRejectedValue({ response: { data: { message: 'registry closed' } } })
    render(<AppsPage />)
    expect(await screen.findByText('registry closed')).toBeInTheDocument()
  })
})
