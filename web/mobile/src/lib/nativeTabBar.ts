import { ref, watch } from 'vue'
import type { Router } from 'vue-router'
import { Capacitor, registerPlugin, type PluginListenerHandle } from '@capacitor/core'
import { language, t } from '@app/i18n'
import { splashDone } from './splash'
import { dialogOpen, watchOverlays } from './overlays'

/**
 * Native iOS tab bar (ios/App/App/NativeTabBarPlugin.swift): Liquid Glass from iOS 26 on. It replaces BottomNav on
 * iPhone; the HTML bar stays on Android, in the browser, and if the plugin does not answer.
 */
interface NativeTabBarPlugin {
  show(options: { items: { id: string; title: string; icon: string }[]; selected?: string }): Promise<{ height: number }>
  select(options: { id?: string }): Promise<void>
  setHidden(options: { hidden: boolean }): Promise<void>
  remove(): Promise<void>
  /** Colours as #RRGGBB (empty: the system's), and the look of the page. */
  setAppearance(options: { tint: string; normal: string; theme: 'light' | 'dark' }): Promise<void>
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

/** A canvas pixel: resolves any CSS colour (oklch, color-mix…) to sRGB. */
let pixel: CanvasRenderingContext2D | null = null

/** Colour of a CSS variable of the page, as it shows it now (light or dark), as #RRGGBB; null when unreadable. */
function cssColor(variable: string): string | null {
  const probe = document.createElement('span')
  probe.style.cssText = `display:none;color:var(${variable})`
  document.body.append(probe)
  const value = getComputedStyle(probe).color
  probe.remove()
  pixel ??= Object.assign(document.createElement('canvas'), { width: 1, height: 1 }).getContext('2d', { willReadFrequently: true })
  if (!pixel || !value) return null
  pixel.clearRect(0, 0, 1, 1)
  pixel.fillStyle = value
  pixel.fillRect(0, 0, 1, 1)
  const [r = 0, g = 0, b = 0] = pixel.getImageData(0, 0, 1, 1).data
  return `#${[r, g, b].map((x) => x.toString(16).padStart(2, '0')).join('')}`
}

/**
 * Gives the bar the colours of the BottomNav it replaces: the active tab in `--ui-primary`, the others in
 * `--ui-text-muted`, and the light or dark look of the page. Read from the page itself, so they follow every change of
 * `<html>` (its `dark` class, its colour variables); returns the function that stops following.
 */
function followAppearance(): () => void {
  let last = ''
  const apply = () => {
    const appearance = {
      tint: cssColor('--ui-primary') ?? '',
      normal: cssColor('--ui-text-muted') ?? '',
      theme: document.documentElement.classList.contains('dark') ? 'dark' as const : 'light' as const,
    }
    const key = JSON.stringify(appearance)
    if (key === last) return
    last = key
    void NativeTabBar.setAppearance(appearance).catch(() => {})
  }
  const observer = new MutationObserver(apply)
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style'] })
  apply()
  return () => observer.disconnect()
}

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
  // Screen points: divided by the interface zoom (main.css) to stay the same on screen.
  document.documentElement.style.setProperty('--native-tabbar-height', `calc(${height}px / var(--fm-zoom, 1))`)

  // Colours of the bar: those the page shows, read after each change of the theme, of the colours (the Settings
  // preview included) or of the system appearance (theme « Système »).
  const stopAppearance = followAppearance()

  const tap = await NativeTabBar.addListener('tabSelected', ({ id }) => { void router.push(id) })
  const stopRoute = watch(() => router.currentRoute.value.path, () => { void NativeTabBar.select({ id: current() }) })

  // The native bar lies above the whole page: it is hidden while a modal, a slideover or the menu is open, otherwise
  // it would cover its bottom.
  const stopOverlays = watchOverlays()
  let hidden = false
  const sync = () => {
    if (dialogOpen.value !== hidden) { hidden = dialogOpen.value; void NativeTabBar.setHidden({ hidden }) }
  }
  const stopHidden = watch(dialogOpen, sync, { immediate: true })

  // Language changed (Settings): the bar is shown again with the new titles. show() reuses the bar and makes it
  // visible, so the hidden state is synced again afterwards.
  const stopLanguage = watch(language, () => {
    void NativeTabBar.show({ items: items(), selected: current() }).then(() => { hidden = false; sync() }, () => {})
  })

  return () => {
    stopHidden()
    stopOverlays()
    stopLanguage()
    stopRoute()
    stopAppearance()
    void tap.remove()
    void NativeTabBar.remove()
    nativeTabs.value = false
    document.documentElement.style.removeProperty('--native-tabbar-height')
  }
}
