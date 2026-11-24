import { reactive } from 'vue'
import { connect, disconnect } from '@app/composables/useFm'
import { setUnauthorizedHandler, type Instance } from '@app/lib/api'

/**
 * Connexion du dashboard embarqué à son daemon, d'après /config.json (servi par le daemon avec les pages) :
 *
 * - local (https://frankenmanager.test, https://<alias>.test, https://127.0.0.1:DASHBOARD_HTTPS_PORT) : la page
 *   ouvre une session sans mot de passe sur l'écouteur HTTPS de 127.0.0.1, que seul un navigateur de cette machine
 *   atteint (POST /api/v1/auth/local), sauf avec DASHBOARD_LOCAL_AUTH ;
 * - remote (l'accès distant ouvert dans un navigateur) : le mot de passe du dashboard.
 *
 * Le jeton est gardé dans ce navigateur (localStorage de cette origine) : une nouvelle visite reprend la même session
 * tant que le daemon la reconnaît (30 jours sans usage au plus).
 */
interface Config {
  /** Adresse de l'API ; vide : l'origine de la page. */
  api: string
  mode: 'local' | 'remote'
  version: string
}

const KEY = 'fm-dashboard-session'
const read = () => { try { return localStorage.getItem(KEY) ?? '' } catch { return '' } }
const write = (token: string) => {
  try { if (token) localStorage.setItem(KEY, token); else localStorage.removeItem(KEY) } catch { /* stockage indisponible */ }
}

export const session = reactive({
  phase: 'loading' as 'loading' | 'login' | 'ready' | 'error',
  mode: 'local' as Config['mode'],
  /** Adresse de l'API. */
  base: '',
  error: '',
})

const bearer = (token: string): Record<string, string> => (token ? { Authorization: `Bearer ${token}` } : {})

/** La session `token` (ou le cookie, sans jeton) est-elle reconnue ? */
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
      // 403 : DASHBOARD_LOCAL_AUTH, le mot de passe est demandé ici aussi
      if (r.status !== 403) throw new Error(`Connexion locale refusée (${r.status}).`)
    }
    session.phase = 'login'
  } catch (e) {
    session.phase = 'error'
    session.error = e instanceof Error ? e.message : String(e)
  }
}

/** Connexion avec le mot de passe du dashboard ; renvoie l'erreur, null si elle a réussi. */
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

// Session révoquée ou expirée : on oublie le jeton et on recommence (nouvelle session locale, ou mot de passe).
setUnauthorizedHandler(() => {
  write('')
  disconnect()
  void boot()
})
