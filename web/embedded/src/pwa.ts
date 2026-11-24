/**
 * Service worker of the installable dashboard (PWA, sw/sw.js), registered on the remote access only: that is where a
 * phone installs the dashboard and needs it to open without the daemon. The pages of this machine
 * (https://frankenmanager.test, https://127.0.0.1:DASHBOARD_HTTPS_PORT) gain nothing from it, and a service worker
 * left on them would outlive the daemon's updates: none is registered there, and one found is removed.
 *
 * A new version (a new build served by the daemon) installs in the background and waits: `offerUpdate` gets the
 * function that switches to it and reloads the page, called when the user asks.
 */
export function setupServiceWorker(mode: 'local' | 'remote', offerUpdate: (apply: () => void) => void) {
  // Secure contexts only (HTTPS with a trusted certificate), and the build only: no sw.js under `pnpm dev`.
  if (!import.meta.env.PROD || !window.isSecureContext || !('serviceWorker' in navigator)) return
  const container = navigator.serviceWorker
  if (mode !== 'remote') {
    void container.getRegistrations().then((all) => all.forEach((r) => void r.unregister()), () => {})
    return
  }
  // The first install takes control of the page too: only a requested update reloads it.
  let reloading = false
  container.addEventListener('controllerchange', () => {
    if (reloading) location.reload()
  })
  const offer = (worker: ServiceWorker) =>
    offerUpdate(() => {
      reloading = true
      worker.postMessage({ type: 'SKIP_WAITING' })
    })
  container
    .register('sw.js', { scope: './', updateViaCache: 'none' })
    .then((reg) => {
      // An update is a new worker installed while another one controls the page.
      if (reg.waiting && container.controller) offer(reg.waiting)
      reg.addEventListener('updatefound', () => {
        const worker = reg.installing
        worker?.addEventListener('statechange', () => {
          if (worker.state === 'installed' && container.controller) offer(worker)
        })
      })
      // An installed app can stay open for days: look for a new version when it comes back to the foreground,
      // and every hour.
      const check = () => void reg.update().catch(() => {})
      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') check()
      })
      setInterval(check, 60 * 60_000)
    })
    .catch(() => {
      // Certificate not trusted by the device, private browsing…: the dashboard works without it.
    })
}
