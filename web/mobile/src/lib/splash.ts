import { SplashScreen } from '@capacitor/splash-screen'

/**
 * Splash screen: the native screen (image from assets/) stays displayed until the page is painted, then the HTML
 * screen of index.html, identical with a spinner, takes over until the app is ready.
 */

/** The HTML screen is painted: the native screen can go, without a transition since both are identical. */
export function handOffNativeSplash() {
  requestAnimationFrame(() => requestAnimationFrame(() => {
    void SplashScreen.hide({ fadeOutDuration: 0 }).catch(() => {})
  }))
}

let hidden = false
let resolveDone: () => void
/** Resolved when the HTML screen fades out: the native tab bar, a view laid over the page, waits for that moment. */
export const splashDone = new Promise<void>((resolve) => { resolveDone = resolve })

/** The app is ready: the HTML screen fades out. */
export function hideSplash() {
  if (hidden) return
  hidden = true
  resolveDone()
  const el = document.getElementById('fm-splash')
  if (!el) return
  el.classList.add('fm-splash-out')
  el.addEventListener('transitionend', () => el.remove(), { once: true })
  setTimeout(() => el.remove(), 600)
}
