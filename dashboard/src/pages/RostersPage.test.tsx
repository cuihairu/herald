import { describe, it, expect, vi, beforeEach } from 'vitest'
import dayjs from 'dayjs'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('../api', () => ({
  heraldApi: {
    getRosters: vi.fn(),
    createRoster: vi.fn(),
    updateRoster: vi.fn(),
    deleteRoster: vi.fn(),
  },
}))

import RostersPage, { toRosterPayload } from './RostersPage'
import { heraldApi } from '../api'
import { useHeraldStore } from '../stores/herald'
import { resetStore } from '../test/store'

const apiMocks = vi.mocked(heraldApi)

// r2 覆盖省略字段的分支：无 description、无 periods。
const rosters = [
  {
    id: 'ops-oncall', description: '周末值班',
    periods: [
      { start: '2026-10-10T09:00:00+08:00', end: '2026-10-10T18:00:00+08:00' },
    ],
  },
  { id: 'minimal' },
]

function renderPage() {
  return render(<RostersPage />)
}

const user = userEvent.setup()

async function openEditor() {
  await user.click(await screen.findByRole('button', { name: /新\s*建\s*值\s*班\s*表/ }))
  await screen.findByRole('dialog')
}

// 打开编辑器并添加一行时段，返回该行的两个输入框。
async function addPeriodRow() {
  await user.click(screen.getByRole('button', { name: /添\s*加\s*时\s*段/ }))
  const starts = screen.getAllByPlaceholderText(/开\s*始/)
  const ends = screen.getAllByPlaceholderText(/结\s*束/)
  return { start: starts[starts.length - 1], end: ends[ends.length - 1] }
}

