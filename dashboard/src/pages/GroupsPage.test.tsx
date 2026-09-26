import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('../api', () => ({
  heraldApi: {
    getGroups: vi.fn(),
    createGroup: vi.fn(),
    updateGroup: vi.fn(),
    deleteGroup: vi.fn(),
  },
}))

import GroupsPage from './GroupsPage'
import { heraldApi } from '../api'
import { useHeraldStore } from '../stores/herald'
import { resetStore } from '../test/store'

const apiMocks = vi.mocked(heraldApi)

// g2 覆盖省略字段的分支：无 description、成员无 recipients 钉选。
const groups = [
  {
    id: 'ops', description: '值班花名册',
    members: [
      { channel: 'feishu-oncall', recipients: ['@zhang'] },
      { channel: 'sms-duty' },
    ],
  },
  { id: 'minimal', members: [{ channel: 'wecom' }] },
]

function renderPage() {
  return render(<GroupsPage />)
}

const user = userEvent.setup()

async function openEditor() {
  await user.click(await screen.findByRole('button', { name: /新\s*建\s*群\s*组/ }))
  await screen.findByRole('dialog')
}

// 打开编辑器并添加一行成员，返回该行的渠道输入框。
async function addMemberRow(channel: string) {
  await user.click(screen.getByRole('button', { name: /添\s*加\s*成\s*员/ }))
  const inputs = screen.getAllByPlaceholderText('feishu-oncall')
  await user.type(inputs[inputs.length - 1], channel)
}

