import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { ConfigProvider, theme, unstableSetRender } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import App from './App'
import './App.css'

// React 19 移除了 ReactDOM.render，antd v5 的静态方法（message 等）
// 需要显式注入 createRoot，否则 message.success/error 静默不弹。
// 抽成可导出工厂（seam）：测试可以不经 rc-motion 动画直接验证 render/unmount。
export function createStaticRenderer() {
  return (node: React.ReactNode, container: Element | DocumentFragment) => {
    const root = ReactDOM.createRoot(container)
    root.render(node)
    return async () => {
      root.unmount()
    }
  }
}

unstableSetRender(createStaticRenderer())

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: theme.darkAlgorithm,
        token: { colorPrimary: '#3b82f6' },
      }}
    >
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </ConfigProvider>
  </React.StrictMode>
)
