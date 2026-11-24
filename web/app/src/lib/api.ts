import type { components, paths } from './api.gen'
import { t } from '../i18n'

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

// ---------- Daemon API types (generated from api/openapi.yaml: pnpm --dir web gen:api) ----------

/** A schema of the spec, by name. */
export type Schema<N extends keyof components['schemas']> = components['schemas'][N]

type Method = 'get' | 'post' | 'put' | 'delete'
type JsonBody<R> = R extends { content: { 'application/json': infer B } } ? B : never
/** JSON body of the success response (200, 201 or 202) of a route of the spec, e.g. `ApiResponse<'/api/v1/redis'>`. */
export type ApiResponse<P extends keyof paths, M extends Method = 'get'> =
  paths[P][M] extends { responses: infer R } ? JsonBody<R[Extract<keyof R, 200 | 201 | 202>]> : never

export type HostInfo = Schema<'HostInfo'>
export type Host = Schema<'Host'>
export type Service = Schema<'Service'>
export type Status = Schema<'Status'>
export type Container = Schema<'Container'>
export type Stat = Schema<'Stat'>
export type OpEvent = Schema<'Event'>
export type Operation = Schema<'Operation'>
export type LogLine = Schema<'LogLine'>

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
export const unreachableMessage = () => t('errors.unreachable', { setting: 'DASHBOARD_ACCESS=lan' })

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
    throw new ApiError(0, unreachableMessage())
  }
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new ApiError(res.status, (data && data.error) || t('errors.http', { status: res.status }))
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
        const password = await confirmer(opts.action ?? t('store.confirmAction'))
        if (!password) throw new ApiError(428, t('errors.cancelled'))
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
        throw new ApiError(0, unreachableMessage())
      }
      const data = await res.json().catch(() => null)
      if (!res.ok) throw new ApiError(res.status, (data && data.error) || t('errors.http', { status: res.status }))
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
        if (Date.now() > deadline) throw new ApiError(0, t('errors.operationTooLong'))
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
export const health = (url: string) => send<ApiResponse<'/api/health'>>(url, null, 'GET', '/api/health', { timeout: 8_000 })

/** Opens a session with the dashboard password: the daemon returns a (Bearer) token for the app. */
export const login = (url: string, password: string, device: string) =>
  send<ApiResponse<'/api/v1/auth/login', 'post'>>(url, null, 'POST', '/api/v1/auth/login', { body: { password, device } })

/**
 * Exchanges a one-time pairing code (QR code of the dashboard) for a session token, as a sign-in does.
 * 401: invalid, expired or already used code; 429: too many attempts.
 */
export const pair = (url: string, code: string, device: string) =>
  send<ApiResponse<'/api/v1/auth/pair', 'post'>>(url, null, 'POST', '/api/v1/auth/pair', { body: { code, device }, timeout: 10_000 })

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
