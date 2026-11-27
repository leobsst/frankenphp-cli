import { t } from '@app/i18n'
import { clientNetwork } from '@app/lib/api'
import { homeInstance, type Instance, type InstancePlatform } from '@app/lib/instances'
import { session } from './session'

/**
 * Storage of the other instances this dashboard manages (the list and its pages are shared with the mobile app:
 * web/app/src/lib/instances.ts): the browser's localStorage of this origin, tokens included, where the page already
 * keeps its own session (session.ts). Each origin of the dashboard (remote access, https://frankenmanager.test) has
 * its own list.
 */
const KEY = 'fm-instances'

function load(): Promise<Instance[]> {
  let list: Instance[] = []
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(KEY) ?? '[]')
    if (Array.isArray(parsed)) list = parsed as Instance[]
  } catch {
    // Storage unavailable or unreadable: an empty list.
  }
  // No token (storage partly cleared): the instance stays listed, to sign in again.
  return Promise.resolve(list.map((i) => ({ ...i, token: i.token ?? '', expired: i.expired || !i.token })))
}

function save(list: Instance[]): Promise<void> {
  try {
    localStorage.setItem(KEY, JSON.stringify(list))
  } catch {
    // Storage unavailable (private browsing): the list lasts as long as the page.
  }
  return Promise.resolve()
}

const HOME_STATE = { loading: 'loading', ready: 'ready', login: 'signIn', error: 'offline' } as const

export const webPlatform: InstancePlatform = {
  kind: 'web',
  load,
  save,
  // Name of the session on each instance (Remote access › Devices): this dashboard, by its address.
  deviceName: () => Promise.resolve(t('instances.webDevice', { host: location.host })),
  // This machine signs in with the page (session.ts): its sign-in screen, or the error screen of its daemon.
  homeState: () => HOME_STATE[session.phase],
  // A browser does not tell its local address: the daemon of this machine says which one it sees for this device.
  async localNetwork() {
    const home = homeInstance()
    if (!home || session.phase !== 'ready') return null
    const net = await clientNetwork(home.url, home.token)
    return net.ip && net.prefix ? { ip: net.ip, prefix: net.prefix } : null
  },
}
