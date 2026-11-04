import { createContext, useContext, useEffect, useLayoutEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import {
  loadThemeMode,
  resolveTheme,
  saveThemeMode,
  systemPrefersDark,
  themeConfigs,
  type ThemeMode,
  type ResolvedTheme,
} from './themes'

interface ThemeContextValue {
  mode: ThemeMode
  resolved: ResolvedTheme
  setMode: (mode: ThemeMode) => void
}

const defaultContext: ThemeContextValue = {
  mode: 'system',
  resolved: 'light',
  setMode: () => {},
}

const ThemeContext = createContext<ThemeContextValue>(defaultContext)

export function useThemeMode(): ThemeContextValue {
  return useContext(ThemeContext)
}

// 主题单一入口：system 跟随 prefers-color-scheme 实时解析，手动三选一
// 存 localStorage。ConfigProvider 的 token 全部来自 themes.ts，页面代码
// 不持字面色值。
export default function ThemeModeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(() => loadThemeMode())
  const [systemDark, setSystemDark] = useState<boolean>(() => systemPrefersDark())

  useEffect(() => {
    if (!window.matchMedia) return
    const mql = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (e: MediaQueryListEvent) => setSystemDark(e.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])

  const resolved = resolveTheme(mode, systemDark)

  // antd 只接管组件树；body 底色由这里按主题同步，深色下不留白边。
  // useLayoutEffect 在浏览器绘制前写入样式，避免 system→dark 首帧白闪。
  useLayoutEffect(() => {
    document.body.style.background = themeConfigs[resolved].token!.colorBgBase!
  }, [resolved])

  const value = useMemo<ThemeContextValue>(
    () => ({
      mode,
      resolved,
      setMode: (next: ThemeMode) => {
        setModeState(next)
        saveThemeMode(next)
      },
    }),
    [mode, resolved]
  )

  return (
    <ThemeContext.Provider value={value}>
      <ConfigProvider locale={zhCN} theme={themeConfigs[resolved]}>
        {children}
      </ConfigProvider>
    </ThemeContext.Provider>
  )
}
