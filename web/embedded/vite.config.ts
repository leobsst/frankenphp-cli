import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'
import { uiIcons, uiTheme } from '../app/src/theme'
import { serviceWorker } from './sw/plugin'

/**
 * Dashboard embedded in the binary (go:embed of dist/) and served by the daemon. The pages, components and store
 * come from web/app (@app alias), shared with the mobile app.
 *
 * In development (pnpm dev), /api and /config.json are proxied to the machine's daemon, on its 127.0.0.1 HTTPS
 * listener (DASHBOARD_HTTPS_PORT, 9444 by default), presented as coming from that listener itself.
 */
const daemon = `https://127.0.0.1:${process.env.DASHBOARD_HTTPS_PORT ?? '9444'}`

export default defineConfig({
  plugins: [
    vue(),
    // Bundled Lucide icons, the shared pages and .ts files included: no calls to the Iconify API.
    ui({ ui: uiTheme, icon: uiIcons }),
    // dist/sw.js, the service worker of the installable dashboard (PWA), with the list of the built files.
    serviceWorker(),
  ],
  resolve: {
    alias: { '@app': fileURLToPath(new URL('../app/src', import.meta.url)) },
    dedupe: ['vue', 'vue-router', '@nuxt/ui'],
  },
  // Relative paths: the same build serves https://frankenmanager.test, 127.0.0.1 and remote access.
  base: './',
  build: { emptyOutDir: true },
  server: {
    port: 5175,
    host: 'localhost',
    proxy: {
      '/api': { target: daemon, secure: false, changeOrigin: true, headers: { Origin: daemon } },
      '/config.json': { target: daemon, secure: false, changeOrigin: true },
    },
  },
})
