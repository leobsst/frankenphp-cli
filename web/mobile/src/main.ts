import '@app/assets/main.css'
import '@app/lib/icons'

import { createApp } from 'vue'
import ui from '@nuxt/ui/vue-plugin'
import { Capacitor } from '@capacitor/core'
import { Device } from '@capacitor/device'
import { i18n, setLanguage, setSystemLanguages } from '@app/i18n'
import { state } from '@app/composables/useFm'
import { setBeforeRequest, setUnauthorizedHandler, setUnreachableHint } from '@app/lib/api'
import App from './App.vue'
import { router } from './router'
import { loadInstances, patchInstance, setInstancePlatform } from '@app/lib/instances'
import { mobilePlatform } from './lib/instances'
import { allowLocalNetwork, localNetworkHint } from './lib/localNetwork'
import { handOffNativeSplash } from './lib/splash'
import { registerDemo } from './demo'
import { loadAppLock } from './lib/appLock'
import { setUpDisplay } from './lib/display'
import { setSettingsCard } from '@app/lib/shell'
import AppLockSettings from './components/AppLockSettings.vue'
import { allowPinchZoom } from '@app/lib/viewport'

allowPinchZoom()

// The HTML splash screen is already displayed (index.html): the native one can go.
handOffNativeSplash()

// Demo mode: the `demo:` instance is answered in memory (src/demo), before anything connects.
registerDemo()

// The instance list (shared with the dashboard), stored in Preferences and the Keychain / Keystore.
setInstancePlatform(mobilePlatform)

// Local network: Android 17 needs the user's permission, asked before the first request (iOS asks by itself).
// Refused, the « unreachable » message says where to allow it.
setBeforeRequest(allowLocalNetwork)
setUnreachableHint(localNetworkHint)

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

// Settings › the lock of the app, only where it exists (iPhone, iPad, Android).
if (Capacitor.isNativePlatform()) setSettingsCard(AppLockSettings)

// Before mounting: the lock (nothing shows before LockScreen) and the interface size (the first screen has it).
void Promise.all([loadInstances(), loadPhoneLanguage(), loadAppLock().catch(() => {}), setUpDisplay()])
  .then(() => createApp(App).use(i18n).use(router).use(ui).mount('#app'))
