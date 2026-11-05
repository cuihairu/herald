import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('../api', () => ({
  heraldApi: {
    getAudiences: vi.fn(),
    getAudienceSurfaces: vi.fn(),
    getAudienceRelations: vi.fn(),
  },
}))

import AudiencesPage from './AudiencesPage'
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

const audiences = [
  { id: 'ops', recipients: ['alice', 'bob'] },
  { id: 'empty', recipients: [] },
  { id: 'ghost' },
]

async function openDetail(id: string) {
  const buttons = await screen.findAllByRole('button', { name: /查\s*看\s*详\s*情/ })
  await user.click(buttons[0])
  await screen.findByText(`受众 ${id}`)
}

describe('AudiencesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stubClipboard(() => Promise.resolve())
    apiMocks.getAudiences.mockResolvedValue({ data: { audiences, count: 2 } })
    apiMocks.getAudienceSurfaces.mockResolvedValue({
      data: { surfaces: [{ audience_id: 'ops', channel: 'rss', target: 'feed', status: 'active' }], rss_token: 'tok123' },
    })
    apiMocks.getAudienceRelations.mockResolvedValue({
      data: { relations: [{ audience_id: 'ops', category: 'alerts', channel: 'rss', type: 'subscription', source: 'preference_center' }] },
    })
  })

  it('lists audiences and shows a dash for empty recipients', async () => {
    render(<AudiencesPage />)
    expect(await screen.findByText('ops')).toBeInTheDocument()
    expect(screen.getByText('alice, bob')).toBeInTheDocument()
    // 空 recipients 与缺失 recipients 都展示 —。
    expect(screen.getAllByText('—').length).toBeGreaterThan(0)
  })

  it('reports a load failure', async () => {
    apiMocks.getAudiences.mockRejectedValue(new Error('net down'))
    render(<AudiencesPage />)
    expect(await screen.findByText('net down')).toBeInTheDocument()
  })

  it('shows relations and surfaces tabs with a copyable rss token', async () => {
    render(<AudiencesPage />)
    await openDetail('ops')

    // 订阅关系 tab（默认激活）。
    expect(await screen.findByText('alerts')).toBeInTheDocument()
    expect(screen.getByText('subscription')).toBeInTheDocument()

    // 联系面 tab。
    await user.click(screen.getByRole('tab', { name: /联\s*系\s*面/ }))
    expect(await screen.findByText('feed')).toBeInTheDocument()
    expect(screen.getByText('tok123')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /复\s*制/ }))
    expect(await screen.findByText('已复制私密 feed token')).toBeInTheDocument()

    // 复制失败分支。
    stubClipboard(() => Promise.reject(new Error('denied')))
    await user.click(screen.getByRole('button', { name: /复\s*制/ }))
    expect(await screen.findByText('复制失败')).toBeInTheDocument()
  })

  it('falls back to empty tabs when the detail reads fail', async () => {
    apiMocks.getAudienceSurfaces.mockRejectedValue(new Error('no adapter'))
    apiMocks.getAudienceRelations.mockRejectedValue(new Error('no registry'))
    render(<AudiencesPage />)
    await openDetail('ops')
    await user.click(await screen.findByRole('tab', { name: /联\s*系\s*面/ }))
    // surfaces/relations 读失败：没有任何数据行与 token 行。
    expect((await screen.findAllByText('No data')).length).toBeGreaterThan(0)
    expect(screen.queryByText('私密 RSS feed token：')).not.toBeInTheDocument()
  })

  it('defaults missing surfaces, rss token and relations to empty', async () => {
    // openDetail 里 surfaces.data.surfaces / rss_token / relations 各自的
    // `|| []` / `|| ''` 兜底臂：成功响应缺字段而不是空数组。
    apiMocks.getAudienceSurfaces.mockResolvedValue({ data: {} })
    apiMocks.getAudienceRelations.mockResolvedValue({ data: {} })
    render(<AudiencesPage />)
    await openDetail('ops')
    await user.click(await screen.findByRole('tab', { name: /联\s*系\s*面/ }))
    expect((await screen.findAllByText('No data')).length).toBeGreaterThan(0)
    expect(screen.queryByText('私密 RSS feed token：')).not.toBeInTheDocument()
  })

  it('renders an empty list when the response omits the audiences field', async () => {
    // `audiences || []` 兜底臂。
    apiMocks.getAudiences.mockResolvedValue({ data: {} })
    render(<AudiencesPage />)
    await screen.findByText('受众与订阅')
    expect((await screen.findAllByText('No data')).length).toBeGreaterThan(0)
  })

  it('closes the detail modal from the header close button', async () => {
    render(<AudiencesPage />)
    await openDetail('ops')
    expect(await screen.findByText('alerts')).toBeInTheDocument()

    // footer={null}：右上角 X（aria-label=Close）是唯一关闭入口。
    await user.click(screen.getByRole('button', { name: /close/i }))
    const dialog = screen.getByRole('dialog') as HTMLElement
    await waitFor(() => {
      expect(dialog.className).toMatch(/-leave/)
    })
  })
})
