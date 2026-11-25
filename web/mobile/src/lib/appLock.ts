import { reactive } from 'vue'
import { App as CapApp } from '@capacitor/app'
import { Capacitor, registerPlugin } from '@capacitor/core'
import { Preferences } from '@capacitor/preferences'
import { t } from '@app/i18n'

/**
 * Lock of the app with Face ID / Touch ID (iOS) or the fingerprint / face unlock (Android), the device passcode as
 * fallback: two small plugins written in the app itself (ios/App/App/AppLockPlugin.swift,
 * android/app/src/main/java/dev/frankenmanager/mobile/AppLockPlugin.java).
 *
 * Turned on in Settings, it asks at launch and on return to the foreground after GRACE_MS away; meanwhile LockScreen
 * covers the app. The choice is kept on this device only.
 */
export type Biometry = 'faceId' | 'touchId' | 'biometric' | 'none'
interface AppLockPlugin {
  status(): Promise<{ available: boolean; biometry: Biometry }>
  authenticate(options: { reason: string; cancelTitle?: string }): Promise<void>
}
const AppLock = registerPlugin<AppLockPlugin>('AppLock')

const KEY = 'fm-app-lock'
/** Away for less than this, the app opens again without asking (a glance at a message, a copied code). */
const GRACE_MS = 30_000

export const appLock = reactive({
  /** The device can authenticate its owner (biometry or passcode), in the native app. */
  supported: false,
  biometry: 'none' as Biometry,
  enabled: false,
  /** Locked: LockScreen covers the app until the owner authenticates. */
  locked: false,
  /** The app is in the background: its content is hidden from the app switcher. */
  covered: false,
  /** A system prompt is open. */
  busy: false,
})

/** Asks the system prompt once; false when cancelled or failed. */
async function authenticate() {
  if (appLock.busy) return false
  appLock.busy = true
  try {
    await AppLock.authenticate({ reason: t('mobile.lock.reason'), cancelTitle: t('common.cancel') })
    return true
  } catch (e) {
    // Nothing left to authenticate with (passcode removed, which itself needs the passcode): the lock cannot be used.
    if ((e as { code?: string }).code === 'unavailable') await readStatus()
    return false
  } finally {
    appLock.busy = false
  }
}

async function readStatus() {
  try {
    const s = await AppLock.status()
    appLock.supported = s.available
    appLock.biometry = s.biometry
  } catch {
    appLock.supported = false
  }
  if (!appLock.supported) appLock.locked = false
}

/** Unlocks the app; true when it is unlocked. */
export async function unlock() {
  if (!appLock.locked) return true
  if (await authenticate()) appLock.locked = false
  return !appLock.locked
}

/**
 * Turns the lock on or off. Both ways ask the owner first: nobody turns it on with their own face, nor off on a phone
 * left unlocked.
 */
export async function setAppLock(on: boolean) {
  if (on === appLock.enabled) return true
  await readStatus()
  if (!appLock.supported || !(await authenticate())) return false
  appLock.enabled = on
  await Preferences.set({ key: KEY, value: String(on) })
  return true
}

/**
 * Reads the setting and locks the app if it is on: called before mounting, so that nothing shows before LockScreen.
 * The prompt itself waits for LockScreen (the app must be active for it).
 */
export async function loadAppLock() {
  if (!Capacitor.isNativePlatform()) return
  await readStatus()
  appLock.enabled = (await Preferences.get({ key: KEY })).value === 'true'
  appLock.locked = appLock.enabled && appLock.supported

  // `pause` / `resume`: the app goes to the background and comes back. The system prompt only makes the app inactive
  // (appStateChange), so it does not count as leaving. The prompt opens once the app is active again, once per return:
  // cancelled, it leaves the Unlock button of LockScreen.
  let hiddenAt = 0
  let promptOnActive = false
  void CapApp.addListener('pause', () => {
    if (!appLock.enabled) return
    hiddenAt = Date.now()
    appLock.covered = true
  })
  void CapApp.addListener('resume', () => {
    if (!appLock.enabled) return
    if (!appLock.locked && appLock.supported && Date.now() - hiddenAt > GRACE_MS) appLock.locked = true
    appLock.covered = false
    promptOnActive = appLock.locked
    // Android may report the app active before `resume`: the prompt then opens from here.
    setTimeout(() => void CapApp.getState().then(({ isActive }) => { if (isActive) promptNow() }), 250)
  })
  const promptNow = () => {
    if (!promptOnActive) return
    promptOnActive = false
    void unlock()
  }
  void CapApp.addListener('appStateChange', ({ isActive }) => { if (isActive) promptNow() })
}
