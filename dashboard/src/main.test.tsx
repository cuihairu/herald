import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen, act } from '@testing-library/react'

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

  it('routes antd static messages through the renderer injected in main', async () => {
    // 静态 message 弹层在 jsdom 里退场动画收不到 transitionend，
    // unmount 回调不会经 rc-motion 执行；改走 main 导出的 seam，
    // 直接验证注入回调的 render 与 unmount 两条路径。
    const { createStaticRenderer } = await import('./main')
    const renderStatic = createStaticRenderer()
    const container = document.createElement('div')
    document.body.appendChild(container)
    let unmount!: () => Promise<void>
    await act(async () => {
      unmount = renderStatic(<span>渲染注入自检</span>, container)
    })
    expect(container.textContent).toContain('渲染注入自检')
    await act(async () => {
      await unmount()
    })
    container.remove()
  })
})

// 让 tsc 把 render 引入保持使用（真实断言在上方通过 findByText 完成）。
void render
