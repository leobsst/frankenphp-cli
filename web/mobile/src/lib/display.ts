import { Capacitor, registerPlugin } from '@capacitor/core'
import { shell } from '@app/lib/shell'

/**
 * Display of the app on iOS (ios/App/App/DisplayPlugin.swift): the iPad app running on a Mac gets a larger automatic
 * text size. The size itself is a CSS zoom on every platform (main.css).
 */
interface DisplayPlugin {
  info(): Promise<{ onMac: boolean }>
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
    // Native build without the plugin: the automatic size stays 1.
  }
}
