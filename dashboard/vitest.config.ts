import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    // antd 的模块图在 vite dev-server 下首次转换要数秒，默认 5s 会误伤。
    testTimeout: 20000,
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/test/**'],
      // 行覆盖 100% 是验收线：任何一行没测到都直接失败。
      thresholds: { lines: 100 },
    },
  },
})
