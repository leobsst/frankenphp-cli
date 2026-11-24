/**
 * Pages du dashboard, communes au dashboard embarqué (web/embedded) et à l'app mobile : chaque application les
 * monte dans son propre routeur.
 */
export const NAV = [
  { name: 'overview', path: '/', label: 'Vue d’ensemble', icon: 'i-lucide-layout-grid', component: () => import('./pages/OverviewPage.vue') },
  { name: 'projects', path: '/projets', label: 'Projets & domaines', icon: 'i-lucide-globe', component: () => import('./pages/ProjectsPage.vue') },
  { name: 'containers', path: '/conteneurs', label: 'Conteneurs', icon: 'i-lucide-box', component: () => import('./pages/ContainersPage.vue') },
  { name: 'proxy', path: '/reverse-proxy', label: 'Reverse proxy', icon: 'i-lucide-arrow-up-right', component: () => import('./pages/ProxyPage.vue') },
  { name: 'dns', path: '/dns', label: 'Serveur DNS', icon: 'i-lucide-globe', component: () => import('./pages/DnsPage.vue') },
  { name: 'ssl', path: '/ssl', label: 'Certificats SSL', icon: 'i-lucide-lock', component: () => import('./pages/SslPage.vue') },
  { name: 'redis', path: '/redis', label: 'Cache Redis', icon: 'i-lucide-zap', component: () => import('./pages/RedisPage.vue') },
  { name: 's3', path: '/s3', label: 'Stockage S3', icon: 'i-lucide-hard-drive', component: () => import('./pages/S3Page.vue') },
  { name: 'websocket', path: '/websocket', label: 'WebSocket', icon: 'i-lucide-radio', component: () => import('./pages/WebsocketPage.vue') },
  { name: 'databases', path: '/bases-de-donnees', label: 'Bases de données', icon: 'i-lucide-database', component: () => import('./pages/DatabasesPage.vue') },
  { name: 'configuration', path: '/configuration', label: 'Configuration', icon: 'i-lucide-file-cog', component: () => import('./pages/ConfigurationPage.vue') },
  { name: 'access', path: '/acces-distant', label: 'Accès distant', icon: 'i-lucide-shield-check', component: () => import('./pages/AccessPage.vue') },
  { name: 'logs', path: '/journaux', label: 'Journaux', icon: 'i-lucide-square-terminal', component: () => import('./pages/LogsPage.vue') },
] as const


/** Page des préférences de l'interface, rangée à part dans la navigation. */
export const SETTINGS = { name: 'settings', path: '/parametres', label: 'Paramètres', icon: 'i-lucide-sliders-horizontal' } as const

