import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const proxy = { '/api': { target: 'http://127.0.0.1:8082', changeOrigin: false } }
export default defineConfig({
  plugins: [vue()],
  server: {
    proxy,
    strictPort: true,
    // Files on this external volume can miss native filesystem change events.
    watch: { usePolling: true, interval: 300, ignored: ['**/backend/**', '**/game-api/**', '**/dist/**'] },
    fs: { deny: ['.env', '.env.*', '*.{crt,pem}', '**/.git/**', '**/data/**', '**/*.db', '**/*bootstrap*', '**/backend/bin/**', '**/game-api/bin/**'] },
  },
  preview: { proxy },
})
