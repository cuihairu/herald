import { theme } from 'antd'
import type { ThemeConfig } from 'antd'

// 用户可选择的模式。"system" 跟随系统深浅色偏好，其余为手动指定。
export type ThemeMode = 'system' | 'light' | 'dark' | 'midnight'

// 实际生效的主题（system 解析后落到的两套深色之一或亮色）。
export type ResolvedTheme = 'light' | 'dark' | 'midnight'

export const THEME_STORAGE_KEY = 'herald_theme'

export const THEME_MODES: ThemeMode[] = ['system', 'light', 'dark', 'midnight']

const VALID_MODES: ThemeMode[] = THEME_MODES

function primaryFor(mode: ResolvedTheme): string {
  // 克制的单一主色：亮色用深一档蓝，深色用亮一档保证对比；午夜用淡青蓝
  // 与暗色主题拉开识别度。
  if (mode === 'light') return '#2563eb'
  if (mode === 'dark') return '#4d8bff'
  return '#7fb5e8'
}

// 每套主题一份完整 token：页面里的颜色一律取这里或 useToken()，
// 组件内不允许出现字面色值。
export const themeConfigs: Record<ResolvedTheme, ThemeConfig> = {
  light: {
    algorithm: theme.defaultAlgorithm,
    token: {
      colorPrimary: primaryFor('light'),
      colorBgBase: '#f6f7f9',
      colorBgContainer: '#ffffff',
      colorBgLayout: '#f6f7f9',
      colorTextBase: '#1f2329',
      borderRadius: 6,
    },
    components: {
      Layout: {
        headerBg: '#ffffff',
        siderBg: '#ffffff',
      },
      Menu: { itemBg: '#ffffff' },
    },
  },
  dark: {
    algorithm: theme.darkAlgorithm,
    token: {
      colorPrimary: primaryFor('dark'),
      colorBgBase: '#141414',
      colorBgContainer: '#1d1d1d',
      colorBgLayout: '#101010',
      colorTextBase: '#e6e6e6',
      borderRadius: 6,
    },
    components: {
      Layout: {
        headerBg: '#1d1d1d',
        siderBg: '#1d1d1d',
      },
      Menu: { itemBg: '#1d1d1d' },
    },
  },
  midnight: {
    algorithm: theme.darkAlgorithm,
    token: {
      colorPrimary: primaryFor('midnight'),
      colorBgBase: '#0b1220',
      colorBgContainer: '#121b2e',
      colorBgLayout: '#0b1220',
      // 高对比正文：接近纯白，避免暗底低对比灰字。
      colorTextBase: '#eef2fa',
      colorBorder: '#2c3a55',
      colorBorderSecondary: '#1e2a44',
      borderRadius: 6,
    },
    components: {
      Layout: {
        headerBg: '#121b2e',
        siderBg: '#121b2e',
      },
      Menu: { itemBg: '#121b2e' },
    },
  },
}

// 图表用的派生色：曲线、堆叠与轴文字颜色全部从主题走，三套主题各自成套。
export const chartPalette: Record<
  ResolvedTheme,
  {
    success: string
    failed: string
    line: string
    textSecondary: string
    splitLine: string
    tooltipBg: string
  }
> = {
  light: {
    success: '#16a34a',
    failed: '#dc2626',
    line: primaryFor('light'),
    textSecondary: '#6b7280',
    splitLine: '#e5e7eb',
    tooltipBg: '#ffffff',
  },
  dark: {
    success: '#4ade80',
    failed: '#f87171',
    line: primaryFor('dark'),
    textSecondary: '#9ca3af',
    splitLine: '#333333',
    tooltipBg: '#1d1d1d',
  },
  midnight: {
    success: '#5eead4',
    failed: '#f0a3a3',
    line: primaryFor('midnight'),
    textSecondary: '#8fa1c0',
    splitLine: '#1e2a44',
    tooltipBg: '#121b2e',
  },
}

export function resolveTheme(mode: ThemeMode, systemDark: boolean): ResolvedTheme {
  if (mode === 'system') return systemDark ? 'dark' : 'light'
  return mode
}

export function systemPrefersDark(): boolean {
  if (!window.matchMedia) return false
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

export function loadThemeMode(): ThemeMode {
  try {
    const stored = localStorage.getItem(THEME_STORAGE_KEY)
    if (stored && (VALID_MODES as string[]).includes(stored)) {
      return stored as ThemeMode
    }
  } catch {
    // localStorage 不可用（隐私模式等）时跟随系统。
  }
  return 'system'
}

export function saveThemeMode(mode: ThemeMode): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, mode)
  } catch {
    // 写失败不阻断切换，本次会话内仍然生效。
  }
}
