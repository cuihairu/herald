import '@testing-library/jest-dom/vitest'
import { afterAll, afterEach } from 'vitest'
import { act, cleanup } from '@testing-library/react'
import { unstableSetRender } from 'antd'
import { createRoot } from 'react-dom/client'

// 交还环境前把还排着的工作跑干净。React scheduler 用 processImmediate
// （宏任务）冲刷并发工作，rc-motion 的退场帧又走 rAF——这里的 rAF 兜底是
// setTimeout(16)。任何一项漏到环境销毁之后才触发，就会在 window 已经不存在
// 的情况下进 React 渲染，抛 unhandled "window is not defined"：行覆盖仍然
// 100%，vitest 却以 exit code 1 失败（覆盖门禁抓不到这种泄漏）。
async function flushPendingWork() {
  for (const delay of [0, 20]) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, delay))
    })
  }
}

// vitest 未开 globals 时 RTL 的 auto-cleanup 不注册，DOM 会跨用例残留。
// antd message 的弹层挂在 body 的 portal 里，RTL cleanup 管不到——只清内容、
// 不能删容器：holder 是 antd 全局单例，删掉后下一用例会往 detached 容器渲染，
// 弹层永远进不了 document（表现为 findByText 超时）。
afterEach(async () => {
  cleanup()
  document.querySelectorAll('.ant-message').forEach(el => {
    el.innerHTML = ''
  })
  await flushPendingWork()
})

// antd 的 message/Modal 单例 root 不属于任何用例：RTL cleanup 卸不掉它，
// 它的渲染也可能在用例结束后才被调度。光靠 afterEach 的定时排空是在赌
// 帧时机——CI 上调度慢一点，immediate 就排在 flush 之后、环境销毁之后，
// 照样抛 "window is not defined"。这里改为确定性收尾：登记注入渲染器创建
// 的每个 root，文件结束时全部卸载。unmount 会同步冲刷并作废该 root 名下
// 所有已排程工作，之后 scheduler 再无可为该 root 触发的回调。
const liveRoots = new Set<ReturnType<typeof createRoot>>()

afterAll(async () => {
  for (const root of liveRoots) {
    await act(async () => {
      root.unmount()
    })
  }
  liveRoots.clear()
  await flushPendingWork()
})

// 与 main.tsx 相同：React 19 下 antd 静态方法需要注入 createRoot，
// 否则 message.success/error 静默不弹（每个测试文件的模块图是独立的）。
// render/unmount 都包在 act 里：否则这次 createRoot 的调度工作会漏到用例
// 之外，由 scheduler 在环境销毁后才冲刷（见上方 flushPendingWork 的注释）。
unstableSetRender((node, container) => {
  const root = createRoot(container)
  liveRoots.add(root)
  act(() => {
    root.render(node)
  })
  return async () => {
    liveRoots.delete(root)
    await act(async () => {
      root.unmount()
    })
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
