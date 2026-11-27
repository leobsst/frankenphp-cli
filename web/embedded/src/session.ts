import { reactive } from 'vue'
import { connect, disconnect, reconnect } from '@app/composables/useFm'
import { setUnauthorizedHandler } from '@app/lib/api'
import { homeInstance, patchInstance, setCurrent, setHomeInstance, type Instance } from '@app/lib/instances'
import { t } from '@app/i18n'

/**
 * Connection of the embedded dashboard to its daemon, based on /config.json (served by the daemon with the pages):
 *
 * - local (https://frankenmanager.test, https://<alias>.test, https://127.0.0.1:DASHBOARD_HTTPS_PORT): the page
 *   opens a passwordless session on the 127.0.0.1 HTTPS listener, which only a browser on this machine can reach
 *   (POST /api/v1/auth/local), except with DASHBOARD_LOCAL_AUTH;
 * - remote (remote access opened in a browser): the dashboard password.
 *
 * The token is kept in this browser (localStorage of this origin): a new visit resumes the same session as long as
 * the daemon recognizes it (30 days without use at most).
 *
 * This machine is the first instance of the list (`home`, see @app/lib/instances), the one the dashboard opens on;
 * the others are added from the instance list, like in the mobile app, and reached through their remote access.
 */
interface Config {
  /** API address; empty: the page's origin. */
  api: string
  mode: 'local' | 'remote'
  version: string
}

/** Id of this machine in the instance list. */
export const HOME_ID = 'dashboard'

const KEY = 'fm-dashboard-session'
const read = () => { try { return localStorage.getItem(KEY) ?? '' } catch { return '' } }
const write = (token: string) => {
  try { if (token) localStorage.setItem(KEY, token); else localStorage.removeItem(KEY) } catch { /* storage unavailable */ }
}

export const session = reactive({
  phase: 'loading' as 'loading' | 'login' | 'ready' | 'error',
  mode: 'local' as Config['mode'],
  /** API address. */
  base: '',
  error: '',
})

const bearer = (token: string): Record<string, string> => (token ? { Authorization: `Bearer ${token}` } : {})

/** This machine in the instance list, with the token of its session (empty: the cookie, or not signed in). */
function setHome(token: string) {
  setHomeInstance({ id: HOME_ID, name: '', url: session.base || location.origin, token, session: '', added: 0 })
}
// The dashboard opens on this machine.
setHome('')
setCurrent(HOME_ID)

/** Automatic retry of boot() while the daemon does not answer: 2 s, 4 s… 30 s. */
let bootRetry: ReturnType<typeof setTimeout> | undefined
let bootDelay = 0

/** Is the `token` session (or the cookie, without a token) recognized? */
async function recognized(token: string) {
  const r = await fetch(`${session.base}/api/v1/auth`, { headers: bearer(token) })
  if (!r.ok) return false
  const a = (await r.json()) as { session_required: boolean; authenticated: boolean }
  return !a.session_required || a.authenticated
}

function start(token: string) {
  bootDelay = 0
  setHome(token)
  session.phase = 'ready'
}

// ---------- Connection of the dashboard ----------
/** Instance the dashboard is connected to (DashboardLayout), null on the instance pages. */
let connected: string | null = null

/** Connects the dashboard to an instance: this machine as its page signed in (local or remote), the others remotely. */
export function openInstance(inst: Instance) {
  connected = inst.id
  const mode = inst.home ? session.mode : 'remote'
  connect(inst, { mode, client: mode === 'local' ? t('app.client.local') : t('app.client.remote') })
}

export function closeInstance() {
  connected = null
  disconnect()
}

/**
 * Opens a passwordless session on this machine and keeps its token; null when the password is required here too
 * (403, DASHBOARD_LOCAL_AUTH).
 */
async function localSession(): Promise<string | null> {
  const r = await fetch(`${session.base}/api/v1/auth/local`, { method: 'POST' })
  if (r.ok) {
    const { token } = (await r.json()) as { token: string }
    write(token)
    return token
  }
  if (r.status === 403) return null
  throw new Error(t('app.errors.localRefused', { status: r.status }))
}

export async function boot() {
  clearTimeout(bootRetry)
  // While retrying, the error screen stays up instead of blinking with the loader.
  if (session.phase !== 'error') session.phase = 'loading'
  try {
    const res = await fetch('config.json', { cache: 'no-store' })
    if (!res.ok) throw new Error(t('app.errors.config', { status: res.status }))
    const cfg = (await res.json()) as Config
    session.mode = cfg.mode
    session.base = (cfg.api || location.origin).replace(/\/$/, '')
    const token = read()
    if (await recognized(token)) return start(token)
    write('')
    if (cfg.mode === 'local') {
      const fresh = await localSession()
      if (fresh) return start(fresh)
    }
    setHome('')
    session.phase = 'login'
    session.error = ''
  } catch (e) {
    session.phase = 'error'
    session.error = e instanceof Error ? e.message : String(e)
    // The daemon is not there (yet): try again, the page recovers without a reload.
    bootDelay = Math.min(bootDelay ? bootDelay * 2 : 2_000, 30_000)
    bootRetry = setTimeout(() => void boot(), bootDelay)
  }
}

/** Sign-in with the dashboard password; returns the error, null if it succeeded. */
export async function signIn(password: string): Promise<string | null> {
  const r = await fetch(`${session.base}/api/v1/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password, device: navigator.userAgent.slice(0, 200) }),
  })
  if (!r.ok) {
    const body = (await r.json().catch(() => null)) as { error?: string } | null
    return body?.error ?? t('signIn.error', { status: r.status })
  }
  const { token } = (await r.json()) as { token: string }
  write(token)
  start(token)
  return null
}

/**
 * Session revoked, expired, or lost by a restarted daemon.
 *
 * - Another instance: it is marked, DashboardLayout then leads to its sign-in, and the list shows it.
 * - This machine: forget the token. On this machine, a new local session is opened in place, keeping the page as
 *   is; otherwise (or when it fails), start over (password, or error screen).
 */
let resigning = false
setUnauthorizedHandler(async (inst) => {
  if (inst.id !== HOME_ID) {
    void patchInstance(inst.id, { expired: true })
    return
  }
  // Requests and streams sent in parallel fail together: one re-sign-in, and none for an already replaced token.
  if (resigning || inst.token !== homeInstance()?.token) return
  resigning = true
  try {
    write('')
    if (session.mode === 'local') {
      const fresh = await localSession().catch(() => null)
      if (fresh) {
        setHome(fresh)
        if (connected === HOME_ID) reconnect(homeInstance()!)
        return
      }
    }
    setHome('')
    if (connected === HOME_ID) closeInstance()
    await boot()
  } finally {
    resigning = false
  }
})
