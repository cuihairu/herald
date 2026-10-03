import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('../api', () => ({
  heraldApi: {
    getRules: vi.fn(),
    getRule: vi.fn(),
    createRule: vi.fn(),
    updateRule: vi.fn(),
    deleteRule: vi.fn(),
  },
}))

import RulesPage from './RulesPage'
import { heraldApi } from '../api'
import { useHeraldStore } from '../stores/herald'
import { resetStore } from '../test/store'

const apiMocks = vi.mocked(heraldApi)

// 覆盖 mode / action 的默认值分支：r2 不带 mode 与 action（后端
// 省略零值字段），r3 是 suppress 动作（无路由渠道列）。r1 带影子命中
// 计数，r2/r3 没有（后端只在该规则有过影子命中时才非零）。
const rules = [
  { id: 'r1', match: 'level == "error"', mode: 'active', action: 'route', priority: 10, shadow_hits: 7, route: [{ channels: ['oncall'] }] },
  { id: 'r2', match: 'env == "prod"', mode: '', action: '', priority: 0 },
  { id: 'r3', match: 'type == "noise"', mode: 'shadow', action: 'suppress', priority: -1 },
]

function renderPage() {
  render(<RulesPage />)
}

const user = userEvent.setup()

async function openEditor() {
  await user.click(await screen.findByRole('button', { name: /新\s*建\s*规\s*则/ }))
  await screen.findByRole('dialog')
}

