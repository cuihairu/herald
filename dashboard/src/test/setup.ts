import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'
import { unstableSetRender } from 'antd'
import { createRoot } from 'react-dom/client'

// vitest 未开 globals 时 RTL 的 auto-cleanup 不注册，DOM 会跨用例残留。
// antd message 的弹层挂在 body 的 portal 里，RTL cleanup 管不到——只清内容、
// 不能删容器：holder 是 antd 全局单例，删掉后下一用例会往 detached 容器渲染，
// 弹层永远进不了 document（表现为 findByText 超时）。
afterEach(() => {
  cleanup()
  document.querySelectorAll('.ant-message').forEach(el => {
    el.innerHTML = ''
  })
})

// 与 main.tsx 相同：React 19 下 antd 静态方法需要注入 createRoot，
// 否则 message.success/error 静默不弹（每个测试文件的模块图是独立的）。
unstableSetRender((node, container) => {
  const root = createRoot(container)
  root.render(node)
  return async () => {
    root.unmount()
  }
})

// jsdom 不实现 matchMedia / ResizeObserver / scrollTo，
// 而 antd 的响应式组件（Menu、Table、Grid）依赖它们。
if (!window.matchMedia) {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
}

class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (!window.ResizeObserver) {
  window.ResizeObserver = ResizeObserverMock as unknown as typeof ResizeObserver
}

window.scrollTo = () => {}

// rc-motion（message/notification 的动画层）依赖 rAF 驱动挂载帧；
// jsdom 默认不实现，portal 就永远挂不出来。
if (!window.requestAnimationFrame) {
  window.requestAnimationFrame = ((cb: FrameRequestCallback) =>
    setTimeout(() => cb(Date.now()), 16) as unknown as number) as typeof window.requestAnimationFrame
  window.cancelAnimationFrame = ((id: number) =>
    clearTimeout(id as unknown as ReturnType<typeof setTimeout>)) as typeof window.cancelAnimationFrame
}
