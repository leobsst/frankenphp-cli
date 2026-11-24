import '@app/assets/main.css'
import '@app/lib/icons'

import { createApp } from 'vue'
import ui from '@nuxt/ui/vue-plugin'
import { setUnauthorizedHandler } from '@app/lib/api'
import App from './App.vue'
import { router } from './router'
import { loadCurrent, loadInstances, patchInstance } from './lib/instances'
import { handOffNativeSplash } from './lib/splash'

// The HTML splash screen is already displayed (index.html): the native one can go.
handOffNativeSplash()

// Session rejected by the daemon (revoked, expired, password changed): the instance is marked, DashboardLayout then
// leads to its sign-in, and the list shows it.
setUnauthorizedHandler((inst) => void patchInstance(inst.id, { expired: true }))

void loadInstances().then(loadCurrent).then(() => createApp(App).use(router).use(ui).mount('#app'))
