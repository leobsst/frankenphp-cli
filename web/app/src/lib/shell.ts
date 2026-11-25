import { markRaw, reactive, type Component } from 'vue'

/**
 * What the application mounting the dashboard provides to it: the name and address of the managed instance and, if
 * there are several (the mobile app), the page used to switch between them.
 */
export const shell = reactive({
  /** Displayed instance name; empty: the machine name, read from the API. */
  name: '',
  /** Displayed instance address; empty: its address on the local network, read from the API. */
  address: '',
  /** Route of the instance list, null if there is only one. */
  switchTo: null as string | null,
  /**
   * Interface scale of the `auto` text size. 1 everywhere, except where the app knows the system shrinks it: the iPad
   * app running on a Mac (macOS draws it at 77 %).
   */
  autoScale: 1,
  /**
   * Applies the interface scale natively (iOS: the zoom of the WebView, which also adapts the breakpoints), returns
   * false when it cannot: the dashboard then zooms with CSS.
   */
  setZoom: null as ((scale: number) => boolean) | null,
  /** Settings card of the application itself (the mobile app: the Face ID lock), shown in Settings. */
  settingsCard: null as Component | null,
})

/** Registers the application's settings card (not made reactive: a component). */
export function setSettingsCard(card: Component | null) {
  shell.settingsCard = card && markRaw(card)
}
