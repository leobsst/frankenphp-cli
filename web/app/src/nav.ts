/**
 * Dashboard pages, shared by the embedded dashboard (web/embedded) and the mobile app: each application mounts them
 * in its own router. `labelKey` is a translation key, resolved at render time (i18n.ts).
 */
export const NAV = [
  { name: 'overview', labelKey: 'nav.overview', path: '/', icon: 'i-lucide-layout-grid', component: () => import('./pages/OverviewPage.vue') },
  { name: 'projects', labelKey: 'nav.projects', path: '/projets', icon: 'i-lucide-globe', component: () => import('./pages/ProjectsPage.vue') },
  { name: 'containers', labelKey: 'nav.containers', path: '/conteneurs', icon: 'i-lucide-box', component: () => import('./pages/ContainersPage.vue') },
  { name: 'proxy', labelKey: 'nav.proxy', path: '/reverse-proxy', icon: 'i-lucide-arrow-up-right', component: () => import('./pages/ProxyPage.vue') },
  { name: 'dns', labelKey: 'nav.dns', path: '/dns', icon: 'i-lucide-globe', component: () => import('./pages/DnsPage.vue') },
  { name: 'ssl', labelKey: 'nav.ssl', path: '/ssl', icon: 'i-lucide-lock', component: () => import('./pages/SslPage.vue') },
  { name: 'redis', labelKey: 'nav.redis', path: '/redis', icon: 'i-lucide-zap', component: () => import('./pages/RedisPage.vue') },
  { name: 's3', labelKey: 'nav.s3', path: '/s3', icon: 'i-lucide-hard-drive', component: () => import('./pages/S3Page.vue') },
  { name: 'websocket', labelKey: 'nav.websocket', path: '/websocket', icon: 'i-lucide-radio', component: () => import('./pages/WebsocketPage.vue') },
  { name: 'databases', labelKey: 'nav.databases', path: '/bases-de-donnees', icon: 'i-lucide-database', component: () => import('./pages/DatabasesPage.vue') },
  { name: 'configuration', labelKey: 'nav.configuration', path: '/configuration', icon: 'i-lucide-file-cog', component: () => import('./pages/ConfigurationPage.vue') },
  { name: 'access', labelKey: 'nav.access', path: '/acces-distant', icon: 'i-lucide-shield-check', component: () => import('./pages/AccessPage.vue') },
  { name: 'logs', labelKey: 'nav.logs', path: '/journaux', icon: 'i-lucide-square-terminal', component: () => import('./pages/LogsPage.vue') },
] as const


/** Interface preferences page, kept apart in the navigation. */
export const SETTINGS = { name: 'settings', labelKey: 'nav.settings', path: '/parametres', icon: 'i-lucide-sliders-horizontal' } as const

