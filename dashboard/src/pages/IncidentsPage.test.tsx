import { describe, it, expect, vi, beforeEach } from 'vitest'
import dayjs from 'dayjs'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('../api', () => ({
  heraldApi: {
    getIncidents: vi.fn(),
    getIncident: vi.fn(),
    ackAlert: vi.fn(),
  },
}))

import IncidentsPage, { incidentStatus, currentUsername } from './IncidentsPage'
import { heraldApi } from '../api'

const apiMocks = vi.mocked(heraldApi)

// 四行铺满状态/字段分支：open（有 alert_id，可确认）、acked（无 acked_by，
// 详情里确认人回落 '?'）、resolved（无 alert_id，无确认按钮）、blank
//（无标题无级别，占位符 '—'）。
const incidents = [
  {
    id: 'inc-open', rule_id: 'r1', alert_id: 'a1', title: '磁盘告警', level: 'error',
    opened_at: '2026-10-09T10:00:00+08:00',
  },
  {
    id: 'inc-acked', rule_id: 'r1', alert_id: 'a2', title: '内存告警', level: 'warning',
    opened_at: '2026-10-09T09:00:00+08:00', acked_at: '2026-10-09T09:05:00+08:00',
  },
  {
    id: 'inc-resolved', rule_id: 'r2', title: '网络告警', level: 'info',
    opened_at: '2026-10-09T08:00:00+08:00', resolved_at: '2026-10-09T08:30:00+08:00',
  },
  { id: 'inc-blank', title: '', opened_at: '2026-10-09T07:00:00+08:00' },
  // 未知级别：级别色表未收录，回落 default 色。
  { id: 'inc-notice', title: '通告', level: 'notice', opened_at: '2026-10-09T06:00:00+08:00' },
]

function renderPage() {
  return render(<IncidentsPage />)
}

const user = userEvent.setup()

