import { _api } from '@iconify/vue'

/**
 * Cuts the Iconify runtime off from its online API (api.iconify.design and its mirrors).
 *
 * Every icon is bundled at build time (`uiIcons` in theme.ts): the dashboard and the mobile app must never ask a
 * third-party server for one, neither offline (PWA, app) nor on a network without Internet access. An icon missing
 * from the bundle is reported as not found instead of being fetched, and named in the console during development.
 * Imported first by the entry points of the embedded dashboard and the mobile app.
 */
_api.setAPIModule('', {
  prepare: (provider, prefix, icons) => [{ type: 'icons', provider, prefix, icons }],
  send: (_host, params, callback) => {
    if (import.meta.env.DEV && params.type === 'icons') {
      console.warn(`Icons not bundled, add them in full to the sources: ${params.icons.map((i) => `${params.prefix}:${i}`).join(', ')}`)
    }
    callback('abort', 404)
  },
})
