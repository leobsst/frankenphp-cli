import { createRouter, createWebHashHistory } from 'vue-router'
import { NAV, SETTINGS } from '@app/nav'

/** Les pages du dashboard (web/app), en hash : le daemon sert une seule page, index.html. */
export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    ...NAV.map(({ name, path, label, component }) => ({ name, path, component, meta: { label } })),
    { name: SETTINGS.name, path: SETTINGS.path, component: () => import('@app/pages/SettingsPage.vue'), meta: { label: SETTINGS.label } },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})
