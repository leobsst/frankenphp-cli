import type { Router } from 'vue-router'
import { Preferences } from '@capacitor/preferences'
import { setLanguage, type LanguagePref } from '@app/i18n'
import { addDemoInstance, setCurrent } from '../lib/instances'

/**
 * Screenshot builds only (`vite build --mode screenshots`, see README): a link opens a page of the demo directly, so
 * that the App Store screenshots can be taken from the iOS Simulator without driving taps by hand. The link comes
 * either through the URL scheme (iOS asks to confirm), or, without any prompt, through the app preferences, read
 * every half second:
 *
 *   xcrun simctl spawn <device> defaults write <app id> CapacitorStorage.fm-screenshot \
 *     'frankenmanager://demo/projets?lang=fr&tap=Ajouter%20un%20projet&n=1'
 *
 * The path is the app route ("/" by default, "instances" for the list); `lang` sets the interface language; each
 * `tap` clicks, in order, the first button or link whose text or label is that text. Production builds drop this
 * module: the `frankenmanager://` links stay the pairing ones.
 */
export const SCREENSHOT_LINKS = import.meta.env.MODE === 'screenshots'

export const isScreenshotLink = (url: string) => /^frankenmanager:\/\/demo(\/|\?|$)/i.test(url)

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function findClickable(text: string): HTMLElement | undefined {
  const wanted = text.trim().toLowerCase()
  const all = [...document.querySelectorAll<HTMLElement>('button, a, [role="menuitem"], [role="tab"], [role="option"]')]
  return all.find((el) => el.getAttribute('aria-label')?.trim().toLowerCase() === wanted || el.textContent?.trim().toLowerCase() === wanted)
}

export async function openScreenshotLink(router: Router, url: string) {
  const link = new URL(url.replace(/^frankenmanager:\/\/demo/i, 'demo://x'))
  const lang = link.searchParams.get('lang')
  if (lang) setLanguage(lang as LanguagePref)
  const path = link.pathname.replace(/^\/+/, '')
  // Any open dialog (sheet, menu) is closed first: the page starts clean.
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  if (path === 'instances') {
    await router.push('/instances')
  } else {
    const inst = await addDemoInstance()
    await setCurrent(inst.id)
    await router.push(`/${path}`)
  }
  for (const text of link.searchParams.getAll('tap')) {
    // The page loads its data first; then each tap waits for its target to appear.
    await sleep(1_200)
    for (let i = 0; i < 20; i++) {
      const el = findClickable(text)
      if (el) {
        // Menus (Reka) open on pointerdown, buttons on click.
        el.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, button: 0, pointerType: 'mouse' }))
        el.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, button: 0, pointerType: 'mouse' }))
        el.click()
        break
      }
      await sleep(250)
    }
  }
  // A tap may focus a field: the keyboard would cover the screenshot.
  await sleep(600)
  if (document.activeElement instanceof HTMLElement) document.activeElement.blur()
}

/** Follows the links written to the `fm-screenshot` preference (a changed value opens it). */
export function watchScreenshotRequests(router: Router) {
  let last: string | null | undefined
  setInterval(async () => {
    const { value } = await Preferences.get({ key: 'fm-screenshot' }).catch(() => ({ value: null }))
    if (last !== undefined && value && value !== last && isScreenshotLink(value)) void openScreenshotLink(router, value)
    last = value
  }, 500)
}
