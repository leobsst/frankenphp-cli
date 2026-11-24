import { createRouter, createWebHashHistory } from 'vue-router'
import { NAV, SETTINGS } from '@app/nav'

/** The dashboard pages (web/app), in hash mode: the daemon serves a single page, index.html. */
export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    ...NAV.map(({ name, path, label, component }) => ({ name, path, component, meta: { label } })),
    { name: SETTINGS.name, path: SETTINGS.path, component: () => import('@app/pages/SettingsPage.vue'), meta: { label: SETTINGS.label } },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})
