import { Preferences } from '@capacitor/preferences'
import { t } from '@app/i18n'
import type { Instance, InstancePlatform } from '@app/lib/instances'
import { secureGet, secureRemove, secureSet } from './secureStore'
import { deviceName } from './device'
import { openPairing, scanPairing } from './pairing'
import { DEMO_ID, DEMO_URL, isDemoUrl } from '../demo'
import { resetDemo } from '../demo/server'

/**
 * Storage of the instances in the mobile app (the list and its pages are shared: web/app/src/lib/instances.ts). The
 * list itself (name, address, session id…) is not secret and stays in Capacitor Preferences; each token lives in the
 * secure store (iOS Keychain, Android Keystore: src/lib/secureStore.ts), under `token.<id>`.
 */

/** What is kept in Preferences: everything but the token (present only in lists written by app 0.1.0). */
type StoredInstance = Omit<Instance, 'token'> & { token?: string }

const KEY = 'fm-instances'
/** Instance open at the last launch, stored by earlier versions: a cold start now opens the list. */
const LEGACY_CURRENT = 'fm-current'

const tokenKey = (id: string) => `token.${id}`

/** Instances whose token could not be moved to the secure store yet: it stays in Preferences until it is. */
const unmigrated = new Set<string>()

async function saveList(list: Instance[]) {
  const stored: StoredInstance[] = list.map(({ token, ...rest }) => (unmigrated.has(rest.id) ? { ...rest, token } : rest))
  await Preferences.set({ key: KEY, value: JSON.stringify(stored) })
}

/**
 * Reads the instance list and their tokens. The demo app (0.1.0) kept the tokens in Preferences, next to the list:
 * they are moved to the secure store, then removed from Preferences once safely written there. A token that cannot
 * be moved stays where it was, and the move is tried again on the next launch.
 */
async function load(): Promise<Instance[]> {
  void Preferences.remove({ key: LEGACY_CURRENT }).catch(() => {})
  let stored: StoredInstance[] = []
  try {
    const { value } = await Preferences.get({ key: KEY })
    stored = value ? (JSON.parse(value) as StoredInstance[]) : []
  } catch {
    stored = []
  }
  let migrated = false
  const list = await Promise.all(
    stored.map(async ({ token: legacy, ...rest }): Promise<Instance> => {
      // The demo instance has no session: nothing to read from the secure store.
      if (isDemoUrl(rest.url)) return { ...rest, token: '', expired: false }
      if (legacy) {
        try {
          await secureSet(tokenKey(rest.id), legacy)
          migrated = true
        } catch {
          unmigrated.add(rest.id)
        }
        return { ...rest, token: legacy }
      }
      const token = await secureGet(tokenKey(rest.id)).catch(() => null)
      // No token (secure store reset, backup restored on another device): the instance stays listed, to sign in again.
      return { ...rest, token: token ?? '', expired: rest.expired || !token }
    }),
  )
  if (migrated) await saveList(list)
  return list
}

export const mobilePlatform: InstancePlatform = {
  kind: 'app',
  load,
  save: saveList,
  async setToken(id, token) {
    await secureSet(tokenKey(id), token)
    unmigrated.delete(id)
  },
  async removeToken(id) {
    unmigrated.delete(id)
    await secureRemove(tokenKey(id))
  },
  deviceName,
  async scan(router) {
    const link = await scanPairing()
    if (link) await openPairing(router, link)
  },
  demo: {
    isDemo: isDemoUrl,
    create() {
      resetDemo()
      return { id: DEMO_ID, name: 'Demo', url: DEMO_URL, token: '', session: '', added: Date.now() }
    },
  },
  footer: () => t('mobile.instances.version', { version: __APP_VERSION__, build: __APP_BUILD__ }),
}
