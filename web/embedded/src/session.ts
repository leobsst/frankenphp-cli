import { reactive } from 'vue'
import { connect, disconnect, reconnect } from '@app/composables/useFm'
import { setUnauthorizedHandler, type Instance } from '@app/lib/api'
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
 */
interface Config {
  /** API address; empty: the page's origin. */
  api: string
  mode: 'local' | 'remote'
  version: string
}

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

/** Token of the session in use: a 401 on an older one (request sent before a re-sign-in) is ignored. */
let current = ''
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

const instance = (token: string): Instance => ({ id: 'dashboard', url: session.base, token })

function start(token: string) {
  const inst = instance(token)
  current = token
  bootDelay = 0
  connect(inst, { mode: session.mode, client: session.mode === 'local' ? t('app.client.local') : t('app.client.remote') })
  session.phase = 'ready'
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
 * Session revoked, expired, or lost by a restarted daemon: forget the token. On this machine, a new local session is
 * opened in place, keeping the page as is; otherwise (or when it fails), start over (password, or error screen).
 */
let resigning = false
setUnauthorizedHandler(async (inst) => {
  // Requests and streams sent in parallel fail together: one re-sign-in, and none for an already replaced token.
  if (resigning || inst.token !== current) return
  resigning = true
  try {
    write('')
    if (session.mode === 'local') {
      const fresh = await localSession().catch(() => null)
      if (fresh) {
        current = fresh
        reconnect(instance(fresh))
        return
      }
    }
    current = ''
    disconnect()
    await boot()
  } finally {
    resigning = false
  }
})
