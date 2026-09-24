import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'

const getWorkers = vi.fn()
vi.mock('../api', () => ({
  heraldApi: {
    getWorkers: (...a: unknown[]) => getWorkers(...a),
    getQueue: vi.fn().mockResolvedValue({ data: { size: 7 } }),
  },
}))

import WorkersPage from './WorkersPage'

describe('WorkersPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getWorkers.mockResolvedValue({ data: { workers: [] } })
  })

  it('shows a spinner while the first load is in flight', () => {
    getWorkers.mockReturnValue(new Promise(() => {}))
    const { container } = render(<WorkersPage />)
    expect(container.querySelector('.ant-spin')).toBeInTheDocument()
  })

  it('shows the empty state when no worker is connected', async () => {
    render(<WorkersPage />)
    expect(await screen.findByText('暂无连接的 Workers')).toBeInTheDocument()
    expect(screen.getByText('队列大小')).toBeInTheDocument()
    expect(screen.getByText('7')).toBeInTheDocument()
  })

  it('renders a fresh worker with capabilities and status entries', async () => {
    getWorkers.mockResolvedValue({
      data: {
        workers: [
          {
            worker_id: 'w-1',
            platform: 'linux',
            version: '1.2.0',
            capabilities: ['notify', 'ack'],
            connected_at: new Date(Date.now() - 5000).toISOString(),
            last_heartbeat: new Date(Date.now() - 1000).toISOString(),
            status: { queue: 3 },
          },
        ],
      },
    })
    render(<WorkersPage />)
    expect(await screen.findByText('w-1')).toBeInTheDocument()
    expect(screen.getByText('linux')).toBeInTheDocument()
    expect(screen.getByText('notify')).toBeInTheDocument()
    expect(screen.getByText('ack')).toBeInTheDocument()
    expect(screen.getByText('queue: 3')).toBeInTheDocument()
    // 新鲜心跳 → success 徽标
    expect(document.querySelector('.ant-badge-status-success')).toBeInTheDocument()
  })

  it('flags a stale heartbeat with the error badge', async () => {
    getWorkers.mockResolvedValue({
      data: {
        workers: [
          {
            worker_id: 'w-stale',
            platform: 'windows',
            version: '0.9',
            connected_at: new Date(Date.now() - 600000).toISOString(),
            last_heartbeat: new Date(Date.now() - 120000).toISOString(), // > 60s
          },
        ],
      },
    })
    render(<WorkersPage />)
    expect(await screen.findByText('w-stale')).toBeInTheDocument()
    expect(document.querySelector('.ant-badge-status-error')).toBeInTheDocument()
  })

  it('omits the status row when a worker reports no status', async () => {
    getWorkers.mockResolvedValue({
      data: {
        workers: [
          {
            worker_id: 'w-nostatus',
            platform: 'macos',
            version: '1.0',
            connected_at: new Date().toISOString(),
            last_heartbeat: new Date().toISOString(),
            status: {},
          },
        ],
      },
    })
    render(<WorkersPage />)
    expect(await screen.findByText('w-nostatus')).toBeInTheDocument()
    expect(screen.queryByText('状态')).not.toBeInTheDocument()
  })
})
