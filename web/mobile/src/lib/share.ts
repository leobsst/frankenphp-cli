import { registerPlugin } from '@capacitor/core'

/**
 * Share sheet of a text file (ios/App/App/FileSharePlugin.swift, android/…/FileSharePlugin.java): the WebView of the
 * app ignores the downloads of the page, so the logs export goes through it (shell.saveText, set by main.ts).
 */
interface FileSharePlugin {
  shareText(options: { name: string; text: string }): Promise<{ completed?: boolean }>
}
const FileShare = registerPlugin<FileSharePlugin>('FileShare')

/** Resolves true once shared, false when cancelled, null when the system does not tell (Android). */
export async function shareText(name: string, text: string): Promise<boolean | null> {
  const { completed } = await FileShare.shareText({ name, text })
  return completed ?? null
}