describe('GroupsPage', () => {
  beforeEach(() => {
    resetStore()
    vi.clearAllMocks()
    apiMocks.getGroups.mockResolvedValue({ data: { groups } })
    apiMocks.createGroup.mockResolvedValue({ data: { code: 0 } })
    apiMocks.updateGroup.mockResolvedValue({ data: { code: 0 } })
    apiMocks.deleteGroup.mockResolvedValue({ data: { code: 0 } })
  })

  it('renders groups with member tags and recipients', async () => {
    renderPage()
    expect(await screen.findByText('ops')).toBeInTheDocument()
    expect(screen.getByText('值班花名册')).toBeInTheDocument()
    // g2 没有 description → 占位符。
    expect(screen.getByText('—')).toBeInTheDocument()
    // 带收件人的成员标签展示钉选名单。
    expect(screen.getByText(/feishu-oncall \(@zhang\)/)).toBeInTheDocument()
    expect(screen.getByText('sms-duty')).toBeInTheDocument()
    expect(screen.getByText('wecom')).toBeInTheDocument()
  })

  it('creates a group with members and pinned recipients', async () => {
    renderPage()
    await openEditor()

    await user.type(screen.getByPlaceholderText('ops-oncall'), 'new-group')
    await addMemberRow('feishu-oncall')
    // 收件人是 tags 选择器：占位符是 span 而非 input 属性；
    // 弹窗内 combobox 即该行的收件人选择器。
    const combos = screen.getAllByRole('combobox')
    await user.type(combos[combos.length - 1], '@zhang{enter}')

    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.createGroup).toHaveBeenCalledWith({
        id: 'new-group',
        description: undefined,
        members: [{ channel: 'feishu-oncall', recipients: ['@zhang'] }],
      })
    )
    expect(await screen.findByText('群组已保存')).toBeInTheDocument()
  })

  it('saves members without a recipients field when none pinned', async () => {
    renderPage()
    await openEditor()
    await user.type(screen.getByPlaceholderText('ops-oncall'), 'plain')
    await addMemberRow('wecom')
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.createGroup).toHaveBeenCalledWith({
        id: 'plain',
        description: undefined,
        members: [{ channel: 'wecom' }],
      })
    )
  })

  it('edits an existing group with prefilled roster and id locked', async () => {
    renderPage()
    const editButtons = await screen.findAllByRole('button', { name: /编\s*辑/ })
    await user.click(editButtons[0])
    expect(await screen.findByText('编辑群组 ops')).toBeInTheDocument()

    const idInput = screen.getByDisplayValue('ops') as HTMLInputElement
    expect(idInput.disabled).toBe(true)
    expect(screen.getByDisplayValue('值班花名册')).toBeInTheDocument()
    // 既有成员行回填：渠道与收件人。
    expect(screen.getByDisplayValue('feishu-oncall')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.updateGroup).toHaveBeenCalledWith('ops', expect.objectContaining({ id: 'ops' }))
    )
    expect(await screen.findByText('群组已保存')).toBeInTheDocument()
  })

  it('removes a member row from the editor', async () => {
    renderPage()
    const editButtons = await screen.findAllByRole('button', { name: /编\s*辑/ })
    await user.click(editButtons[0])
    await screen.findByText('编辑群组 ops')
    // 两行成员 → 两个行内移除按钮；删掉第一行。
    const removeButtons = () => screen.getAllByRole('button', { name: /移\s*除\s*成\s*员/ })
    expect(removeButtons().length).toBe(2)
    await user.click(removeButtons()[0])
    expect(removeButtons().length).toBe(1)
    // 删剩一行保存：花名册只剩 wecom。
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.updateGroup).toHaveBeenCalledWith('ops', expect.objectContaining({
        members: [{ channel: 'sms-duty' }],
      }))
    )
  })

  it('blocks saving when an added member row has no channel', async () => {
    renderPage()
    await openEditor()
    await user.type(screen.getByPlaceholderText('ops-oncall'), 'validated')
    await user.click(screen.getByRole('button', { name: /添\s*加\s*成\s*员/ }))
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('渠道必填')).toBeInTheDocument()
    expect(apiMocks.createGroup).not.toHaveBeenCalled()
  })

  it('surfaces backend 409 on duplicate id and keeps the editor open', async () => {
    apiMocks.createGroup.mockRejectedValue({ response: { data: { message: 'group already exists: dup' } } })
    renderPage()
    await openEditor()
    await user.type(screen.getByPlaceholderText('ops-oncall'), 'dup')
    await addMemberRow('wecom')
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('group already exists: dup')).toBeInTheDocument()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('shows client-side validation errors without a request', async () => {
    renderPage()
    await openEditor()
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('请输入群组 ID')).toBeInTheDocument()
    expect(apiMocks.createGroup).not.toHaveBeenCalled()
  })

  it('falls back to the error message when the failure has no payload', async () => {
    apiMocks.updateGroup.mockRejectedValue(new Error('network gone'))
    renderPage()
    const editButtons = await screen.findAllByRole('button', { name: /编\s*辑/ })
    await user.click(editButtons[1])
    await user.click(await screen.findByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('network gone')).toBeInTheDocument()
  })

  it('labels a failure with neither payload nor message as 保存失败', async () => {
    // 拒绝值里既没有 response.data.message 也没有 message，
    // errMsg 三级 || 只剩最后一级：组件内写死的兜底文案。
    apiMocks.createGroup.mockRejectedValue({})
    renderPage()
    await openEditor()
    await user.type(screen.getByPlaceholderText('ops-oncall'), 'no-msg')
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    expect(await screen.findByText('保存失败')).toBeInTheDocument()
  })

  it('keeps a group with an empty roster and saves it back unchanged', async () => {
    // 省略 members 的群组：表格成员列、编辑回填、保存 payload 三处
    // 的 `|| []` 都要落到空数组而不是 undefined。
    apiMocks.getGroups.mockResolvedValue({ data: { groups: [{ id: 'empty-roster' }] } })
    renderPage()
    expect(await screen.findByText('empty-roster')).toBeInTheDocument()

    const editButtons = await screen.findAllByRole('button', { name: /编\s*辑/ })
    await user.click(editButtons[0])
    await screen.findByText('编辑群组 empty-roster')
    await user.click(screen.getByRole('button', { name: /^保\s*存$/ }))
    await waitFor(() =>
      expect(apiMocks.updateGroup).toHaveBeenCalledWith('empty-roster', {
        id: 'empty-roster',
        description: undefined,
        members: [],
      })
    )
  })

  it('deletes a group after confirmation', async () => {
    renderPage()
    const deleteButtons = await screen.findAllByRole('button', { name: /删\s*除/ })
    await user.click(deleteButtons[1])
    await user.click(await screen.findByRole('button', { name: /^确\s*认$/ }))
    await waitFor(() => expect(apiMocks.deleteGroup).toHaveBeenCalledWith('minimal'))
    expect(await screen.findByText('群组已删除')).toBeInTheDocument()
    await waitFor(() => expect(apiMocks.getGroups).toHaveBeenCalledTimes(2))
  })

  it('reports delete failures', async () => {
    apiMocks.deleteGroup.mockRejectedValue({ response: { data: { message: 'still referenced' } } })
    renderPage()
    const deleteButtons = await screen.findAllByRole('button', { name: /删\s*除/ })
    await user.click(deleteButtons[0])
    await user.click(await screen.findByRole('button', { name: /^确\s*认$/ }))
    expect(await screen.findByText('still referenced')).toBeInTheDocument()
  })

  it('closes the editor without saving on cancel', async () => {
    renderPage()
    await openEditor()
    await user.click(screen.getByRole('button', { name: /取\s*消/ }))
    // jsdom 里 rc-motion 的关闭动画不收敛，DOM 断言不可靠；
    // 行为断言：取消不发出任何请求。
    await waitFor(() => expect(apiMocks.createGroup).not.toHaveBeenCalled())
  })
})
