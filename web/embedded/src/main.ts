import '@app/assets/main.css'
import '@app/lib/icons'

import { createApp } from 'vue'
import ui from '@nuxt/ui/vue-plugin'
import App from './App.vue'
import { router } from './router'
import { boot } from './session'

createApp(App).use(router).use(ui).mount('#app')
void boot()
