import { reactive } from 'vue'
import { Preferences } from '@capacitor/preferences'
import type { Instance as ApiInstance } from '@app/lib/api'
import { secureGet, secureRemove, secureSet } from './secureStore'

/**
 * A FrankenManager instance managed by the app: the address of its remote access (HTTPS) and the session opened with
 * the dashboard password or a pairing code. The token can be revoked from the dashboard (Remote access › Devices)
 * and expires after 30 days without use.
 *
 * Storage: the list itself (name, address, session id…) is not secret and stays in Capacitor Preferences; each token
 * lives in the secure store (iOS Keychain, Android Keystore: src/lib/secureStore.ts), under `token.<id>`.
 */
export interface Instance extends ApiInstance {
  name: string
  /** Origin of the remote access, for example https://192.168.1.42:9443. */
  url: string
  /** Session id on the daemon side (to recognize it in the list of devices). */
  session: string
  /** Daemon version at the last contact. */
  version?: string
  /** The session was rejected (revoked, expired, password changed): signing in again is needed. */
  expired?: boolean
  added: number
}

/** What is kept in Preferences: everything but the token (present only in lists written by app 0.1.0). */
type StoredInstance = Omit<Instance, 'token'> & { token?: string }

const KEY = 'fm-instances'
/** Default port of the remote access (DASHBOARD_REMOTE_PORT). */
export const DEFAULT_REMOTE_PORT = 9443

const tokenKey = (id: string) => `token.${id}`

export const store = reactive<{ list: Instance[]; loaded: boolean }>({ list: [], loaded: false })

/** Instances whose token could not be moved to the secure store yet: it stays in Preferences until it is. */
const unmigrated = new Set<string>()

/**
 * Reads the instance list and their tokens. The demo app (0.1.0) kept the tokens in Preferences, next to the list:
 * they are moved to the secure store, then removed from Preferences once safely written there. A token that cannot
 * be moved stays where it was, and the move is tried again on the next launch.
 */
export async function loadInstances() {
  let stored: StoredInstance[] = []
  try {
    const { value } = await Preferences.get({ key: KEY })
    stored = value ? (JSON.parse(value) as StoredInstance[]) : []
  } catch {
    stored = []
  }
  let migrated = false
  store.list = await Promise.all(
    stored.map(async ({ token: legacy, ...rest }): Promise<Instance> => {
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
  if (migrated) await save()
  store.loaded = true
}

/** Writes the list without the tokens (they are in the secure store), except those not moved there yet. */
async function save() {
  const list: StoredInstance[] = store.list.map(({ token, ...rest }) => (unmigrated.has(rest.id) ? { ...rest, token } : rest))
  await Preferences.set({ key: KEY, value: JSON.stringify(list) })
}

export const findInstance = (id: string) => store.list.find((i) => i.id === id)
export const findByUrl = (url: string) => store.list.find((i) => i.url === url)

export async function upsertInstance(inst: Instance) {
  await secureSet(tokenKey(inst.id), inst.token)
  unmigrated.delete(inst.id)
  const i = store.list.findIndex((x) => x.id === inst.id)
  if (i >= 0) store.list[i] = inst
  else store.list.push(inst)
  await save()
}

export async function patchInstance(id: string, patch: Partial<Instance>) {
  const inst = findInstance(id)
  if (!inst) return
  if (patch.token) {
    await secureSet(tokenKey(id), patch.token)
    unmigrated.delete(id)
  }
  Object.assign(inst, patch)
  await save()
}

export async function removeInstance(id: string) {
  store.list = store.list.filter((i) => i.id !== id)
  unmigrated.delete(id)
  await save()
  await secureRemove(tokenKey(id)).catch(() => {})
}

export const newId = () => Math.random().toString(36).slice(2, 10)

/**
 * Typed address → HTTPS origin of the remote access. Accepts « 192.168.1.42 », « mac.local:9443 » or a full URL;
 * remote access only exists over HTTPS, so an http:// is corrected.
 */
export function normalizeUrl(input: string): string {
  let s = input.trim()
  if (!s) throw new Error('Saisissez l’adresse de l’instance.')
  s = s.replace(/^http:\/\//i, 'https://')
  if (!/^https:\/\//i.test(s)) s = `https://${s}`
  let u: URL
  try {
    u = new URL(s)
  } catch {
    throw new Error('Adresse invalide.')
  }
  // Without a typed port, the remote access one; an explicit :443 (tunnel, public name) is kept.
  if (!u.port && !/^https:\/\/[^/]+:\d+/i.test(s)) u.port = String(DEFAULT_REMOTE_PORT)
  return u.origin
}

// ---------- Instance open in the dashboard ----------
const CURRENT = 'fm-current'
export const current = reactive<{ id: string | null }>({ id: null })

export async function loadCurrent() {
  const { value } = await Preferences.get({ key: CURRENT }).catch(() => ({ value: null }))
  current.id = value && findInstance(value) ? value : null
}

export async function setCurrent(id: string | null) {
  current.id = id
  if (id) await Preferences.set({ key: CURRENT, value: id })
  else await Preferences.remove({ key: CURRENT })
}

export const currentInstance = () => (current.id ? findInstance(current.id) : undefined)
