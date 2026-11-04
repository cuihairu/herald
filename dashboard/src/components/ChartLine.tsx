import { useEffect, useRef } from 'react'
import * as echarts from 'echarts/core'
import { LineChart, BarChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'

// 按需注册：只引折线/柱状与网格/提示/图例，控制 bundle 体积。
echarts.use([LineChart, BarChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer])

// 主题色全部由调用方在 option 里给（来自 chartPalette），组件本身不做配色。
export default function ChartLine({ option }: { option: Record<string, unknown> }) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const chart = echarts.init(ref.current!)
    chart.setOption(option)
    return () => chart.dispose()
  }, [option])

  return <div ref={ref} style={{ width: '100%', height: 220 }} />
}
