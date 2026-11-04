import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import ThemeModeProvider from './theme/ThemeModeProvider'
import App from './App'
import './App.css'

// antd v6 起静态方法（message 等）原生用 createRoot 渲染（React 19 安全），
// v5 时代的 unstableSetRender 注入接缝已移除。
ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ThemeModeProvider>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </ThemeModeProvider>
  </React.StrictMode>
)
