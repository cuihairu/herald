import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('../api', () => ({
  heraldApi: {
    getTemplates: vi.fn(),
    createTemplate: vi.fn(),
    deleteTemplate: vi.fn(),
  },
}))

import TemplatesPage, { toTemplatePayload } from './TemplatesPage'
import { heraldApi } from '../api'

const apiMocks = vi.mocked(heraldApi)
const user = userEvent.setup()

const templates = [
  { id: 'deploy-notice', name: '部署通知', title: '部署完成', level: 'info', fields: ['app', 'env'] },
  { id: 'bare', level: '', fields: [] },
]

describe('toTemplatePayload', () => {
  it('splits fields on commas and whitespace, dropping empties', () => {
    expect(toTemplatePayload({ id: 't', level: 'info', fields: 'a, b  c,,d' })).toEqual({
      id: 't', name: undefined, title: undefined, level: 'info', fields: ['a', 'b', 'c', 'd'],
    })
    expect(toTemplatePayload({ id: 't', fields: '' }).fields).toEqual([])
  })
})

async function openCreate() {
  await user.click(await screen.findByRole('button', { name: /新\s*建\s*模\s*板/ }))
  return await screen.findByRole('dialog')
}

describe('TemplatesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.getTemplates.mockResolvedValue({ data: { templates, count: 2 } })
    apiMocks.createTemplate.mockResolvedValue({ data: { code: 0 } })
  })

  it('lists templates with level tags and fields', async () => {
    render(<TemplatesPage />)
    expect(await screen.findByText('deploy-notice')).toBeInTheDocument()
    expect(screen.getByText('app')).toBeInTheDocument()
    // 空 level 兜底为 default 标签；空字段/空名称展示 —。
    expect(screen.getByText('default')).toBeInTheDocument()
    expect(screen.getAllByText('—').length).toBeGreaterThan(0)
  })

  it('reports a load failure', async () => {
    apiMocks.getTemplates.mockRejectedValue(new Error('net down'))
    render(<TemplatesPage />)
    expect(await screen.findByText('net down')).toBeInTheDocument()
  })

  it('creates a template and refreshes the list', async () => {
    render(<TemplatesPage />)
    const dialog = await openCreate()
    const scope = within(dialog)
    await user.type(scope.getByPlaceholderText('deploy-notice'), 'new-tpl')
    await user.click(scope.getByRole('button', { name: /创\s*建/ }))
    await screen.findByText('模板已创建')
    expect(apiMocks.createTemplate).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'new-tpl', level: 'info', fields: [] }),
    )
    await waitForRefetch()
  })

  it('keeps the modal open on validation and api failures', async () => {
    render(<TemplatesPage />)
    const dialog = await openCreate()
    const scope = within(dialog)
    await user.click(scope.getByRole('button', { name: /创\s*建/ }))
    expect(await screen.findByText('请输入模板 ID')).toBeInTheDocument()
    apiMocks.createTemplate.mockRejectedValue({ response: { data: { message: 'duplicate id' } } })
    await user.type(scope.getByPlaceholderText('deploy-notice'), 'dup')
    await user.click(scope.getByRole('button', { name: /创\s*建/ }))
    expect(await screen.findByText('duplicate id')).toBeInTheDocument()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  // Popconfirm 的确认键也在弹层里；以首个行内删除键开刀，确认键取弹层主按钮。
  async function confirmDelete() {
    const rowButtons = await screen.findAllByRole('button', { name: /删\s*除/ })
    await user.click(rowButtons[0])
    const popover = await screen.findByText(/删除模板/).then((el) => el.closest('.ant-popover') as HTMLElement)
    await user.click(within(popover).getByRole('button', { name: /删\s*除/ }))
  }

  it('deletes a template after confirm and reports failures', async () => {
    apiMocks.deleteTemplate.mockResolvedValueOnce({ data: { code: 0 } })
    render(<TemplatesPage />)
    await screen.findByText('deploy-notice')
    await confirmDelete()
    await screen.findByText('模板已删除')
    expect(apiMocks.deleteTemplate).toHaveBeenCalledWith('deploy-notice')
    await waitForRefetch()

    apiMocks.deleteTemplate.mockRejectedValueOnce(new Error('still referenced'))
    await confirmDelete()
    expect(await screen.findByText('still referenced')).toBeInTheDocument()
  })
})

async function waitForRefetch() {
  await vi.waitFor(() => expect(apiMocks.getTemplates.mock.calls.length).toBeGreaterThanOrEqual(2))
}
