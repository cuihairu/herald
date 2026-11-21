// 首访语言探测（L10N-001）：只对「首次到访且浏览器语言为中文」的访客做一次
// 跳转，之后一律以访客自己的选择为准——不反复抢跳、不覆盖手动切换。
//
// 语义：
// - localStorage 有 herald:lang（首访时由 applyAutoLocale 写入）→ 从不跳转；
// - 首访且 navigator.languages 含 zh* → 保留当前路径跳 /zh/ 对应页；
// - 其余（非中文首访、已在 /zh/、已有记录）→ 不动。
// 因此手动切回英文后刷新/再次到访都不会被抢跳回中文。
const STORAGE_KEY = 'herald:lang'

// resolveLocaleRedirect 是纯函数：输入首访信号，返回应跳转的完整路径或
// null（不跳转）。不触碰 window/localStorage，便于单独推演与测试。
export function resolveLocaleRedirect(input: {
  pathname: string
  search: string
  hash: string
  languages: readonly string[]
  stored: string | null
  base: string
}): string | null {
  // 已有语言记录（首访落点或手动切换后刷新）——访客偏好已定，不抢跳。
  if (input.stored !== null) return null

  // 只对中文浏览器做探测；其它语言保持默认（English 根路径）。
  const wantsZh = input.languages.some((lang) =>
    lang.toLowerCase().startsWith('zh'),
  )
  if (!wantsZh) return null

  // 已在中文站内（直达分享链接等）——无需跳转。
  const zhPrefix = input.base + 'zh/'
  if (input.pathname === zhPrefix.slice(0, -1) || input.pathname.startsWith(zhPrefix)) return null

  // 保留完整路径与查询/锚点，跳到中文对应页；404 页也会照跳（中英页面
  // 经审计对齐，缺页属边缘，VitePress 404 页自带语言切换出口）。
  return zhPrefix + input.pathname.slice(input.base.length) + input.search + input.hash
}

// applyAutoLocale 在布局挂载（仅客户端）后执行一次首访判定：解析、落记录、
// 需要时跳转。记录写在跳转之前，即使跳转被打断也不会在下次到访时循环抢跳。
export function applyAutoLocale(base: string): void {
  let stored: string | null = null
  try {
    stored = window.localStorage.getItem(STORAGE_KEY)
  } catch {
    // 隐私模式等禁用存储：跳过探测（宁可少跳，不可循环跳）。
    return
  }

  const zhPrefix = base + 'zh/'
  const locale = window.location.pathname.startsWith(zhPrefix) ? 'zh' : 'en'

  const target = resolveLocaleRedirect({
    pathname: window.location.pathname,
    search: window.location.search,
    hash: window.location.hash,
    languages: window.navigator.languages,
    stored,
    base,
  })

  // 无论是否跳转都记下本次落点语言：此后的一切切换都由访客主导。
  window.localStorage.setItem(STORAGE_KEY, locale)

  if (target !== null) window.location.replace(target)
}
