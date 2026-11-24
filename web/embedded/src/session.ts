import { reactive } from 'vue'
import { connect, disconnect } from '@app/composables/useFm'
import { setUnauthorizedHandler, type Instance } from '@app/lib/api'

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

/** Is the `token` session (or the cookie, without a token) recognized? */
async function recognized(token: string) {
  const r = await fetch(`${session.base}/api/v1/auth`, { headers: bearer(token) })
  if (!r.ok) return false
  const a = (await r.json()) as { session_required: boolean; authenticated: boolean }
  return !a.session_required || a.authenticated
}

function start(token: string) {
  const inst: Instance = { id: 'dashboard', url: session.base, token }
  connect(inst, { mode: session.mode, client: session.mode === 'local' ? 'Navigateur de cette machine' : 'Navigateur' })
  session.phase = 'ready'
}

export async function boot() {
  session.phase = 'loading'
  session.error = ''
  try {
    const res = await fetch('config.json', { cache: 'no-store' })
    if (!res.ok) throw new Error(`config.json : ${res.status}`)
    const cfg = (await res.json()) as Config
    session.mode = cfg.mode
    session.base = (cfg.api || location.origin).replace(/\/$/, '')
    const token = read()
    if (await recognized(token)) return start(token)
    write('')
    if (cfg.mode === 'local') {
      const r = await fetch(`${session.base}/api/v1/auth/local`, { method: 'POST' })
      if (r.ok) {
        const { token: fresh } = (await r.json()) as { token: string }
        write(fresh)
        return start(fresh)
      }
      // 403: DASHBOARD_LOCAL_AUTH, the password is required here too
      if (r.status !== 403) throw new Error(`Connexion locale refusée (${r.status}).`)
    }
    session.phase = 'login'
  } catch (e) {
    session.phase = 'error'
    session.error = e instanceof Error ? e.message : String(e)
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
    return body?.error ?? `Erreur ${r.status}`
  }
  const { token } = (await r.json()) as { token: string }
  write(token)
  start(token)
  return null
}

// Session revoked or expired: forget the token and start over (new local session, or password).
setUnauthorizedHandler(() => {
  write('')
  disconnect()
  void boot()
})
