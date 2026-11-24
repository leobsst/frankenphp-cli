import { createRouter, createWebHashHistory } from 'vue-router'
import { NAV, SETTINGS } from '@app/nav'
import { currentInstance } from './lib/instances'
import { PAIR_PATH } from './lib/pairing'

/** Instance list: the app's home when no instance is open, and the target of « Changer d’instance ». */
export const INSTANCES = { name: 'instances', path: '/instances' } as const

export const router = createRouter({
  // Hash: the WebView serves index.html from a local scheme, without URL rewriting.
  history: createWebHashHistory(),
  routes: [
    { name: INSTANCES.name, path: INSTANCES.path, component: () => import('./pages/InstancesPage.vue') },
    { path: '/instances/ajouter', component: () => import('./pages/InstanceForm.vue') },
    { path: '/instances/:id/connexion', component: () => import('./pages/InstanceForm.vue'), props: true },
    { path: PAIR_PATH, component: () => import('./pages/PairPage.vue') },
    {
      // The dashboard pages of web/app, on the instance chosen in the list.
      path: '/',
      component: () => import('./DashboardLayout.vue'),
      meta: { dashboard: true },
      children: [
        ...NAV.map(({ name, path, labelKey, component }) => ({ name, path: path.slice(1), component, meta: { labelKey, dashboard: true } })),
        { name: SETTINGS.name, path: SETTINGS.path.slice(1), component: () => import('@app/pages/SettingsPage.vue'), meta: { labelKey: SETTINGS.labelKey, dashboard: true } },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
  scrollBehavior: () => ({ top: 0 }),
})

// The dashboard drives the chosen instance: without one (or with an expired session), back to the list.
router.beforeEach((to) => {
  if (!to.meta.dashboard) return
  const inst = currentInstance()
  if (!inst) return INSTANCES.path
  if (inst.expired) return `/instances/${inst.id}/connexion`
})
