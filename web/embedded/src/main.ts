import '@app/assets/main.css'
import '@app/lib/icons'

import { createApp } from 'vue'
import ui from '@nuxt/ui/vue-plugin'
import { i18n } from '@app/i18n'
import { loadInstances, setInstancePlatform } from '@app/lib/instances'
import App from './App.vue'
import { router } from './router'
import { boot } from './session'
import { webPlatform } from './instances'
import { allowPinchZoom } from '@app/lib/viewport'

allowPinchZoom()

// The other instances this dashboard manages (instance list, shared with the mobile app), kept in this browser.
setInstancePlatform(webPlatform)

void loadInstances().then(() => createApp(App).use(i18n).use(router).use(ui).mount('#app'))
void boot()
