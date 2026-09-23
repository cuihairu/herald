import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    rollupOptions: {
      output: {
        // Vite 8 (rolldown) only accepts manualChunks as a function; the
        // object form now fails the build with "manualChunks is not a
        // function".
        manualChunks(id) {
          if (!id.includes('node_modules')) {
            return undefined
          }
          const rest = id.slice(id.lastIndexOf('node_modules/') + 'node_modules/'.length)
          const name = rest.startsWith('@')
            ? rest.split('/').slice(0, 2).join('/')
            : rest.split('/')[0]
          if (name === 'antd' || name === '@ant-design/icons') {
            return 'antd'
          }
          if (['react', 'react-dom', 'react-router', 'react-router-dom', 'scheduler'].includes(name)) {
            return 'react'
          }
          return undefined
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
