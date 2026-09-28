import fs from 'node:fs'
import path from 'node:path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// In development the engine runs on a fixed port and writes its ready line
// (including the API token) to .dev/engine.json. The dev server attaches the
// token to proxied requests so it never reaches the browser.
const devApiPort = Number(process.env.TK_DEV_API_PORT || 47821)
const devState = path.resolve(import.meta.dirname, '../../.dev/engine.json')

function devToken() {
  try {
    return JSON.parse(fs.readFileSync(devState, 'utf8')).token
  } catch {
    return null
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  clearScreen: false,
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/v1': {
        target: `http://127.0.0.1:${devApiPort}`,
        changeOrigin: true,
        configure(proxy) {
          proxy.on('proxyReq', (req) => {
            const token = devToken()
            if (token) req.setHeader('Authorization', `Bearer ${token}`)
          })
        },
      },
    },
  },
  build: {
    target: 'es2022',
    sourcemap: true,
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.js'],
  },
})
