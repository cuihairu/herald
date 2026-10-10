import '@testing-library/jest-dom/vitest'
import { afterAll, afterEach } from 'vitest'
import { act, cleanup, configure } from '@testing-library/react'

// RTL 的 waitFor/findBy 默认 1s 上限在负载下会批量击穿：jsdom 里 rc-motion
// 的 rAF 兜底本身就是 setTimeout(16) 宏任务链，与 Go -race 全量套件并行或
// CI 慢 runner 时排队被拉长（2026-09-27 实证一次 9 个交互用例连挂、复跑即
// 全绿）。放宽到 3s 只抬上限——waitFor 一满足立即返回，快路径不受影响。
configure({ asyncUtilTimeout: 3000 })

// 交还环境前把还排着的工作跑干净。React scheduler 用 processImmediate
// （宏任务）冲刷并发工作，rc-motion 的退场帧又走 rAF——这里的 rAF 兜底是
// setTimeout(16)。任何一项漏到环境销毁之后才触发，就会在 window 已经不存在
// 的情况下进 React 渲染，抛 unhandled "window is not defined"：行覆盖仍然
// 100%，vitest 却以 exit code 1 失败（覆盖门禁抓不到这种泄漏）。
//
// 只冲刷固定两轮不够：antd 6 + React 19.2 下，带活动过渡的提交会经 scheduler
// 排一个 NormalPriority 回调冲刷 passive effects（react-dom-client:17920 的
// `window.event` 取值），而 rc-motion 退场帧每 ~16ms 一帧、一帧一提交，链条
// 长度≈退场时长（~300ms≈19 帧）。慢机（CI）上提交 actualDuration≠0 必然走到
// 这条路径，两轮只覆盖 ~4 帧，余下帧漏到环境销毁后触发 → exit 1。本地快机
// actualDuration===0 走不到，故无法本地复现（2026-10-10 实证 CI 挂、本地绿）。
// 这里循环冲刷直到文档里没有 rc-motion 活动态类名（退场链收敛），至少 4 轮
// 兜底非 motion 的短链，30 轮（~600ms）上限防真死循环；静默用例首轮即收工。
// 活动态选择器只命中 *-appear/enter/leave-active|start|prepare|end 这类进行中
// 的状态后缀（CSSMotion 在 STATUS_NONE 静止态会整段移除 motionName 类），故
// 文档无活动态类名即代表所有退场链已收敛、不会再派生新提交。
async function flushPendingWork() {
  const ACTIVE_MOTION =
    '[class*="-appear-active"],[class*="-appear-start"],[class*="-appear-prepare"],' +
    '[class*="-enter-active"],[class*="-enter-start"],[class*="-enter-prepare"],' +
    '[class*="-leave-active"],[class*="-leave-start"],[class*="-leave-prepare"],[class*="-leave-end"]'
  for (let i = 0; i < 30; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20))
    })
    if (i >= 3 && !document.querySelector(ACTIVE_MOTION)) {
      return
    }
  }
}

// vitest 未开 globals 时 RTL 的 auto-cleanup 不注册，DOM 会跨用例残留。
// antd message 的弹层挂在 body 的 portal 里，RTL cleanup 管不到。v6 的
// 关闭链（destroy 与 duration 自动消失都一样）在 jsdom+act 环境下静默
// 失效——弹层挂上就成僵尸（开路径正常，close 的 setState 永不落地，
// 2026-10-10 实证 4.5s 后仍不退场）。v5 时代的 innerHTML 整段清空会把
// React 托管的节点从脚下抽走、毒化 holder 单例；这里只抹掉僵尸弹层的
// 文本内容（close 已死，React 不会再渲染进它们），上一用例的提示文本
// 就不会让下一用例的 findByText 撞出 multiple elements。
afterEach(async () => {
  cleanup()
  document.querySelectorAll('.ant-message-notice').forEach((el) => {
    el.textContent = ''
  })
  await flushPendingWork()
})

// antd 静态 message 的 holder 是独立 React root：@rc-component/util 的 render
// 把 root 存在 container['__rc_react_root__'] 上，container 是一个
// DocumentFragment（模块单例、从不暴露），RTL cleanup 只 unmount render()
// 创建的 root，这个 root 永远活着。活着就不够——notice 挂载后
// useNoticeTimer 启动 4.5s 进度条 rAF 步进链，每帧一次 setPercent setState；
// 唯一能断链的地方是 effect cleanup 里的 raf.cancel，而 cleanup 只在
// unmount 时跑。文件结束后步进链仍在排下一帧，vitest 销毁环境先删 window
// 全局再关 window，夹缝里触发的一帧让 react-dom 一碰 window 就抛
// unhandled "window is not defined"：183/183 全绿、覆盖率 100% 仍 exit 1
//（2026-10-10 CI 三连实证，本地快机时序不同无法复现）。
// 所以这里在文件收尾时对全部 DocumentFragment 记账，把带 rc root 标记的
// 逐个真 unmount（act 内跑完卸载提交），再整体冲刷一遍余波。
// 只在 afterAll 做：afterEach 就 unmount 会杀掉模块单例 holder，后续用例的
// message 会渲染进已卸载的 root，断言文本全部落空。
const rcRootContainers: DocumentFragment[] = []
const originalCreateFragment = document.createDocumentFragment.bind(document)
document.createDocumentFragment = ((...args: Parameters<Document['createDocumentFragment']>) => {
  const fragment = originalCreateFragment(...args)
  rcRootContainers.push(fragment)
  return fragment
}) as typeof document.createDocumentFragment

async function unmountTrackedRoots() {
  const RC_ROOT_MARK = '__rc_react_root__'
  for (const container of rcRootContainers) {
    const root = (container as unknown as Record<string, { unmount(): void } | undefined>)[RC_ROOT_MARK]
    if (root) {
      await act(async () => {
        root.unmount()
      })
    }
  }
  rcRootContainers.length = 0
}

afterAll(async () => {
  await flushPendingWork()
  await unmountTrackedRoots()
  await flushPendingWork()
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
