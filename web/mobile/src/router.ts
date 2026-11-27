import { createRouter, createWebHashHistory } from 'vue-router'
import { NAV, SETTINGS } from '@app/nav'
import { ADD_INSTANCE_PATH, currentInstance, INSTANCES_PATH, signInPath } from '@app/lib/instances'
import { PAIR_PATH } from './lib/pairing'

/** Instance list: the app's home when no instance is open, and the target of « Changer d’instance ». */
export const INSTANCES = { name: 'instances', path: INSTANCES_PATH } as const

export const router = createRouter({
  // Hash: the WebView serves index.html from a local scheme, without URL rewriting.
  history: createWebHashHistory(),
  routes: [
    // The instance pages are shared with the dashboard (web/app).
    { name: INSTANCES.name, path: INSTANCES.path, component: () => import('@app/pages/InstancesPage.vue') },
    { path: ADD_INSTANCE_PATH, component: () => import('@app/pages/InstanceForm.vue') },
    { path: signInPath(':id'), component: () => import('@app/pages/InstanceForm.vue'), props: true },
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
  if (inst.expired) return signInPath(inst.id)
})