describe('RostersPage', () => {
  beforeEach(() => {
    resetStore()
    vi.clearAllMocks()
    apiMocks.getRosters.mockResolvedValue({ data: { rosters } })
    apiMocks.createRoster.mockResolvedValue({ data: { code: 0 } })
    apiMocks.updateRoster.mockResolvedValue({ data: { code: 0 } })
    apiMocks.deleteRoster.mockResolvedValue({ data: { code: 0 } })
  })

  it('renders rosters with description and formatted periods', async () => {
    renderPage()
    expect(await screen.findByText('ops-oncall')).toBeInTheDocument()
    expect(screen.getByText('周末值班')).toBeInTheDocument()
    // minimal 没有 description 也没有 periods → 两处占位符。
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(2)
    // 用组件同款 dayjs 格式化推期望串：测试机与 CI 的时区不同（CI 跑 UTC），
    // 硬编码 +08:00 的渲染串会在 UTC 下错位。
    const fmt = (t: string) => dayjs(t).format('MM-DD HH:mm')
    const start = '2026-10-10T09:00:00+08:00'
    const end = '2026-10-10T18:00:00+08:00'
    expect(screen.getByText(`${fmt(start)} ~ ${fmt(end)}`)).toBeInTheDocument()
  })

  it('creates a roster with periods', async () => {
    renderPage()
    await openEditor()

    await user.type(screen.getByPlaceholderText('ops-oncall'), 'weekend')
    const row = await addPeriodRow()
    await user.type(row.start, '2026-10-11T09:00:00+08:00')
    await user.type(row.end, '2026-10-11T18:00:00+08:00')

    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.createRoster).toHaveBeenCalledWith({
        id: 'weekend',
        description: undefined,
        periods: [{ start: '2026-10-11T09:00:00+08:00', end: '2026-10-11T18:00:00+08:00' }],
      })
    )
    expect(await screen.findByText('值班表已保存')).toBeInTheDocument()
  })

  it('saves a roster without any period rows', async () => {
    renderPage()
    await openEditor()
    await user.type(screen.getByPlaceholderText('ops-oncall'), 'plain')
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.createRoster).toHaveBeenCalledWith({
        id: 'plain',
        description: undefined,
        periods: [],
      })
    )
  })

  it('drops period rows left entirely blank', async () => {
    renderPage()
    await openEditor()
    await user.type(screen.getByPlaceholderText('ops-oncall'), 'cleaned')
    await addPeriodRow() // 整行留白
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.createRoster).toHaveBeenCalledWith({
        id: 'cleaned',
        description: undefined,
        periods: [],
      })
    )
  })

  it('trims whitespace and keeps a partially filled period row', async () => {
    renderPage()
    await openEditor()
    await user.type(screen.getByPlaceholderText('ops-oncall'), 'padded')
    const row = await addPeriodRow()
    await user.type(row.start, '  2026-10-11T09:00:00+08:00  ')
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.createRoster).toHaveBeenCalledWith({
        id: 'padded',
        description: undefined,
        periods: [{ start: '2026-10-11T09:00:00+08:00', end: '' }],
      })
    )
  })

  it('edits an existing roster with prefilled periods and id locked', async () => {
    renderPage()
    const editButtons = await screen.findAllByRole('button', { name: /编\s*辑/ })
    await user.click(editButtons[0])
    expect(await screen.findByText('编辑值班表 ops-oncall')).toBeInTheDocument()

    const idInput = screen.getByDisplayValue('ops-oncall') as HTMLInputElement
    expect(idInput.disabled).toBe(true)
    expect(screen.getByDisplayValue('周末值班')).toBeInTheDocument()
    expect(screen.getByDisplayValue('2026-10-10T09:00:00+08:00')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.updateRoster).toHaveBeenCalledWith('ops-oncall', expect.objectContaining({ id: 'ops-oncall' }))
    )
    expect(await screen.findByText('值班表已保存')).toBeInTheDocument()
  })

  it('removes a period row from the editor', async () => {
    renderPage()
    const editButtons = await screen.findAllByRole('button', { name: /编\s*辑/ })
    await user.click(editButtons[0])
    await screen.findByText('编辑值班表 ops-oncall')
    // queryAll：删掉最后一行后 getAll 会因零匹配抛错，query 才返回 []。
    const removeButtons = () => screen.queryAllByRole('button', { name: /移\s*除\s*时\s*段/ })
    expect(removeButtons().length).toBe(1)
    await user.click(removeButtons()[0])
    expect(removeButtons().length).toBe(0)
    // 删空后保存：时段表为空。
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.updateRoster).toHaveBeenCalledWith('ops-oncall', expect.objectContaining({
        periods: [],
      }))
    )
  })

  it('blocks saving when the id is missing', async () => {
    renderPage()
    await openEditor()
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('请输入值班表 ID')).toBeInTheDocument()
    expect(apiMocks.createRoster).not.toHaveBeenCalled()
  })

  it('surfaces backend 409 on duplicate id and keeps the editor open', async () => {
    apiMocks.createRoster.mockRejectedValue({ response: { data: { message: 'roster already exists: dup' } } })
    renderPage()
    await openEditor()
    await user.type(screen.getByPlaceholderText('ops-oncall'), 'dup')
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('roster already exists: dup')).toBeInTheDocument()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('falls back to the error message when the failure has no payload', async () => {
    apiMocks.updateRoster.mockRejectedValue(new Error('network gone'))
    renderPage()
    const editButtons = await screen.findAllByRole('button', { name: /编\s*辑/ })
    await user.click(editButtons[1])
    await user.click(await screen.findByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('network gone')).toBeInTheDocument()
  })

  it('labels a failure with neither payload nor message as 保存失败', async () => {
    apiMocks.createRoster.mockRejectedValue({})
    renderPage()
    await openEditor()
    await user.type(screen.getByPlaceholderText('ops-oncall'), 'no-msg')
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('保存失败')).toBeInTheDocument()
  })

  it('deletes a roster after confirmation', async () => {
    renderPage()
    const deleteButtons = await screen.findAllByRole('button', { name: /删\s*除/ })
    await user.click(deleteButtons[1])
    await user.click(await screen.findByRole('button', { name: /^确\s*认$/ }))
    await waitFor(() => expect(apiMocks.deleteRoster).toHaveBeenCalledWith('minimal'))
    expect(await screen.findByText('值班表已删除')).toBeInTheDocument()
    await waitFor(() => expect(apiMocks.getRosters).toHaveBeenCalledTimes(2))
  })

  it('reports delete failures', async () => {
    apiMocks.deleteRoster.mockRejectedValue({ response: { data: { message: 'roster in use' } } })
    renderPage()
    const deleteButtons = await screen.findAllByRole('button', { name: /删\s*除/ })
    await user.click(deleteButtons[0])
    await user.click(await screen.findByRole('button', { name: /^确\s*认$/ }))
    expect(await screen.findByText('roster in use')).toBeInTheDocument()
  })

  it('closes the editor without saving on cancel', async () => {
    renderPage()
    await openEditor()
    await user.click(screen.getByRole('button', { name: /取\s*消/ }))
    // jsdom 里 rc-motion 的关闭动画不收敛，DOM 断言不可靠；
    // 行为断言：取消不发出任何请求。
    await waitFor(() => expect(apiMocks.createRoster).not.toHaveBeenCalled())
  })
})

describe('toRosterPayload', () => {
  // periods 兜底右支：表单值里缺 periods 字段（antd Form.List 契约变化、
  // 或表单未挂载就被提交）时按空时段表处理，而不是崩在 .map 上。
  it('treats missing periods as an empty list', () => {
    // 键整个缺失
    expect(toRosterPayload({ id: 'ops' })).toEqual({
      id: 'ops',
      description: undefined,
      periods: [],
    })
    // 键存在但显式 undefined（antd Form 清空字段的真实形状），同一右支
    expect(toRosterPayload({ id: 'ops', periods: undefined })).toEqual({
      id: 'ops',
      description: undefined,
      periods: [],
    })
  })

  it('drops blank rows and trims the rest', () => {
    expect(toRosterPayload({
      id: 'ops',
      description: '周末值班',
      periods: [
        { start: '  ', end: '' },                    // 整行留白 → 丢
        { start: ' s1 ', end: ' e1 ' },              // 两侧裁剪
        { start: 's2', end: '' },                    // 半行保留
        { start: '', end: 'e3' },                    // 另半行保留
      ],
    })).toEqual({
      id: 'ops',
      description: '周末值班',
      periods: [
        { start: 's1', end: 'e1' },
        { start: 's2', end: '' },
        { start: '', end: 'e3' },
      ],
    })
  })
})
