/**
 * Instance pilotée : l'adresse de son API et le jeton de session envoyé en Bearer. Sans jeton, la session est le
 * cookie du navigateur (accès distant ouvert dans un navigateur, sur la même origine que l'API).
 */
export interface Instance {
  id: string
  url: string
  token: string
}

/** Appelé quand le daemon refuse la session (révoquée, expirée, mot de passe changé) : l'hôte reconnecte. */
let unauthorized: (inst: Instance) => void = () => {}
export function setUnauthorizedHandler(fn: (inst: Instance) => void) {
  unauthorized = fn
}

// ---------- Types de l'API du daemon (api/openapi.yaml, /api/v1) ----------

export interface HostInfo {
  hostname: string
  os: string
  arch: string
  version: string
  lan_ip?: string
  remote: boolean
  mode: string
  remote_url?: string
}
export interface Host {
  name: string
  kind: 'php' | 'proxy' | 'static' | 'alias'
  php?: string
  target?: string
  root?: string
  spa?: boolean
  websocket?: boolean
}
export interface Service {
  name: string
  setting: string
  enabled: boolean
  state: string
}
export interface Status {
  running: boolean
  hosts: Host[]
  services: Service[]
}
export interface Container {
  name: string
  state: string
  health: string
}
export interface Stat {
  name: string
  cpu_percent: number
  mem_percent: number
  mem_usage: string
  net_io: string
  block_io: string
}
export interface OpEvent {
  kind: 'message' | 'step' | 'break' | 'output'
  level: 'info' | 'success' | 'warning' | 'error'
  message?: string
}
export interface Operation {
  id: string
  kind: string
  status: 'queued' | 'running' | 'succeeded' | 'failed'
  error?: string
  events: OpEvent[]
}
export interface LogLine {
  container: string
  time: string
  line: string
}

// ---------- Erreurs ----------

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
}

/** Message d'une requête qui n'a pas abouti : le plus souvent certificat non approuvé ou accès distant fermé. */
export const UNREACHABLE =
  'Instance injoignable. Vérifiez que le daemon FrankenManager tourne ; depuis un autre appareil, que l’accès distant ' +
  'est ouvert (DASHBOARD_ACCESS=lan) et que la CA de FrankenManager y est installée et approuvée.'

// ---------- Confirmation du mot de passe ----------

/**
 * À distance, une action destructive (arrêter la stack, un conteneur…) redemande le mot de passe du dashboard :
 * le daemon répond 428, l'app le demande puis rejoue la requête avec l'en-tête X-FM-Password.
 */
let confirmer: (action: string) => Promise<string | null> = async () => null
export function setConfirmer(fn: typeof confirmer) {
  confirmer = fn
}

// ---------- Requêtes ----------

interface RequestOptions {
  body?: unknown
  password?: string
  timeout?: number
  /** Libellé de l'action, montré dans la demande de mot de passe. */
  action?: string
}

async function send<T>(base: string, token: string | null, method: string, path: string, opts: RequestOptions = {}) {
  const headers: Record<string, string> = {}
  if (token) headers.Authorization = `Bearer ${token}`
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json'
  if (opts.password) headers['X-FM-Password'] = opts.password
  let res: Response
  try {
    res = await fetch(base + path, {
      method,
      headers,
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
      signal: AbortSignal.timeout(opts.timeout ?? 15_000),
    })
  } catch {
    throw new ApiError(0, UNREACHABLE)
  }
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new ApiError(res.status, (data && data.error) || `Erreur ${res.status}`)
  return data as T
}

/** Client d'une instance connectée : jeton Bearer, confirmation du mot de passe, session expirée. */
export function client(inst: Instance) {
  async function request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
    try {
      return await send<T>(inst.url, inst.token, method, path, opts)
    } catch (e) {
      if (!(e instanceof ApiError)) throw e
      if (e.status === 401) {
        unauthorized(inst)
      }
      if (e.status === 428 && !opts.password) {
        const password = await confirmer(opts.action ?? 'Confirmer cette action')
        if (!password) throw new ApiError(428, 'Action annulée.')
        return request<T>(method, path, { ...opts, password })
      }
      throw e
    }
  }

  return {
    get: <T>(path: string) => request<T>('GET', path),
    /** `opts.password` : mot de passe déjà saisi pour une action destructive ; sans lui, demandé sur la réponse 428. */
    post: <T = Operation>(path: string, body: unknown = {}, opts: { action?: string; password?: string } = {}) => request<T>('POST', path, { body, ...opts }),
    put: <T = Operation>(path: string, body: unknown, opts: { action?: string; password?: string } = {}) => request<T>('PUT', path, { body, ...opts }),
    del: <T = unknown>(path: string, opts: { action?: string; password?: string } = {}) => request<T>('DELETE', path, opts),
    /** Envoie un fichier tel quel (téléversement S3), avec le jeton. */
    async upload(path: string, file: Blob) {
      let res: Response
      try {
        res = await fetch(inst.url + path, { method: 'PUT', headers: { ...(inst.token ? { Authorization: `Bearer ${inst.token}` } : {}), 'Content-Type': file.type || 'application/octet-stream' }, body: file })
      } catch {
        throw new ApiError(0, UNREACHABLE)
      }
      const data = await res.json().catch(() => null)
      if (!res.ok) throw new ApiError(res.status, (data && data.error) || `Erreur ${res.status}`)
      return data as Operation
    },

    /** Adresse d'un flux SSE : EventSource n'envoie pas d'en-tête, le jeton passe dans la requête. */
    streamUrl(path: string, params: Record<string, string | string[]> = {}) {
      const u = new URL(inst.url + path)
      for (const [k, v] of Object.entries(params)) for (const x of [v].flat()) u.searchParams.append(k, x)
      if (inst.token) u.searchParams.set('access_token', inst.token)
      return u.toString()
    },

    /** Suit une opération mise en file (réponse 202) jusqu'à sa fin. */
    async wait(op: Operation, onProgress?: (op: Operation) => void): Promise<Operation> {
      const deadline = Date.now() + 15 * 60_000
      let cur = op
      while (cur.status === 'queued' || cur.status === 'running') {
        if (Date.now() > deadline) throw new ApiError(0, 'L’opération dure trop longtemps.')
        await new Promise((r) => setTimeout(r, 800))
        cur = await request<Operation>('GET', `/api/v1/operations/${cur.id}`)
        onProgress?.(cur)
      }
      return cur
    },
  }
}
export type Client = ReturnType<typeof client>

/** Version du daemon (route publique) : vérifie que l'adresse répond avant de demander le mot de passe. */
export const health = (url: string) => send<{ version: string }>(url, null, 'GET', '/api/health', { timeout: 8_000 })

/** Ouvre une session avec le mot de passe du dashboard : le daemon rend un jeton (Bearer) pour l'app. */
export const login = (url: string, password: string, device: string) =>
  send<{ token: string; session: { id: string } }>(url, null, 'POST', '/api/v1/auth/login', { body: { password, device } })

export const hostInfo = (url: string, token: string) => send<HostInfo>(url, token, 'GET', '/api/v1/host', { timeout: 8_000 })

/** Ferme la session côté daemon ; sans effet si l'instance est injoignable. */
export async function logout(inst: Instance) {
  await send(inst.url, inst.token, 'POST', '/api/v1/auth/logout', { body: {}, timeout: 5_000 }).catch(() => {})
}

/** Dernier message utile d'une opération, pour les notifications. */
export function lastMessage(op: Operation): string | undefined {
  for (let i = op.events.length - 1; i >= 0; i--) {
    const e = op.events[i]!
    if (e.message && e.kind !== 'output') return e.message
  }
}
