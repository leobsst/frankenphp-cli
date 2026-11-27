import { reactive } from 'vue'
import type { Router } from 'vue-router'
import type { Instance as ApiInstance } from './api'
import { t } from '../i18n'

/**
 * The FrankenManager instances an application manages, shared by the mobile app and the embedded dashboard (PWA):
 * the address of each remote access (HTTPS) and the session opened there with the dashboard password or a pairing
 * code. A token can be revoked from the dashboard of its instance (Remote access › Devices) and expires after 30 days
 * without use.
 *
 * Each application stores the list through its platform (setInstancePlatform): the mobile app in Capacitor
 * Preferences with the tokens in the iOS Keychain / Android Keystore, the dashboard in the browser's localStorage.
 * The dashboard also lists the machine serving its pages (`home`), whose session is the page's own: it is never
 * stored here and cannot be removed.
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
  /** The machine serving the dashboard's pages (embedded dashboard only). */
  home?: boolean
}

/** What an application provides to store and add its instances. */
export interface InstancePlatform {
  /** `app`: the mobile app (a phone); `web`: the dashboard in a browser or installed (PWA). Picks some texts. */
  kind: 'app' | 'web'
  /** Reads the saved instances, their tokens included. */
  load(): Promise<Instance[]>
  /** Saves the list (tokens included: the platform keeps them apart if it wants to). */
  save(list: Instance[]): Promise<void>
  /** Keeps the token of an instance, before the list is saved; absent when save() keeps the tokens. */
  setToken?(id: string, token: string): Promise<void>
  /** Forgets the token of a removed instance. */
  removeToken?(id: string): Promise<void>
  /** Name of this device in the instances' lists of sessions (Remote access › Devices). */
  deviceName(): Promise<string>
  /** Pairing by QR code (the mobile app): scans the code and opens the pairing page; throws an Error to show. */
  scan?(router: Router): Promise<void>
  /** Demo mode (the mobile app): a simulated instance, without a session. */
  demo?: {
    isDemo(url: string): boolean
    /** A fresh demo instance, its simulated data reset. */
    create(): Instance
  }
  /** Line at the bottom of the instance list (the app version). */
  footer?(): string
  /**
   * State of the dashboard's machine (`home`), whose page signs in by itself: it is only asked for its state once
   * `ready`. Reactive. Absent: always `ready`.
   */
  homeState?(): 'loading' | 'ready' | 'signIn' | 'offline'
  /** Private IPv4 address of this device and the prefix length of its network, for the address placeholder; null when unknown. */
  localNetwork?(): Promise<LocalNetwork | null>
}

export interface LocalNetwork {
  ip: string
  prefix: number
}

let platform: InstancePlatform | null = null

/** Sets the platform of the application, before loadInstances(). */
export function setInstancePlatform(p: InstancePlatform) {
  platform = p
}

export function instancePlatform(): InstancePlatform {
  if (!platform) throw new Error('setInstancePlatform() was not called')
  return platform
}

export const store = reactive<{ list: Instance[]; loaded: boolean }>({ list: [], loaded: false })

/** Reads the instance list; the machine of the dashboard (setHomeInstance) stays first. */
export async function loadInstances() {
  const saved = await instancePlatform().load().catch(() => [])
  store.list = [...store.list.filter((i) => i.home), ...saved.filter((i) => !i.home)]
  store.loaded = true
}

const save = () => instancePlatform().save(store.list.filter((i) => !i.home))

/**
 * Sets the machine serving the dashboard, first in the list; replaced as a whole (a new object) on each change of its
 * session, so that a request sent with an older token is recognized as such.
 */
export function setHomeInstance(inst: Omit<Instance, 'home'>) {
  const i = store.list.findIndex((x) => x.home)
  // The daemon version read so far stays until the next read (noteVersion).
  const home: Instance = { version: store.list[i]?.version, ...inst, home: true }
  if (i >= 0) store.list[i] = home
  else store.list.unshift(home)
}

export const homeInstance = () => store.list.find((i) => i.home)
export const findInstance = (id: string) => store.list.find((i) => i.id === id)
export const findByUrl = (url: string) => store.list.find((i) => i.url === url)

export async function upsertInstance(inst: Instance) {
  await instancePlatform().setToken?.(inst.id, inst.token)
  const i = store.list.findIndex((x) => x.id === inst.id)
  if (i >= 0) store.list[i] = inst
  else store.list.push(inst)
  await save()
}

export async function patchInstance(id: string, patch: Partial<Instance>) {
  const inst = findInstance(id)
  if (!inst || inst.home) return
  if (patch.token) await instancePlatform().setToken?.(id, patch.token)
  Object.assign(inst, patch)
  await save()
}

/**
 * Daemon version read from an instance (/api/v1/host, with its status: the list, the dashboard): kept with it as soon
 * as it changes (after an update of the daemon), without waiting for a new sign-in. The demo keeps none.
 */
export async function noteVersion(id: string, version: string | undefined) {
  const inst = findInstance(id)
  if (!inst || !version || inst.version === version || isDemo(inst)) return
  inst.version = version
  // The dashboard's machine is not stored: its version lives as long as the page.
  if (!inst.home) await save()
}

