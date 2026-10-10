import { describe, it, expect, beforeEach } from 'vitest'
import { screen, act } from '@testing-library/react'
import { message } from 'antd'

// 真实挂载入口：不 mock react-dom/client，让 createRoot 真跑一遍，
// 断言应用确实渲染进 #root。路由 '/' 未登录时落在 LoginPage。
describe('main entry', () => {
  beforeEach(() => {
    localStorage.clear()
    document.body.innerHTML = '<div id="root"></div>'
  })

  it('mounts App into #root', async () => {
    // createRoot().render() 的并发渲染工作要用 act 冲刷后才会提交到 DOM。
    await act(async () => {
      await import('./main')
    })
    expect(await screen.findByText('请登录以继续')).toBeInTheDocument()
  })

  it('renders antd static messages natively under antd 6', async () => {
    // v5 时代靠 unstableSetRender 注入接缝保证 message 静态弹层可达；
    // v6 原生 createRoot 已覆盖该路径，这里端到端验证弹层真的渲染进
    // body 级 holder（setup.ts 的 @rc-component/util mock 负责 act 冲刷）。
    await act(async () => {
      message.success('静态弹层自检')
    })
    expect(await screen.findByText('静态弹层自检')).toBeInTheDocument()
  })
})
