import type { Router } from 'vue-router'
import { App as CapApp } from '@capacitor/app'
import { Capacitor } from '@capacitor/core'

/**
 * Pairing with a QR code. The dashboard of an instance (Remote access) shows a QR code holding a pairing link:
 *
 *   frankenmanager://pair?v=1&url=<https base URL of the remote access>&code=<one-time code>&name=<host name>&ca=<…>
 *
 * `ca` (optional) is the lowercase hex SHA-256 of the mkcert root CA certificate (DER), shown to compare with the CA
 * installed on the phone. The link is scanned from the app (scanPairing) or with the system camera, which opens the
 * app through the `frankenmanager` URL scheme (ios/App/App/Info.plist, android AndroidManifest.xml). Both lead to the
 * pairing page (PairPage.vue), which shows what it is about to trust, then exchanges the code for a session token
 * (POST /api/v1/auth/pair).
 */
export interface PairRequest {
  /** HTTPS origin of the remote access. */
  url: string
  code: string
  /** Host name announced by the instance; empty when absent. */
  name: string
  /** SHA-256 of the root CA (64 lowercase hex characters), or null. */
  ca: string | null
}

export const PAIR_PATH = '/instances/appairer'

/** Parses a pairing link; throws an Error with a message for the user when it is not one. */
export function parsePairLink(text: string): PairRequest {
  const invalid = new Error('Ce QR code n’est pas un code d’appairage FrankenManager.')
  let link: URL
  try {
    link = new URL(text.trim())
  } catch {
    throw invalid
  }
  // frankenmanager://pair?… : "pair" is the host of the link (or its path with some URL parsers).
  const target = link.host || link.pathname.replace(/^\/+/, '')
  if (link.protocol !== 'frankenmanager:' || target !== 'pair') throw invalid
  const q = link.searchParams
  if (q.get('v') !== '1') throw new Error('Ce code d’appairage vient d’une version de FrankenManager que l’app ne gère pas : mettez l’app à jour.')

  const code = q.get('code')?.trim() ?? ''
  if (!code) throw invalid
  let base: URL
  try {
    base = new URL(q.get('url') ?? '')
  } catch {
    throw invalid
  }
  if (base.protocol !== 'https:') throw new Error('Code d’appairage refusé : l’adresse de l’instance n’est pas en HTTPS.')
  if (base.username || base.password) throw invalid

  const ca = q.get('ca')?.trim().toLowerCase() || null
  if (ca && !/^[0-9a-f]{64}$/.test(ca)) throw invalid
  return { url: base.origin, code, name: q.get('name')?.trim().slice(0, 100) ?? '', ca }
}

/** CA fingerprint as shown by most tools: pairs of uppercase hex digits separated by colons. */
export const formatFingerprint = (hex: string) => hex.toUpperCase().match(/.{2}/g)!.join(':')

/** Opens the pairing page for a link. */
export function openPairing(router: Router, link: string) {
  return router.push({ path: PAIR_PATH, query: { link } })
}

/**
 * Opens the camera on a full-screen native scanner (@capacitor/barcode-scanner) and resolves the scanned text, or null when
 * the user cancels.
 */
export async function scanPairing(): Promise<string | null> {
  // Loaded on first use: the plugin brings a web scanner (html5-qrcode) the app does not need at startup.
  const { CapacitorBarcodeScanner, CapacitorBarcodeScannerTypeHint } = await import('@capacitor/barcode-scanner')
  try {
    const { ScanResult } = await CapacitorBarcodeScanner.scanBarcode({
      hint: CapacitorBarcodeScannerTypeHint.QR_CODE,
      scanInstructions: 'Visez le QR code d’appairage affiché par le dashboard (Accès distant).',
      cancelButtonAccessibilityLabel: 'Annuler',
      torchButtonOnAccessibilityLabel: 'Éteindre la lampe',
      torchButtonOffAccessibilityLabel: 'Allumer la lampe',
    })
    return ScanResult || null
  } catch (e) {
    // Plugin error codes (OS-PLUG-BARC-xxxx): 6 cancelled by the user, nothing to report; 7 camera access refused.
    const { code, message = String(e) } = (e ?? {}) as { code?: string; message?: string }
    if (code === 'OS-PLUG-BARC-0006' || /cancel/i.test(message)) return null
    if (code === 'OS-PLUG-BARC-0007' || /permission/i.test(message)) throw new Error('L’app n’a pas accès à l’appareil photo : autorisez-le dans les réglages du téléphone.')
    throw new Error(`Scan impossible : ${message}`)
  }
}

/** Links already handled: a link may come both as the launch URL and as an appUrlOpen event. */
const handled = new Set<string>()

function handle(router: Router, url: string | undefined) {
  if (!url || !/^frankenmanager:/i.test(url) || handled.has(url)) return
  handled.add(url)
  void openPairing(router, url)
}

/** Follows the pairing links that open the app (system camera, link in another app). */
export async function listenPairingLinks(router: Router) {
  if (!Capacitor.isNativePlatform()) return
  await CapApp.addListener('appUrlOpen', ({ url }) => handle(router, url))
  const launch = await CapApp.getLaunchUrl().catch(() => undefined)
  handle(router, launch?.url)
}
