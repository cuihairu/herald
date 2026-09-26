import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    // antd 的模块图在 vite dev-server 下首次转换要数秒，默认 5s 会误伤。
    // 组件用例要真开 Modal/Popconfirm 并逐步 user-event，单例在空载下就
    // 接近 10s；共用构建机被别的项目占满时（load 30+）实测会慢到 2 倍以上，
    // 20s 偶发误报超时。门禁是覆盖阈值不是快慢，45s 换掉这类负载抖动，
    // 真实挂死仍会照样超时。
    testTimeout: 45000,
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/test/**'],
      // 行覆盖 100% 是验收线：任何一行没测到都直接失败。
      thresholds: { lines: 100 },
    },
  },
})
