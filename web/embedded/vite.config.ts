import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'

/**
 * Dashboard embarqué dans le binaire (go:embed de dist/) et servi par le daemon. Les pages, composants et le store
 * viennent de web/app (alias @app), communs avec l'app mobile.
 *
 * En développement (pnpm dev), /api et /config.json passent au daemon de la machine, sur son écouteur HTTPS de
 * 127.0.0.1 (DASHBOARD_HTTPS_PORT, 9444 par défaut), présentés comme venant de cet écouteur lui-même.
 */
const daemon = `https://127.0.0.1:${process.env.DASHBOARD_HTTPS_PORT ?? '9444'}`

export default defineConfig({
  plugins: [
    vue(),
    ui({
      ui: {
        colors: { primary: 'brand', info: 'accent', success: 'brand', warning: 'amber', error: 'red', neutral: 'sage' as 'zinc' },
        // Élément de navigation actif : fond pâle + texte primaire (maquette).
        navigationMenu: {
          compoundVariants: [{ variant: 'pill', active: true, highlight: false, class: { link: 'before:bg-primary/10 font-semibold' } }],
        },
      },
      // Icônes Lucide embarquées : aucun appel à l'API Iconify.
      icon: { clientBundle: { scan: true } },
    }),
  ],
  resolve: {
    alias: { '@app': fileURLToPath(new URL('../app/src', import.meta.url)) },
    dedupe: ['vue', 'vue-router', '@nuxt/ui'],
  },
  // Chemins relatifs : la même build sert https://frankenmanager.test, 127.0.0.1 et l'accès distant.
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
