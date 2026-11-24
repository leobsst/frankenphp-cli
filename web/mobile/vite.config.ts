import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'
import { uiTheme } from '../app/src/theme'
// @ts-expect-error Node script without type declarations
import { appVersion } from './scripts/app-version.mjs'

const { version, build } = appVersion()

/**
 * Mobile app (Capacitor). The pages, components and store come from web/app (@app alias), shared with the embedded
 * dashboard; this package only adds the instance list, pairing, the native shell (tab bar, splash screen, secure
 * storage) and its own router.
 *
 * Same Nuxt UI settings as the embedded dashboard (mockup colours, bundled Lucide icons): the app makes no network
 * call besides the FrankenManager instances.
 */
export default defineConfig({
  plugins: [
    vue(),
    ui({
      ui: uiTheme,
      icon: { clientBundle: { scan: true } },
    }),
  ],
  resolve: {
    alias: { '@app': fileURLToPath(new URL('../app/src', import.meta.url)) },
    dedupe: ['vue', 'vue-router', '@nuxt/ui'],
  },
  // Version shown in the app: the same as the native projects' (scripts/app-version.mjs).
  define: { __APP_VERSION__: JSON.stringify(version), __APP_BUILD__: JSON.stringify(build) },
  // Relative paths: the Capacitor WebView serves dist/ from its own origin.
  base: './',
  build: { emptyOutDir: true },
  server: { port: 5174, host: 'localhost' },
})
