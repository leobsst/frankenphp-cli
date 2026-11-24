import { reactive } from 'vue'
import { Preferences } from '@capacitor/preferences'
import type { Instance as ApiInstance } from '@app/lib/api'

/**
 * A FrankenManager instance managed by the app: the address of its remote access (HTTPS) and the session opened with
 * the dashboard password. The token can be revoked from the dashboard (Remote access › Devices) and expires after
 * 30 days without use.
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

const KEY = 'fm-instances'
/** Default port of the remote access (DASHBOARD_REMOTE_PORT). */
export const DEFAULT_REMOTE_PORT = 9443

export const store = reactive<{ list: Instance[]; loaded: boolean }>({ list: [], loaded: false })

export async function loadInstances() {
  try {
    const { value } = await Preferences.get({ key: KEY })
    store.list = value ? (JSON.parse(value) as Instance[]) : []
  } catch {
    store.list = []
  }
  store.loaded = true
}

async function save() {
  await Preferences.set({ key: KEY, value: JSON.stringify(store.list) })
}

export const findInstance = (id: string) => store.list.find((i) => i.id === id)

export async function upsertInstance(inst: Instance) {
  const i = store.list.findIndex((x) => x.id === inst.id)
  if (i >= 0) store.list[i] = inst
  else store.list.push(inst)
  await save()
}

export async function patchInstance(id: string, patch: Partial<Instance>) {
  const inst = findInstance(id)
  if (!inst) return
  Object.assign(inst, patch)
  await save()
}

export async function removeInstance(id: string) {
  store.list = store.list.filter((i) => i.id !== id)
  await save()
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