describe('IncidentsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    apiMocks.getIncidents.mockResolvedValue({ data: { incidents } })
    apiMocks.getIncident.mockResolvedValue({ data: { ...incidents[0], timeline: [] } })
    apiMocks.ackAlert.mockResolvedValue({ data: { code: 0 } })
  })

  it('loads all incidents on mount and renders status tags', async () => {
    renderPage()
    expect(await screen.findByText('磁盘告警')).toBeInTheDocument()
    expect(apiMocks.getIncidents).toHaveBeenCalledWith({ status: 'all' })
    // open 行有三行（inc-open、inc-blank、inc-notice），acked/resolved 各一行。
    expect(screen.getAllByText('open').length).toBe(3)
    expect(screen.getByText('acked')).toBeInTheDocument()
    expect(screen.getByText('resolved')).toBeInTheDocument()
    // blank 行的标题与级别都回落占位符。
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(2)
    // 只有 open 且带 alert_id 的行有确认按钮。
    expect(screen.getAllByRole('button', { name: /确\s*认\s*告\s*警/ }).length).toBe(1)
  })

  it('reloads with the selected status filter', async () => {
    renderPage()
    await screen.findByText('磁盘告警')
    await user.click(screen.getByText('全部'))
    await user.click(await screen.findByText('已确认'))
    await waitFor(() => expect(apiMocks.getIncidents).toHaveBeenCalledWith({ status: 'acked' }))
  })

  it('refreshes with the current filter on the refresh button', async () => {
    renderPage()
    await screen.findByText('磁盘告警')
    await user.click(screen.getByRole('button', { name: /刷\s*新/ }))
    await waitFor(() => expect(apiMocks.getIncidents).toHaveBeenCalledTimes(2))
    expect(apiMocks.getIncidents).toHaveBeenLastCalledWith({ status: 'all' })
  })

  it('treats a response without incidents as an empty table', async () => {
    apiMocks.getIncidents.mockResolvedValue({ data: {} })
    renderPage()
    await screen.findByText('事件台账')
    await waitFor(() => expect(screen.queryByText('磁盘告警')).not.toBeInTheDocument())
  })

  it('opens the detail modal with group key, ack info and timeline', async () => {
    apiMocks.getIncident.mockResolvedValue({
      data: {
        ...incidents[0], group_key: 'env=prod', events: 3,
        acked_at: '2026-10-09T10:02:00+08:00', acked_by: 'ops',
        timeline: [
          { at: '2026-10-09T10:01:00+08:00', kind: 'escalated', detail: 'to L3' },
          { at: '2026-10-09T10:00:00+08:00', kind: 'opened' },
        ],
      },
    })
    renderPage()
    await screen.findByText('磁盘告警')
    await user.click(screen.getAllByRole('button', { name: /详\s*情/ })[0])
    expect(await screen.findByText('事件 inc-open')).toBeInTheDocument()
    expect(apiMocks.getIncident).toHaveBeenCalledWith('inc-open')
    expect(await screen.findByText(/escalated: to L3/)).toBeInTheDocument()
    // 无 detail 的时间线条目不带冒号。
    expect(screen.getByText(/opened$/)).toBeInTheDocument()
  })

  it('falls back to the row snapshot when the detail fetch fails', async () => {
    apiMocks.getIncident.mockRejectedValue(new Error('gone'))
    renderPage()
    await screen.findByText('磁盘告警')
    // 行内快照 = inc-acked：已确认但没记录确认人 → 展示 '?'。
    await user.click(screen.getAllByRole('button', { name: /详\s*情/ })[1])
    expect(await screen.findByText('事件 inc-acked')).toBeInTheDocument()
    expect(screen.getByText(/by \?/)).toBeInTheDocument()
  })

  it('shows 未确认/未恢复 placeholders and closes the modal', async () => {
    // 拉取详情剥掉除 resolved_at 外的全部字段：占位符各就各位。
    apiMocks.getIncident.mockResolvedValue({
      data: { id: 'inc-resolved', opened_at: '2026-10-09T08:00:00+08:00', resolved_at: '2026-10-09T08:30:00+08:00' },
    })
    renderPage()
    await screen.findByText('磁盘告警')
    await user.click(screen.getAllByRole('button', { name: /详\s*情/ })[2])
    expect(await screen.findByText('事件 inc-resolved')).toBeInTheDocument()
    expect(screen.getByText('未确认')).toBeInTheDocument()
    // 恢复时间用组件同款 dayjs 推期望串：测试机与 CI 时区不同（CI 跑 UTC），
    // 硬编码 +08:00 的渲染时刻会在 UTC 下错位。
    expect(screen.getByText(new RegExp(dayjs('2026-10-09T08:30:00+08:00').format('MM-DD HH:mm:ss')))).toBeInTheDocument()
    expect(screen.getByText('无')).toBeInTheDocument() // 时间线为空
    // 详情弹窗 footer={null}，关窗走右上角 Close。jsdom 里 rc-motion 的关闭
    // 动画不收敛、DOM 断言不可靠（仓库既有约定），行为断言：关窗不再拉详情。
    await user.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(apiMocks.getIncident).toHaveBeenCalledTimes(1))
  })

  it('acknowledges an open alert and refreshes', async () => {
    localStorage.setItem('herald_user', JSON.stringify({ username: 'alice' }))
    renderPage()
    await screen.findByText('磁盘告警')
    await user.click(screen.getByRole('button', { name: /确\s*认\s*告\s*警/ }))
    await user.click(await screen.findByRole('button', { name: /^确\s*认$/ }))
    await waitFor(() => expect(apiMocks.ackAlert).toHaveBeenCalledWith('a1', { acked_by: 'alice' }))
    expect(await screen.findByText('已确认')).toBeInTheDocument()
    await waitFor(() => expect(apiMocks.getIncidents).toHaveBeenCalledTimes(2))
  })

  it('reports ack failures with the backend message', async () => {
    apiMocks.ackAlert.mockRejectedValue({ response: { data: { message: 'ack store down' } } })
    renderPage()
    await screen.findByText('磁盘告警')
    await user.click(screen.getByRole('button', { name: /确\s*认\s*告\s*警/ }))
    await user.click(await screen.findByRole('button', { name: /^确\s*认$/ }))
    expect(await screen.findByText('ack store down')).toBeInTheDocument()
  })

  it('falls back to err.message when the ack failure has no payload', async () => {
    apiMocks.ackAlert.mockRejectedValue(new Error('network gone'))
    renderPage()
    await screen.findByText('磁盘告警')
    await user.click(screen.getByRole('button', { name: /确\s*认\s*告\s*警/ }))
    await user.click(await screen.findByRole('button', { name: /^确\s*认$/ }))
    expect(await screen.findByText('network gone')).toBeInTheDocument()
  })

  it('labels a bare ack failure as 确认失败', async () => {
    apiMocks.ackAlert.mockRejectedValue({})
    renderPage()
    await screen.findByText('磁盘告警')
    await user.click(screen.getByRole('button', { name: /确\s*认\s*告\s*警/ }))
    await user.click(await screen.findByRole('button', { name: /^确\s*认$/ }))
    expect(await screen.findByText('确认失败')).toBeInTheDocument()
  })

  it('reports load failures', async () => {
    apiMocks.getIncidents.mockRejectedValue({ response: { data: { message: 'incident store down' } } })
    renderPage()
    expect(await screen.findByText('incident store down')).toBeInTheDocument()
  })
})

describe('incidentStatus', () => {
  it('derives resolved > acked > open', () => {
    expect(incidentStatus({ resolved_at: 'x', acked_at: 'y' })).toBe('resolved')
    expect(incidentStatus({ acked_at: 'y' })).toBe('acked')
    expect(incidentStatus({})).toBe('open')
  })
})

describe('currentUsername', () => {
  beforeEach(() => localStorage.clear())

  it('reads the logged-in username', () => {
    localStorage.setItem('herald_user', JSON.stringify({ username: 'alice' }))
    expect(currentUsername()).toBe('alice')
  })

  it('falls back to admin when the user is absent or unparsable', () => {
    expect(currentUsername()).toBe('admin')
    localStorage.setItem('herald_user', '{not json')
    expect(currentUsername()).toBe('admin')
  })

  it('falls back to admin when the stored user has no username', () => {
    localStorage.setItem('herald_user', JSON.stringify({ id: '1' }))
    expect(currentUsername()).toBe('admin')
  })
})
