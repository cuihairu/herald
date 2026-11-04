import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

type Listener = (e: { matches: boolean }) => void

// 可控的 matchMedia 桩：记录 change 监听器，测试里手动触发系统偏好切换。
let darkListeners: Listener[]
let systemDark: boolean

function installMatchMedia() {
  darkListeners = []
  window.matchMedia = ((query: string) => ({
    matches: query.includes('dark') ? systemDark : false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: (_: string, fn: Listener) => darkListeners.push(fn),
    removeEventListener: (_: string, fn: Listener) => {
      darkListeners = darkListeners.filter((l) => l !== fn)
    },
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
}

function flipSystemPreference(dark: boolean) {
  systemDark = dark
  for (const fn of [...darkListeners]) fn({ matches: dark })
}

vi.mock('./themes', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./themes')>()
  return { ...actual }
})

import ThemeModeProvider, { useThemeMode } from './ThemeModeProvider'
import { THEME_STORAGE_KEY, themeConfigs } from './themes'

function Probe() {
  const { mode, resolved } = useThemeMode()
  return (
    <div>
      <span data-testid="mode">{mode}</span>
      <span data-testid="resolved">{resolved}</span>
      <span data-testid="bg">{themeConfigs[resolved].token?.colorBgBase}</span>
    </div>
  )
}

describe('ThemeModeProvider', () => {
  const originalMatchMedia = window.matchMedia

  beforeEach(() => {
    localStorage.clear()
    systemDark = false
    installMatchMedia()
  })

  afterEach(() => {
    window.matchMedia = originalMatchMedia
    document.body.style.background = ''
  })

  it('follows the system preference by default and re-resolves live', async () => {
    render(
      <ThemeModeProvider>
        <Probe />
      </ThemeModeProvider>
    )
    expect(screen.getByTestId('mode').textContent).toBe('system')
    expect(screen.getByTestId('resolved').textContent).toBe('light')

    await act(async () => flipSystemPreference(true))
    expect(screen.getByTestId('resolved').textContent).toBe('dark')
    // 深色下不留白：body 背景同步到主题底色。
    expect(document.body.style.background).toBe('rgb(20, 20, 20)')
    expect(screen.getByTestId('bg').textContent).toBe('#141414')
  })

  it('unregisters the media listener on unmount', () => {
    const { unmount } = render(
      <ThemeModeProvider>
        <Probe />
      </ThemeModeProvider>
    )
    expect(darkListeners).toHaveLength(1)
    unmount()
    cleanup()
    expect(darkListeners).toHaveLength(0)
  })

  it('persists a manual pick and stops following the system', async () => {
    const user = userEvent.setup()
    const { rerender } = render(
      <ThemeModeProvider>
        <Switcher />
      </ThemeModeProvider>
    )
    await user.click(screen.getByRole('button', { name: '午夜' }))
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('midnight')
    expect(screen.getByTestId('resolved').textContent).toBe('midnight')

    // 同一 mode 值重复设置也要能写入（branch 覆盖）。
    await user.click(screen.getByRole('button', { name: '午夜' }))

    rerender(
      <ThemeModeProvider>
        <Switcher />
      </ThemeModeProvider>
    )
    // 手动选择优先于系统偏好。
    await act(async () => flipSystemPreference(true))
    expect(screen.getByTestId('resolved').textContent).toBe('midnight')
  })

  it('reads the persisted mode on mount', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'dark')
    render(
      <ThemeModeProvider>
        <Probe />
      </ThemeModeProvider>
    )
    expect(screen.getByTestId('mode').textContent).toBe('dark')
    expect(screen.getByTestId('resolved').textContent).toBe('dark')
  })

  it('keeps rendering when matchMedia is unavailable', () => {
    window.matchMedia = undefined as unknown as typeof window.matchMedia
    render(
      <ThemeModeProvider>
        <Probe />
      </ThemeModeProvider>
    )
    expect(screen.getByTestId('resolved').textContent).toBe('light')
  })
})

function Switcher() {
  const { mode, resolved, setMode } = useThemeMode()
  return (
    <div>
      <span data-testid="mode">{mode}</span>
      <span data-testid="resolved">{resolved}</span>
      {(['light', 'dark', 'midnight'] as const).map((m) => (
        <button key={m} onClick={() => setMode(m)}>
          {m === 'light' ? '亮色' : m === 'dark' ? '暗色' : '午夜'}
        </button>
      ))}
    </div>
  )
}
