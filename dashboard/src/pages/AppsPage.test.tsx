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

  it('renders an empty table when the registry omits the apps field', async () => {
    // `apps || []` 兜底臂：响应缺字段而不是空数组。
    apiMocks.getApps.mockResolvedValue({ data: {} })
    render(<AppsPage />)
    await screen.findByText('Apps 集成命名空间')
    expect((await screen.findAllByText('No data')).length).toBeGreaterThan(0)
    expect(apiMocks.getApps).toHaveBeenCalledTimes(1)
  })
})
