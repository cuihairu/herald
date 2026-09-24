import { useHeraldStore } from '../stores/herald'

// zustand store 是模块级单例，测试间必须重置回干净快照，
// 否则前一个用例留下的 status/loading 会串到下一个用例。
const initial = useHeraldStore.getState()

export function resetStore() {
  useHeraldStore.setState(initial, true)
}
