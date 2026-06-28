import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const port = parseInt(env.VITE_PORT || '80', 10)

  return {
    plugins: [vue()],
    server: {
      host: '0.0.0.0',
      port,
      proxy: {
        '/api': {
          target: (env.VITE_API_URL && env.VITE_API_URL.startsWith('http')) ? env.VITE_API_URL : 'http://localhost:8081',
          changeOrigin: true
        }
      }
    }
  }
})
