import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('../api', () => ({
  heraldApi: {
    getFeeds: vi.fn(),
  },
}))

import RssPage, { feedUrlOrigin } from './RssPage'
import { heraldApi } from '../api'

const apiMocks = vi.mocked(heraldApi)
const user = userEvent.setup()

function stubClipboard(write: () => Promise<void>) {
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: vi.fn(write) },
    configurable: true,
  })
}

const feeds = {
  public: [{
    name: 'alerts', url: '/feeds/alerts.xml', total: 2,
    recent: [{ id: 'p1', title: '公开告警' }, { id: 'p2', title: '第二条' }],
  }],
  private: [{
    audience: 'alice', url: '/feeds/private/tok123.xml', total: 1,
    recent: [{ id: 'q1', title: '私人账单' }],
  }],
}

describe('feedUrlOrigin', () => {
  it('returns the current origin', () => {
    expect(feedUrlOrigin()).toBe(window.location.origin)
  })
})

describe('RssPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stubClipboard(() => Promise.resolve())
    apiMocks.getFeeds.mockResolvedValue({ data: feeds })
  })

  it('lists public and private feeds with recent entries and absolute urls', async () => {
    render(<RssPage />)
    expect(await screen.findByText('alerts')).toBeInTheDocument()
    expect(screen.getByText('alice')).toBeInTheDocument()
    expect(screen.getByText('公开告警')).toBeInTheDocument()
    expect(screen.getByText('私人账单')).toBeInTheDocument()
    expect(screen.getAllByText(window.location.origin + '/feeds/alerts.xml')).toHaveLength(1)
  })

  it('copies a feed address', async () => {
    render(<RssPage />)
    await user.click((await screen.findAllByRole('button', { name: /复\s*制\s*地\s*址/ }))[0])
    expect(await screen.findByText('已复制 feed 地址')).toBeInTheDocument()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(window.location.origin + '/feeds/alerts.xml')
  })

  it('reports a copy failure', async () => {
    stubClipboard(() => Promise.reject(new Error('denied')))
    render(<RssPage />)
    await user.click((await screen.findAllByRole('button', { name: /复\s*制\s*地\s*址/ }))[0])
    expect(await screen.findByText('复制失败')).toBeInTheDocument()
  })

  it('reports a load failure', async () => {
    apiMocks.getFeeds.mockRejectedValue(new Error('net down'))
    render(<RssPage />)
    expect(await screen.findByText('net down')).toBeInTheDocument()
  })

  it('renders empty tables when no feeds exist', async () => {
    apiMocks.getFeeds.mockResolvedValue({ data: {} })
    render(<RssPage />)
    await screen.findByText('公开 Feed')
    await vi.waitFor(() => {
      expect(document.querySelectorAll('.ant-spin-spinning').length).toBe(0)
    })
  })
})
