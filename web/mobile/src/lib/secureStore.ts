import { Capacitor, registerPlugin } from '@capacitor/core'

/**
 * Secret storage of the app, for the session tokens: the iOS Keychain (ios/App/App/SecureStorePlugin.swift) and the
 * Android Keystore (android/app/src/main/java/dev/frankenmanager/mobile/SecureStorePlugin.java), two small plugins
 * written in the app itself and registered by MainViewController / MainActivity.
 *
 * In a browser (pnpm dev), there is no secure storage: the values go to sessionStorage, forgotten with the tab.
 */
interface SecureStorePlugin {
  get(options: { key: string }): Promise<{ value: string | null }>
  set(options: { key: string; value: string }): Promise<void>
  remove(options: { key: string }): Promise<void>
}

const web: SecureStorePlugin = {
  async get({ key }) { return { value: sessionStorage.getItem(key) } },
  async set({ key, value }) { sessionStorage.setItem(key, value) },
  async remove({ key }) { sessionStorage.removeItem(key) },
}

const SecureStore = Capacitor.isNativePlatform() ? registerPlugin<SecureStorePlugin>('SecureStore') : web

export const secureGet = async (key: string) => (await SecureStore.get({ key })).value ?? null
export const secureSet = (key: string, value: string) => SecureStore.set({ key, value })
export const secureRemove = (key: string) => SecureStore.remove({ key })