describe('RulesPage', () => {
  beforeEach(() => {
    resetStore()
    vi.clearAllMocks()
    apiMocks.getRules.mockResolvedValue({ data: { rules } })
    apiMocks.getRule.mockResolvedValue({
      data: { data: { id: 'r1', shadow_stats: { total: 0, last_24h: 0, last_7d: 0 } } },
    })
    apiMocks.createRule.mockResolvedValue({ data: { code: 0 } })
    apiMocks.updateRule.mockResolvedValue({ data: { code: 0 } })
    apiMocks.deleteRule.mockResolvedValue({ data: { code: 0 } })
  })

  it('renders every rule with its mode, action and channels', async () => {
    renderPage()
    expect(await screen.findByText('r1')).toBeInTheDocument()
    // r1: active → 生效标签 + 开关打开；r2 空字段回落 route/观察。
    expect(screen.getByText('生效')).toBeInTheDocument()
    expect(screen.getAllByText('观察').length).toBe(2)
    expect(screen.getAllByText('改道').length).toBe(2)
    expect(screen.getByText('抑制')).toBeInTheDocument()
    expect(screen.getByText('oncall')).toBeInTheDocument()
    // 空字段回落默认动作（route）标签而不是空白。
    expect(screen.getByText('level == "error"')).toBeInTheDocument()
  })

  it('shows the shadow hit count reported by the backend', async () => {
    renderPage()
    expect(await screen.findByText('r1')).toBeInTheDocument()
    // r1 影子期命中 7 次。
    expect(screen.getByText('7')).toBeInTheDocument()
    // r2/r3 回落 0 而不是空白（0 同时出现在 r2 的优先级列）。
    expect(screen.getAllByText('0').length).toBe(3)
  })

  it('toggles a rule between shadow and active via the switch', async () => {
    renderPage()
    const switches = await screen.findAllByRole('switch')
    expect(switches.length).toBe(3)
    // r2 的开关（shadow）点开 → PUT mode=active，带上原字段整体替换。
    await user.click(switches[1])
    await waitFor(() =>
      expect(apiMocks.updateRule).toHaveBeenCalledWith('r2', expect.objectContaining({ id: 'r2', mode: 'active' }))
    )
    await waitFor(() => expect(apiMocks.getRules).toHaveBeenCalledTimes(2))
  })

  it('switches an active rule back to shadow', async () => {
    renderPage()
    const switches = await screen.findAllByRole('switch')
    // r1 是 active → 开关已打开，点它落到 shadow。
    await user.click(switches[0])
    await waitFor(() =>
      expect(apiMocks.updateRule).toHaveBeenCalledWith('r1', expect.objectContaining({ id: 'r1', mode: 'shadow' }))
    )
    await waitFor(() => expect(apiMocks.getRules).toHaveBeenCalledTimes(2))
  })

  it('reports toggle failures without crashing', async () => {
    apiMocks.updateRule.mockRejectedValue({ response: { data: { message: 'boom' } } })
    renderPage()
    const switches = await screen.findAllByRole('switch')
    await user.click(switches[1])
    expect(await screen.findByText('boom')).toBeInTheDocument()
    expect(screen.getByText('r2')).toBeInTheDocument()
  })

  it('creates a route rule through the editor', async () => {
    renderPage()
    await openEditor()

    await user.type(screen.getByPlaceholderText('prod-payment-failure'), 'new-rule')
    await user.type(screen.getByPlaceholderText(/fail_rate/), 'level == "warning"')
    // 路由渠道是 tags 选择器：其占位符是 span 而非 input 属性，
    // 取弹窗内最后一个 combobox（mode/action 之后的 channels）。
    const combos = screen.getAllByRole('combobox')
    await user.type(combos[combos.length - 1], 'feishu-oncall{enter}')

    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.createRule).toHaveBeenCalledWith({
        id: 'new-rule',
        match: 'level == "warning"',
        mode: 'shadow',
        action: 'route',
        priority: 0,
        route: [{ channels: ['feishu-oncall'] }],
      })
    )
    expect(await screen.findByText('规则已保存')).toBeInTheDocument()
  })

  it('hides the channel field for non-route rules', async () => {
    renderPage()
    const editButtons = await screen.findAllByRole('button', { name: /编\s*辑/ })
    // r3 是 suppress 规则：编辑弹窗不出现渠道输入。
    await user.click(editButtons[2])
    await screen.findByText('编辑规则 r3')
    // suppress 无路由渠道步骤 → 弹窗内只有 mode/action 两个选择器。
    expect(screen.getAllByRole('combobox').length).toBe(2)
  })

  it('saves a suppress rule without any route step', async () => {
    renderPage()
    const editButtons = await screen.findAllByRole('button', { name: /编\s*辑/ })
    await user.click(editButtons[2])
    await screen.findByText('编辑规则 r3')
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    // 后端在 allow/suppress 上拒收 route 字段（route 是改道专属语义），
    // 所以 payload 里必须整个没有 route 键，而不只是 route 为空。
    await waitFor(() => expect(apiMocks.updateRule).toHaveBeenCalledTimes(1))
    const [id, payload] = apiMocks.updateRule.mock.calls[0]
    expect(id).toBe('r3')
    expect(payload).not.toHaveProperty('route')
    expect(payload).toMatchObject({ id: 'r3', mode: 'shadow', action: 'suppress', priority: -1 })
  })

  it('shows client-side validation errors and stays open', async () => {
    renderPage()
    await openEditor()
    // 不填任何字段直接保存：必填校验拦截，不发请求。
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('请输入规则 ID')).toBeInTheDocument()
    expect(apiMocks.createRule).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('surfaces backend 400 messages when the expression fails to compile', async () => {
    apiMocks.createRule.mockRejectedValue({ response: { data: { message: 'invalid expression' } } })
    renderPage()
    await openEditor()
    await user.type(screen.getByPlaceholderText('prod-payment-failure'), 'bad')
    await user.type(screen.getByPlaceholderText(/fail_rate/), '&&&')
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('invalid expression')).toBeInTheDocument()
    // 弹窗保持打开，用户可以修正表达式。
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('edits an existing rule with the id locked and prefilled', async () => {
    renderPage()
    const editButtons = await screen.findAllByRole('button', { name: /编\s*辑/ })
    await user.click(editButtons[0])
    expect(await screen.findByText('编辑规则 r1')).toBeInTheDocument()

    const idInput = screen.getByDisplayValue('r1') as HTMLInputElement
    expect(idInput.disabled).toBe(true)
    expect(screen.getByDisplayValue('level == "error"')).toBeInTheDocument()

    // 10（优先级）→ 清空后输入 99。
    const priority = screen.getByRole('spinbutton')
    await user.clear(priority)
    await user.type(priority, '99')
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.updateRule).toHaveBeenCalledWith('r1', expect.objectContaining({ id: 'r1', priority: 99 }))
    )
  })

  it('falls back to the generic message when a save failure has no payload', async () => {
    apiMocks.updateRule.mockRejectedValue(new Error('network gone'))
    renderPage()
    const editButtons = await screen.findAllByRole('button', { name: /编\s*辑/ })
    await user.click(editButtons[0])
    await user.click(await screen.findByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('network gone')).toBeInTheDocument()
  })

  it('labels a failure with neither payload nor message as 保存失败', async () => {
    // 拒绝值里既没有 response.data.message 也没有 message，
    // errMsg 三级 || 只剩最后一级：组件内写死的兜底文案。
    apiMocks.createRule.mockRejectedValue({})
    renderPage()
    await openEditor()
    await user.type(screen.getByPlaceholderText('prod-payment-failure'), 'no-msg')
    await user.type(screen.getByPlaceholderText(/fail_rate/), 'level == "warning"')
    // 不填渠道：validateFields 通过但 channels 回落空数组。
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('保存失败')).toBeInTheDocument()
  })

  it('prefills zero priority and no channels for a rule that omits both', async () => {
    // 后端省略零值字段：priority 缺失回落 0、route 缺失回落空渠道列表。
    apiMocks.getRules.mockResolvedValue({ data: { rules: [{ id: 'bare', match: 'level == "error"' }] } })
    renderPage()
    const editButtons = await screen.findAllByRole('button', { name: /编\s*辑/ })
    await user.click(editButtons[0])
    await screen.findByText('编辑规则 bare')
    expect((screen.getByRole('spinbutton') as HTMLInputElement).value).toBe('0')

    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.updateRule).toHaveBeenCalledWith('bare', {
        id: 'bare',
        match: 'level == "error"',
        mode: 'shadow',
        action: 'route',
        priority: 0,
        route: [{ channels: [] }],
      })
    )
  })

  it('deletes a rule after confirmation', async () => {
    renderPage()
    // r2 的删除按钮。
    const deleteButtons = await screen.findAllByRole('button', { name: /删\s*除/ })
    await user.click(deleteButtons[1])
    await user.click(await screen.findByRole('button', { name: /^确\s*认$/ }))
    await waitFor(() => expect(apiMocks.deleteRule).toHaveBeenCalledWith('r2'))
    expect(await screen.findByText('规则已删除')).toBeInTheDocument()
    await waitFor(() => expect(apiMocks.getRules).toHaveBeenCalledTimes(2))
  })

  it('reports delete failures', async () => {
    apiMocks.deleteRule.mockRejectedValue({ response: { data: { message: 'cannot delete' } } })
    renderPage()
    const deleteButtons = await screen.findAllByRole('button', { name: /删\s*除/ })
    await user.click(deleteButtons[0])
    await user.click(await screen.findByRole('button', { name: /^确\s*认$/ }))
    expect(await screen.findByText('cannot delete')).toBeInTheDocument()
  })

  it('closes the editor without saving on cancel', async () => {
    renderPage()
    await openEditor()
    await user.click(screen.getByRole('button', { name: /取\s*消/ }))
    // jsdom 里 rc-motion 的关闭动画不收敛，DOM 断言不可靠；
    // 行为断言：取消不发出任何请求。
    await waitFor(() => expect(apiMocks.createRule).not.toHaveBeenCalled())
  })

  it('opens the shadow detail drawer with windows and samples', async () => {
    // r1 影子命中 9 次：5 次在近 24 小时；样本最新在前，首条带渠道与
    // 级别，次条两者皆缺（模板通知没有内联标题）。
    apiMocks.getRule.mockResolvedValue({
      data: {
        data: {
          id: 'r1',
          shadow_hits: 9,
          shadow_stats: {
            total: 9,
            last_24h: 5,
            last_7d: 9,
            samples: [
              { at: '2026-10-03T08:00:00Z', type: 'deploy', level: 'error', title: '上线完成', channels: ['sms-duty'] },
              { at: '2026-10-03T07:00:00Z', type: 'alert', title: '磁盘告警' },
            ],
          },
        },
      },
    })
    renderPage()
    const detailButtons = await screen.findAllByRole('button', { name: /详\s*情/ })
    await user.click(detailButtons[0])
    expect(await screen.findByText('影子统计详情')).toBeInTheDocument()
    // 样本表渲染：标题、类型与渠道（sms-duty 不与列表渠道列混淆）。
    expect(screen.getByText('上线完成')).toBeInTheDocument()
    expect(screen.getByText('磁盘告警')).toBeInTheDocument()
    expect(screen.getByText('sms-duty')).toBeInTheDocument()
    await waitFor(() => expect(apiMocks.getRule).toHaveBeenCalledWith('r1'))

    // 关闭抽屉后可重新打开：onClose 复位状态，重开触发重新拉取。
    // （rc-motion 关闭动画在 jsdom 不收敛，用行为断言。）
    await user.click(screen.getByLabelText('Close'))
    const buttons = await screen.findAllByRole('button', { name: /详\s*情/ })
    await user.click(buttons[0])
    await waitFor(() => expect(apiMocks.getRule).toHaveBeenCalledTimes(2))
  })

  it('shows an empty sample list for a rule without hits', async () => {
    // 从未命中过的规则：后端省略 samples 字段，详情回落空表。
    apiMocks.getRule.mockResolvedValue({
      data: { data: { id: 'r2', shadow_stats: { total: 0, last_24h: 0, last_7d: 0 } } },
    })
    renderPage()
    const detailButtons = await screen.findAllByRole('button', { name: /详\s*情/ })
    await user.click(detailButtons[1])
    expect(await screen.findByText('暂无命中样本')).toBeInTheDocument()
    await waitFor(() => expect(apiMocks.getRule).toHaveBeenCalledWith('r2'))
  })

  it('reports detail load failures', async () => {
    apiMocks.getRule.mockRejectedValue({ response: { data: { message: 'rule detail down' } } })
    renderPage()
    const detailButtons = await screen.findAllByRole('button', { name: /详\s*情/ })
    await user.click(detailButtons[0])
    expect(await screen.findByText('rule detail down')).toBeInTheDocument()
  })
})
