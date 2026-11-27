import { createRouter, createWebHashHistory } from 'vue-router'
import { NAV, SETTINGS } from '@app/nav'
import { ADD_INSTANCE_PATH, currentInstance, INSTANCES_PATH, signInPath } from '@app/lib/instances'
// The first screen: in the main bundle, as when App.vue showed it directly.
import DashboardLayout from './DashboardLayout.vue'

/**
 * The dashboard pages (web/app), in hash mode: the daemon serves a single page, index.html. They show the instance
 * chosen in the instance list (shared with the mobile app): this machine by default.
 */
export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { name: 'instances', path: INSTANCES_PATH, component: () => import('@app/pages/InstancesPage.vue') },
    { path: ADD_INSTANCE_PATH, component: () => import('@app/pages/InstanceForm.vue') },
    { path: signInPath(':id'), component: () => import('@app/pages/InstanceForm.vue'), props: true },
    {
      path: '/',
      component: DashboardLayout,
      meta: { dashboard: true },
      children: [
        ...NAV.map(({ name, path, labelKey, component }) => ({ name, path: path.slice(1), component, meta: { labelKey, dashboard: true } })),
        { name: SETTINGS.name, path: SETTINGS.path.slice(1), component: () => import('@app/pages/SettingsPage.vue'), meta: { labelKey: SETTINGS.labelKey, dashboard: true } },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

// The dashboard drives the chosen instance: without one, back to the list; with an expired session, to its sign-in.
router.beforeEach((to) => {
  if (!to.meta.dashboard) return
  const inst = currentInstance()
  if (!inst) return INSTANCES_PATH
  if (inst.expired) return signInPath(inst.id)
})
