import { Capacitor, registerPlugin } from '@capacitor/core'
import { shell } from '@app/lib/shell'

/**
 * Interface scale on iOS (ios/App/App/DisplayPlugin.swift): the WebView zooms natively, and the iPad app running on a
 * Mac gets a larger automatic size. Elsewhere (Android, browser) the dashboard zooms with CSS (main.css).
 */
interface DisplayPlugin {
  info(): Promise<{ onMac: boolean }>
  setZoom(options: { scale: number }): Promise<void>
}
const Display = registerPlugin<DisplayPlugin>('Display')

/** macOS draws the iPad app at 77 %: the automatic size brings its text back to the size of an iPad's. */
const MAC_SCALE = 1.3

/** Called before mounting: the first screen already has the right size. */
export async function setUpDisplay() {
  if (Capacitor.getPlatform() !== 'ios') return
  try {
    const { onMac } = await Display.info()
    if (onMac) shell.autoScale = MAC_SCALE
  } catch {
    return // Native build without the plugin: CSS zoom.
  }
  let current = 1
  shell.setZoom = (scale) => {
    if (scale !== current) {
      current = scale
      void Display.setZoom({ scale }).catch(() => {})
    }
    return true
  }
}