export async function removeInstance(id: string) {
  if (findInstance(id)?.home) return
  store.list = store.list.filter((i) => i.id !== id)
  await save()
  await instancePlatform().removeToken?.(id).catch(() => {})
}

/** The demo instance (simulated data, the mobile app): never a token. */
export const isDemo = (inst: Pick<Instance, 'url'>) => !!platform?.demo?.isDemo(inst.url)

/** Adds the demo instance, fresh, unless it is already in the list; returns it. */
export async function addDemoInstance(): Promise<Instance> {
  const existing = store.list.find(isDemo)
  if (existing) return existing
  const demo = instancePlatform().demo
  if (!demo) throw new Error('no demo on this platform')
  const inst = demo.create()
  store.list.push(inst)
  await save()
  return inst
}

/** Name shown for an instance: the demo's follows the interface language, the dashboard's machine says what it is. */
export const displayName = (inst: Instance) =>
  isDemo(inst) ? t('mobile.demo.name') : inst.home ? inst.name || t('instances.home') : inst.name
/** Address shown for an instance: the demo has none, it says its data is simulated. */
export const displayAddress = (inst: Instance) => (isDemo(inst) ? t('mobile.demo.address') : inst.url.replace('https://', ''))

export const newId = () => Math.random().toString(36).slice(2, 10)

/** Default port of the remote access (DASHBOARD_REMOTE_PORT). */
export const DEFAULT_REMOTE_PORT = 9443

/**
 * Typed address → its host and, when one was typed with it, its port. Accepts « 192.168.1.42 », « mac.local:9443 »,
 * an IPv6 address (« fe80::1 », « [fe80::1]:9443 ») or a pasted URL, whose scheme and path are dropped.
 */
export function splitAddress(input: string): { host: string; port: string } {
  const s = input.trim().replace(/^[a-z][a-z0-9+.-]*:\/\//i, '').replace(/[/?#].*$/, '')
  const v6 = /^\[([^\]]*)\](?::(\d*))?$/.exec(s)
  if (v6) return { host: v6[1]!, port: v6[2] ?? '' }
  // Several colons without brackets: an IPv6 address, without a port.
  const parts = s.split(':')
  if (parts.length === 2) return { host: parts[0]!, port: parts[1]! }
  return { host: s, port: '' }
}

/** Host and port of a remote access → its HTTPS origin (the only scheme of the remote access). */
export function remoteOrigin(hostInput: string, portInput: string): string {
  const { host, port: typed } = splitAddress(hostInput)
  if (!host) throw new Error(t('mobile.form.errors.addressRequired'))
  const port = typed || portInput.trim() || String(DEFAULT_REMOTE_PORT)
  if (!/^\d{1,5}$/.test(port) || +port < 1 || +port > 65535) throw new Error(t('mobile.form.errors.invalidPort'))
  try {
    // An explicit :443 (tunnel, public name) gives the origin without a port.
    return new URL(`https://${host.includes(':') ? `[${host}]` : host}:${port}`).origin
  } catch {
    throw new Error(t('mobile.form.errors.invalidAddress'))
  }
}

/** Address placeholder when the device's network is unknown. */
export const ADDRESS_PLACEHOLDER = '192.168.1.x'

/**
 * Address placeholder on the device's network: the bytes its prefix covers kept, the others written x
 * (192.168.0.23/24 → 192.168.0.x, 10.20.3.4/16 → 10.20.x.x, /20 → 192.168.x.x). At least the last byte stays x;
 * null for a network wider than a /8 or an address that is not a private IPv4 one.
 */
export function subnetPlaceholder({ ip, prefix }: LocalNetwork): string | null {
  const bytes = ip.split('.').map(Number)
  if (bytes.length !== 4 || bytes.some((b) => !Number.isInteger(b) || b < 0 || b > 255)) return null
  const [a, b] = bytes as [number, number]
  if (!(a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168))) return null
  const kept = Math.min(3, Math.floor(prefix / 8))
  if (kept < 1) return null
  return bytes.map((x, i) => (i < kept ? String(x) : 'x')).join('.')
}

/** Origin of a remote access → host and port, for the form (443 when the origin has none). */
export function originParts(origin: string): { host: string; port: string } {
  try {
    const u = new URL(origin)
    return { host: u.hostname.replace(/^\[(.*)\]$/, '$1'), port: u.port || '443' }
  } catch {
    return splitAddress(origin)
  }
}

// ---------- Instance open in the dashboard ----------
/**
 * Kept in memory only: a cold start of the mobile app opens the instance list, the dashboard opens its own machine.
 */
export const current = reactive<{ id: string | null }>({ id: null })

export function setCurrent(id: string | null) {
  current.id = id
}

export const currentInstance = () => (current.id ? findInstance(current.id) : undefined)

/** Routes of the instance pages, mounted by each application's router next to the dashboard. */
export const INSTANCES_PATH = '/instances'
export const ADD_INSTANCE_PATH = '/instances/ajouter'
export const signInPath = (id: string) => `/instances/${id}/connexion`
