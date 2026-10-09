import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    rolldownOptions: {
      output: {
        // Vendor layering for the antd stack: one antd chunk hit the 500 kB
        // warning (~923 kB), so split it along its real dependency layers —
        // icons (stable) / rc-* implementation packages / antd components.
        // Higher priority wins on overlap: @ant-design/icons also matches the
        // antd group's test, so it must rank above it. Modules no group
        // captures (dayjs, axios, zustand) keep automatic chunking.
        codeSplitting: {
          groups: [
            {
              name: 'react',
              test: /node_modules[\\/](react|react-dom|react-router|react-router-dom|scheduler)[\\/]/,
              priority: 4,
            },
            {
              name: 'antd-icons',
              test: /node_modules[\\/]@ant-design[\\/](icons|icons-svg)[\\/]/,
              priority: 3,
            },
            {
              name: 'antd-rc',
              test: /node_modules[\\/](rc-[a-z-]+|@rc-component[\\/][a-z-]+)[\\/]/,
              priority: 2,
            },
            {
              name: 'antd-cssinjs',
              test: /node_modules[\\/]@ant-design[\\/](cssinjs|cssinjs-utils|colors|fast-color)[\\/]/,
              priority: 2,
            },
            {
              name: 'antd',
              test: /node_modules[\\/](antd|@ant-design)[\\/]/,
              priority: 1,
            },
          ],
        },
      },
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true
      },
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true
      }
    }
  }
})
