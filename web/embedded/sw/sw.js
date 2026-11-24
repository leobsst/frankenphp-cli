/* global VERSION, PRECACHE */
/**
 * Service worker of the dashboard, registered on the remote access only (src/pwa.ts). The build (sw/plugin.ts)
 * prepends VERSION (a hash of the built files) and PRECACHE (those files, relative to the scope).
 *
 * - Install: precaches the app shell (index.html, served at the root of the scope, and the files of the build), so
 *   that the installed app opens without the daemon and shows its own "daemon unreachable" screen.
 * - Navigations: network first, the cached index.html when the daemon does not answer (or is too slow).
 * - Precached files: from the cache (hashed names, they never change under the same name).
 * - Everything else goes to the network untouched, never cached: the API (/api/*), /config.json (mode and
 *   version of the daemon), this script, the requests to other origins and any non-GET request.
 * - An update waits until the page asks for it (SKIP_WAITING, the "Recharger" button), never swapping the
 *   version under an open session; the caches of the previous versions are deleted on activation.
 */

const CACHE = `fm-shell-${VERSION}`
const SCOPE = new URL(self.registration.scope)
const SHELL = new URL('./', SCOPE).href
const PRECACHED = new Set(PRECACHE.map((p) => new URL(p, SCOPE).href))
/** Time given to the daemon to answer a navigation before the cached page is shown. */
const NAVIGATION_TIMEOUT = 4000

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE).then((cache) =>
      // cache: 'reload': from the daemon itself, never an older copy from the HTTP cache.
      cache.addAll([SHELL, ...PRECACHED].map((url) => new Request(url, { cache: 'reload' }))),
    ),
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      for (const key of await caches.keys()) {
        if (key.startsWith('fm-shell-') && key !== CACHE) await caches.delete(key)
      }
      await self.clients.claim()
    })(),
  )
})

self.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'SKIP_WAITING') void self.skipWaiting()
})

/** Is this URL served by the daemon to the page, outside the API and the configuration? */
function ownPage(url) {
  if (url.origin !== SCOPE.origin || !url.pathname.startsWith(SCOPE.pathname)) return false
  const rel = url.pathname.slice(SCOPE.pathname.length)
  return !(rel === 'api' || rel.startsWith('api/') || rel === 'config.json' || rel === 'sw.js')
}

async function navigate(request) {
  const network = fetch(request)
  network.catch(() => {}) // Settled later, or never read: no unhandled rejection.
  try {
    const response = await Promise.race([network, new Promise((resolve) => setTimeout(resolve, NAVIGATION_TIMEOUT))])
    if (response) return response
  } catch {
    // Offline, or the daemon is stopped: the cached page below.
  }
  const cached = await caches.match(SHELL, { cacheName: CACHE })
  return cached || network
}

async function precached(request) {
  return (await caches.match(request, { cacheName: CACHE, ignoreSearch: true })) || fetch(request)
}

self.addEventListener('fetch', (event) => {
  const request = event.request
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (!ownPage(url)) return
  if (request.mode === 'navigate') {
    event.respondWith(navigate(request))
    return
  }
  if (PRECACHED.has(url.origin + url.pathname)) event.respondWith(precached(request))
})
