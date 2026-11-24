import { ref, watch } from 'vue'
import type { Router } from 'vue-router'
import { Capacitor, registerPlugin, type PluginListenerHandle } from '@capacitor/core'
import { state } from '@app/composables/useFm'
import { draftPrefs } from '@app/composables/usePrefs'
import { language, t } from '@app/i18n'
import { splashDone } from './splash'

/**
 * Native iOS tab bar (ios/App/App/NativeTabBarPlugin.swift): Liquid Glass from iOS 26 on. It replaces BottomNav on
 * iPhone; the HTML bar stays on Android, in the browser, and if the plugin does not answer.
 */
interface NativeTabBarPlugin {
  show(options: { items: { id: string; title: string; icon: string }[]; selected?: string }): Promise<{ height: number }>
  select(options: { id?: string }): Promise<void>
  setHidden(options: { hidden: boolean }): Promise<void>
  remove(): Promise<void>
  setAppearance(options: { tint: string; theme: 'light' | 'dark' | 'system' }): Promise<void>
  addListener(event: 'tabSelected', fn: (data: { id: string }) => void): Promise<PluginListenerHandle>
}
const NativeTabBar = registerPlugin<NativeTabBarPlugin>('NativeTabBar')

/** The BottomNav tabs, with the SF Symbols closest to their Lucide icons; the id is the route. */
const TABS = [
  { id: '/', labelKey: 'tabs.overview', icon: 'square.grid.2x2' },
  { id: '/projets', labelKey: 'tabs.projects', icon: 'globe' },
  { id: '/conteneurs', labelKey: 'tabs.services', icon: 'shippingbox' },
  { id: '/parametres', labelKey: 'tabs.settings', icon: 'slider.horizontal.3' },
]

/** The tabs with their titles in the active language. */
const items = () => TABS.map(({ id, labelKey, icon }) => ({ id, title: t(labelKey), icon }))

/** The native bar is displayed: BottomNav hides and the content leaves its height free at the bottom. */
export const nativeTabs = ref(false)

/** Phone: above that width, the dashboard sidebar replaces the bottom bar. */
const phone = () => window.matchMedia('(max-width: 719px)')

/** Shows the native bar and links it to the router; returns the function that removes it. */
export async function mountNativeTabBar(router: Router): Promise<() => void> {
  if (Capacitor.getPlatform() !== 'ios' || !phone().matches) return () => {}
  // The bar is a native view above the page: it would cover the splash screen.
  await splashDone
  const current = () => (TABS.some((t) => t.id === router.currentRoute.value.path) ? router.currentRoute.value.path : undefined)
  let height: number
  try {
    ;({ height } = await NativeTabBar.show({ items: items(), selected: current() }))
  } catch {
    return () => {}
  }
  nativeTabs.value = true
  document.documentElement.style.setProperty('--native-tabbar-height', `${height}px`)

  // Primary colour and theme of the app (Settings): the Settings page previews them before saving them.
  const stopAppearance = watch(
    () => (router.currentRoute.value.name === 'settings' ? draftPrefs.value : state.prefs),
    (p) => { void NativeTabBar.setAppearance({ tint: p.primary, theme: p.theme }) },
    { immediate: true, deep: true },
  )

  const tap = await NativeTabBar.addListener('tabSelected', ({ id }) => { void router.push(id) })
  const stopRoute = watch(() => router.currentRoute.value.path, () => { void NativeTabBar.select({ id: current() }) })

  // The native bar lies above the whole page: it is hidden while a modal, a slideover or the menu (dialog role in
  // Nuxt UI) is open, otherwise it would cover its bottom.
  let hidden = false
  const sync = () => {
    const hide = !!document.querySelector('[role="dialog"], [role="alertdialog"]')
    if (hide !== hidden) { hidden = hide; void NativeTabBar.setHidden({ hidden: hide }) }
  }
  const observer = new MutationObserver(sync)
  observer.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['role', 'data-state'] })

  // Language changed (Settings): the bar is shown again with the new titles. show() reuses the bar and makes it
  // visible, so the hidden state is synced again afterwards.
  const stopLanguage = watch(language, () => {
    void NativeTabBar.show({ items: items(), selected: current() }).then(() => { hidden = false; sync() }, () => {})
  })

  return () => {
    observer.disconnect()
    stopLanguage()
    stopRoute()
    stopAppearance()
    void tap.remove()
    void NativeTabBar.remove()
    nativeTabs.value = false
    document.documentElement.style.removeProperty('--native-tabbar-height')
  }
}
