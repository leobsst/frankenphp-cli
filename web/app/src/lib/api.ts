/**
 * Managed instance: its API address and the session token sent as Bearer. Without a token, the session is the
 * browser cookie (remote access opened in a browser, on the same origin as the API).
 */
export interface Instance {
  id: string
  url: string
  token: string
}

/** Called when the daemon rejects the session (revoked, expired, password changed): the host reconnects. */
let unauthorized: (inst: Instance) => void = () => {}
export function setUnauthorizedHandler(fn: (inst: Instance) => void) {
  unauthorized = fn
}

// ---------- Daemon API types (api/openapi.yaml, /api/v1) ----------

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

// ---------- Errors ----------

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
}

/** Message for a request that did not go through: most often an untrusted certificate or closed remote access. */
export const UNREACHABLE =
  'Instance injoignable. Vérifiez que le daemon FrankenManager tourne ; depuis un autre appareil, que l’accès distant ' +
  'est ouvert (DASHBOARD_ACCESS=lan) et que la CA de FrankenManager y est installée et approuvée.'

// ---------- Password confirmation ----------

/**
 * Remotely, a destructive action (stopping the stack, a container…) asks for the dashboard password again: the
 * daemon responds 428, the app asks for it then replays the request with the X-FM-Password header.
 */
let confirmer: (action: string) => Promise<string | null> = async () => null
export function setConfirmer(fn: typeof confirmer) {
  confirmer = fn
}

// ---------- Requests ----------

interface RequestOptions {
  body?: unknown
  password?: string
  timeout?: number
  /** Action label, shown in the password prompt. */
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

/** Client of a connected instance: Bearer token, password confirmation, expired session. */
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
    /** `opts.password`: password already entered for a destructive action; without it, asked for on the 428 response. */
    post: <T = Operation>(path: string, body: unknown = {}, opts: { action?: string; password?: string } = {}) => request<T>('POST', path, { body, ...opts }),
    put: <T = Operation>(path: string, body: unknown, opts: { action?: string; password?: string } = {}) => request<T>('PUT', path, { body, ...opts }),
    del: <T = unknown>(path: string, opts: { action?: string; password?: string } = {}) => request<T>('DELETE', path, opts),
    /** Sends a file as is (S3 upload), with the token. */
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

    /** URL of an SSE stream: EventSource sends no headers, the token goes in the query string. */
    streamUrl(path: string, params: Record<string, string | string[]> = {}) {
      const u = new URL(inst.url + path)
      for (const [k, v] of Object.entries(params)) for (const x of [v].flat()) u.searchParams.append(k, x)
      if (inst.token) u.searchParams.set('access_token', inst.token)
      return u.toString()
    },

    /** Follows a queued operation (202 response) until it ends. */
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

/** Daemon version (public route): checks that the address responds before asking for the password. */
export const health = (url: string) => send<{ version: string }>(url, null, 'GET', '/api/health', { timeout: 8_000 })

/** Opens a session with the dashboard password: the daemon returns a (Bearer) token for the app. */
export const login = (url: string, password: string, device: string) =>
  send<{ token: string; session: { id: string } }>(url, null, 'POST', '/api/v1/auth/login', { body: { password, device } })

export const hostInfo = (url: string, token: string) => send<HostInfo>(url, token, 'GET', '/api/v1/host', { timeout: 8_000 })

/** Closes the session on the daemon side; no effect if the instance is unreachable. */
export async function logout(inst: Instance) {
  await send(inst.url, inst.token, 'POST', '/api/v1/auth/logout', { body: {}, timeout: 5_000 }).catch(() => {})
}

/** Last useful message of an operation, for notifications. */
export function lastMessage(op: Operation): string | undefined {
  for (let i = op.events.length - 1; i >= 0; i--) {
    const e = op.events[i]!
    if (e.message && e.kind !== 'output') return e.message
  }
}
