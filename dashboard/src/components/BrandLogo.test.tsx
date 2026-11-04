import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import BrandLogo from './BrandLogo'

describe('BrandLogo', () => {
  it('renders the repository logo asset unchanged', () => {
    const { container, unmount } = render(<BrandLogo />)
    const img = container.querySelector('img')!
    expect(img.getAttribute('src')).toBe('/logo.svg')
    expect(img.getAttribute('alt')).toBe('Herald logo')
    unmount()
  })

  it('honours a custom size', () => {
    const { container, unmount } = render(<BrandLogo size={40} />)
    const img = container.querySelector('img')!
    expect(img.getAttribute('width')).toBe('40')
    expect(img.getAttribute('height')).toBe('40')
    unmount()
  })
})
