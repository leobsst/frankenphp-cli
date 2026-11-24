import '@app/assets/main.css'
import '@app/lib/icons'

import { createApp } from 'vue'
import ui from '@nuxt/ui/vue-plugin'
import { Device } from '@capacitor/device'
import { i18n, setLanguage, setSystemLanguages } from '@app/i18n'
import { state } from '@app/composables/useFm'
import { setUnauthorizedHandler } from '@app/lib/api'
import App from './App.vue'
import { router } from './router'
import { loadCurrent, loadInstances, patchInstance } from './lib/instances'
import { handOffNativeSplash } from './lib/splash'
import { registerDemo } from './demo'

// The HTML splash screen is already displayed (index.html): the native one can go.
handOffNativeSplash()

// Demo mode: the `demo:` instance is answered in memory (src/demo), before anything connects.
registerDemo()

// Session rejected by the daemon (revoked, expired, password changed): the instance is marked, DashboardLayout then
// leads to its sign-in, and the list shows it.
setUnauthorizedHandler((inst) => void patchInstance(inst.id, { expired: true }))

/**
 * The phone's language, which the WebView does not always report (iOS gives the app's localizations): the system
 * language of the dashboard follows it. Read before mounting, so the first screen is already in the right language.
 */
async function loadPhoneLanguage() {
  try {
    const { value } = await Device.getLanguageTag()
    setSystemLanguages([value])
    setLanguage(state.prefs.language)
  } catch {
    // Browser without the plugin: navigator.languages stays.
  }
}

void Promise.all([loadInstances().then(loadCurrent), loadPhoneLanguage()])
  .then(() => createApp(App).use(i18n).use(router).use(ui).mount('#app'))
