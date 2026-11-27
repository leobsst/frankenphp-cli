import { Capacitor, registerPlugin } from '@capacitor/core'
import { t } from '@app/i18n'
import type { LocalNetwork } from '@app/lib/instances'

/**
 * Local network of the phone (ios/App/App/LocalNetworkPlugin.swift, android/…/LocalNetworkPlugin.java):
 *
 * - its private IPv4 network, for the address placeholder of the instance form (a WebView does not tell its local
 *   address);
 * - the access to it. iOS asks the user by itself on the first connection to a device of the local network
 *   (NSLocalNetworkUsageDescription, Info.plist). Android 17 blocks it for an app targeting API 37 until the user
 *   allows it: the app asks before its first request to such an address. Refused, the « unreachable » message says
 *   where to allow it.
 */
type Access = 'granted' | 'denied' | 'prompt' | 'prompt-with-rationale'
interface LocalNetworkPlugin {
  ipv4(): Promise<{ ip?: string; prefix?: number }>
  /** Android only: `granted` while Android does not require the permission (before 17, or a target below 37). */
  requestAccess(): Promise<{ access: Access }>
}
const Plugin = registerPlugin<LocalNetworkPlugin>('LocalNetwork')

export async function localNetwork(): Promise<LocalNetwork | null> {
  if (!Capacitor.isNativePlatform()) return null
  try {
    const { ip, prefix } = await Plugin.ipv4()
    return ip && prefix ? { ip, prefix } : null
  } catch {
    // Native build without the plugin: the default placeholder.
    return null
  }
}

/**
 * The address may be on the local network: a host name (it may resolve to one), a private, link-local or unique
 * local IP. A public IP and Tailscale's 100.64.0.0/10 (a VPN, outside the local network for Android) are not.
 */
export function mayBeLocal(url: string): boolean {
  let host: string
  try {
    host = new URL(url).hostname.replace(/^\[(.*)\]$/, '$1').toLowerCase()
  } catch {
    return false
  }
  const v4 = /^(\d+)\.(\d+)\.\d+\.\d+$/.exec(host)
  if (v4) {
    const [a, b] = [Number(v4[1]), Number(v4[2])]
    return a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168) || (a === 169 && b === 254)
  }
  if (host.includes(':')) return /^(f[cd]|fe[89ab])/.test(host)
  return true
}

/** Access asked on Android, once per launch: Android only shows its prompt twice before refusing for good. */
let asked: Promise<void> | null = null
let denied = false

/** Before a request (api.ts setBeforeRequest): on Android, asks for the local network when it is required. */
export function allowLocalNetwork(url: string): Promise<void> {
  if (Capacitor.getPlatform() !== 'android' || !mayBeLocal(url)) return Promise.resolve()
  asked ??= Plugin.requestAccess().then(
    ({ access }) => { denied = access !== 'granted' },
    // Build targeting API 37 without the permission in its manifest (see app/build.gradle).
    (e) => console.error('LocalNetwork.requestAccess:', e),
  )
  return asked
}

/** Lead added to the « unreachable » message (api.ts setUnreachableHint): where to allow the local network. */
export function localNetworkHint(url: string): string | null {
  if (!mayBeLocal(url)) return null
  if (Capacitor.getPlatform() === 'ios') return t('mobile.errors.localNetworkIos')
  if (Capacitor.getPlatform() === 'android' && denied) return t('mobile.errors.localNetworkAndroid')
  return null
}
