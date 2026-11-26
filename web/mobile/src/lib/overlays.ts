import { ref } from 'vue'

/**
 * Native views laid over the web view (tab bar, page tabs) would cover what the page opens above its content: they
 * hide while a modal, a slideover or the menu (dialog role in Nuxt UI) is open, and the page tabs also while a toast
 * (alert role, at the top of the screen) is displayed.
 */
export const dialogOpen = ref(false)
export const toastShown = ref(false)

let observer: MutationObserver | undefined
let users = 0

const sync = () => {
  dialogOpen.value = !!document.querySelector('[role="dialog"], [role="alertdialog"]')
  toastShown.value = !!document.querySelector('li[role="alert"]')
}

/** Starts following the overlays (once for all the native views); returns the function that stops it. */
export function watchOverlays(): () => void {
  if (!users++) {
    observer = new MutationObserver(sync)
    observer.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['role', 'data-state'] })
    sync()
  }
  return () => {
    if (!--users) { observer?.disconnect(); observer = undefined }
  }
}
