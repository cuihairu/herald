import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, cleanup } from '@testing-library/react'

const setOption = vi.fn()
const dispose = vi.fn()
const resize = vi.fn()
const init = vi.fn((_el: HTMLElement) => ({ setOption, dispose, resize }))

vi.mock('echarts/core', () => ({
  use: vi.fn(),
  init: (el: HTMLElement) => init(el),
}))
vi.mock('echarts/charts', () => ({ LineChart: {}, BarChart: {} }))
vi.mock('echarts/components', () => ({
  GridComponent: {},
  TooltipComponent: {},
  LegendComponent: {},
}))
vi.mock('echarts/renderers', () => ({ CanvasRenderer: {} }))

import ChartLine from './ChartLine'

describe('ChartLine', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('mounts a chart and applies the option', () => {
    const option = { xAxis: { data: ['a'] }, series: [] }
    const { container, unmount } = render(<ChartLine option={option} />)
    const el = container.querySelector('div')!
    expect(el).toBeInTheDocument()
    expect(init).toHaveBeenCalledWith(el)
    expect(setOption).toHaveBeenCalledWith(option)
    unmount()
    cleanup()
    expect(dispose).toHaveBeenCalled()
  })

  it('re-applies the option when it changes', () => {
    const first = { series: [1] }
    const second = { series: [2] }
    const { rerender, unmount } = render(<ChartLine option={first} />)
    rerender(<ChartLine option={second} />)
    expect(setOption).toHaveBeenLastCalledWith(second)
    unmount()
    cleanup()
  })

  it('resizes the chart when the window resizes', () => {
    const { unmount } = render(<ChartLine option={{ series: [] }} />)
    window.dispatchEvent(new Event('resize'))
    expect(resize).toHaveBeenCalled()
    unmount()
    cleanup()
  })
})
