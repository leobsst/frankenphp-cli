import { markRaw, ref, watch } from 'vue'
import { Capacitor, registerPlugin, type PluginListenerHandle } from '@capacitor/core'
import { state } from '@app/composables/useFm'
import { shell, type PageTab } from '@app/lib/shell'
import { dialogOpen, toastShown, watchOverlays } from './overlays'

/**
 * Native page tabs on iOS (ios/App/App/NativeSegmentsPlugin.swift): a UISegmentedControl in a bar pinned below the
 * header, the content scrolling under it. The pages show them through PageTabs (web/app), on a phone; elsewhere, and
 * if the plugin does not answer, PageTabs draws them in HTML.
 */
interface NativeSegmentsPlugin {
  show(options: { items: { id: string; title: string }[]; selected: string; top: number; hidden: boolean }): Promise<{ height: number }>
  hide(): Promise<void>
  setHidden(options: { hidden: boolean }): Promise<void>
  setAppearance(options: { theme: 'light' | 'dark' | 'system' }): Promise<void>
  addListener(event: 'segmentSelected', fn: (data: { id: string }) => void): Promise<PluginListenerHandle>
}
const NativeSegments = registerPlugin<NativeSegmentsPlugin>('NativeSegments')

/** The native tabs are displayed: the content leaves their height free at the top (--native-segments-height). */
export const nativeSegments = ref(false)

/** Offset of the bar below the top safe area, in screen points: the header (its height and its border), zoomed. */
function headerOffset() {
  const root = document.documentElement
  const header = parseFloat(getComputedStyle(root).getPropertyValue('--ui-header-height')) || 64
  const zoom = parseFloat(root.style.getPropertyValue('--fm-zoom')) || 1
  return (header + 1) * zoom
}

/** Gives the pages the native tabs (iOS); returns the function that takes them back. */
export async function mountNativeSegments(): Promise<() => void> {
  if (Capacitor.getPlatform() !== 'ios') return () => {}
  /** Page whose tabs are shown. */
  let current: { owner: object; onSelect: (value: string) => void } | null = null
  let tap: PluginListenerHandle
  try {
    tap = await NativeSegments.addListener('segmentSelected', ({ id }) => current?.onSelect(id))
  } catch {
    return () => {}
  }

  // Hidden while a modal, the menu or a toast (top of the screen, under the bar) is open. Plain variable: show() is
  // called from a watchEffect of the page, which must not depend on it.
  let hidden = false
  const stopOverlays = watchOverlays()
  const stopHidden = watch([dialogOpen, toastShown], ([dialog, toast]) => {
    hidden = dialog || toast
    if (current) void NativeSegments.setHidden({ hidden }).catch(() => {})
  }, { immediate: true })
  const stopAppearance = watch(() => state.prefs.theme, (theme) => { void NativeSegments.setAppearance({ theme }).catch(() => {}) }, { immediate: true })

  shell.pageTabs = markRaw({
    show(owner: object, items: PageTab[], selected: string, onSelect: (value: string) => void) {
      current = { owner, onSelect }
      const options = { items: items.map((i) => ({ id: i.value, title: i.label })), selected, top: headerOffset(), hidden }
      void NativeSegments.show(options).then(({ height }) => {
        if (current?.owner !== owner) return
        // Screen points: divided by the interface zoom (main.css) to stay the same on screen.
        document.documentElement.style.setProperty('--native-segments-height', `calc(${height}px / var(--fm-zoom, 1))`)
        nativeSegments.value = true
      }, () => {
        // Native build without the plugin: the pages draw their tabs in HTML.
        shell.pageTabs = null
      })
    },
    hide(owner: object) {
      if (current?.owner !== owner) return
      current = null
      nativeSegments.value = false
      document.documentElement.style.removeProperty('--native-segments-height')
      void NativeSegments.hide().catch(() => {})
    },
  })

  return () => {
    shell.pageTabs = null
    current = null
    nativeSegments.value = false
    document.documentElement.style.removeProperty('--native-segments-height')
    stopHidden()
    stopAppearance()
    stopOverlays()
    void tap.remove()
    void NativeSegments.hide().catch(() => {})
  }
}
