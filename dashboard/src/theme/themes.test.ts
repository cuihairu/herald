import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  THEME_STORAGE_KEY,
  loadThemeMode,
  resolveTheme,
  saveThemeMode,
  systemPrefersDark,
  themeConfigs,
  type ThemeMode,
} from './themes'

describe('themes', () => {
  describe('resolveTheme', () => {
    it('follows the system preference in system mode', () => {
      expect(resolveTheme('system', true)).toBe('dark')
      expect(resolveTheme('system', false)).toBe('light')
    })

    it('returns manual modes as-is', () => {
      expect(resolveTheme('light', true)).toBe('light')
      expect(resolveTheme('dark', false)).toBe('dark')
      expect(resolveTheme('midnight', false)).toBe('midnight')
    })
  })

  describe('systemPrefersDark', () => {
    const original = window.matchMedia

    afterEach(() => {
      window.matchMedia = original
    })

    it('reads the prefers-color-scheme media query', () => {
      window.matchMedia = ((query: string) => ({
        matches: query.includes('dark'),
        media: query,
        onchange: null,
        addListener: () => {},
        removeListener: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        dispatchEvent: () => false,
      })) as unknown as typeof window.matchMedia
      expect(systemPrefersDark()).toBe(true)
    })

    it('reports light when nothing prefers dark', () => {
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
      expect(systemPrefersDark()).toBe(false)
    })

    it('falls back to light without matchMedia support', () => {
      window.matchMedia = undefined as unknown as typeof window.matchMedia
      expect(systemPrefersDark()).toBe(false)
    })
  })

  describe('storage', () => {
    beforeEach(() => {
      localStorage.clear()
    })

    it.each<ThemeMode>(['system', 'light', 'dark', 'midnight'])(
      'round-trips mode %s',
      (mode) => {
        saveThemeMode(mode)
        expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe(mode)
        expect(loadThemeMode()).toBe(mode)
      }
    )

    it('defaults to system when nothing is stored', () => {
      expect(loadThemeMode()).toBe('system')
    })

    it('falls back to system on an unknown stored value', () => {
      localStorage.setItem(THEME_STORAGE_KEY, 'neon')
      expect(loadThemeMode()).toBe('system')
    })

    it('falls back to system when localStorage throws', () => {
      vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
        throw new Error('denied')
      })
      expect(loadThemeMode()).toBe('system')
      vi.restoreAllMocks()
    })

    it('swallows write failures', () => {
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new Error('denied')
      })
      expect(() => saveThemeMode('dark')).not.toThrow()
      vi.restoreAllMocks()
    })
  })

  it('ships one full token set per resolved theme', () => {
    for (const resolved of ['light', 'dark', 'midnight'] as const) {
      const config = themeConfigs[resolved]
      expect(config.token?.colorPrimary).toBeTruthy()
      expect(config.token?.colorBgBase).toBeTruthy()
      expect(config.algorithm).toBeTruthy()
    }
    // Midnight is a separate palette, not an alias of dark.
    expect(themeConfigs.midnight.token?.colorBgBase).not.toBe(
      themeConfigs.dark.token?.colorBgBase
    )
  })
})
