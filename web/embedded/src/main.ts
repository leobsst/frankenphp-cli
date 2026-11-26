import '@app/assets/main.css'
import '@app/lib/icons'

import { createApp } from 'vue'
import ui from '@nuxt/ui/vue-plugin'
import { i18n } from '@app/i18n'
import App from './App.vue'
import { router } from './router'
import { boot } from './session'
import { allowPinchZoom } from '@app/lib/viewport'

allowPinchZoom()

createApp(App).use(i18n).use(router).use(ui).mount('#app')
void boot()
