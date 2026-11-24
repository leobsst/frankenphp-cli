import { ApiError, registerClientScheme, type Client, type Operation } from '@app/lib/api'
import { t } from '@app/i18n'
import { handle, openStream, upload } from './server'

/**
 * Demo mode: an instance whose address uses the reserved `demo:` scheme is served by an in-memory fake of the daemon
 * (./server.ts) instead of the network. The seam is the API client: `client(inst)` returns the demo client for such
 * an address, so the store (web/app/src/composables/useFm.ts) and every page run unchanged. Nothing is sent anywhere
 * and the demo instance has no token.
 */
export const DEMO_SCHEME = 'demo:'
export const DEMO_URL = 'demo://frankenmanager'
export const DEMO_ID = 'demo'

export const isDemoUrl = (url: string) => url.toLowerCase().startsWith(DEMO_SCHEME)

/** Same surface as the HTTP client; destructive actions never ask for a password. */
export function demoClient(): Client {
  const request = <T>(method: string, path: string, body?: unknown) => handle(method, path, body) as Promise<T>

  async function wait(op: Operation, onProgress?: (op: Operation) => void): Promise<Operation> {
    const deadline = Date.now() + 60_000
    let cur = op
    while (cur.status === 'queued' || cur.status === 'running') {
      if (Date.now() > deadline) throw new ApiError(0, t('errors.operationTooLong'))
      await new Promise((r) => setTimeout(r, 400))
      cur = await request<Operation>('GET', `/api/v1/operations/${cur.id}`)
      onProgress?.(cur)
    }
    return cur
  }

  return {
    get: <T>(path: string) => request<T>('GET', path),
    post: <T = Operation>(path: string, body: unknown = {}) => request<T>('POST', path, body),
    put: <T = Operation>(path: string, body: unknown) => request<T>('PUT', path, body),
    del: <T = unknown>(path: string) => request<T>('DELETE', path),
    upload: async (path: string, file: Blob) => upload(path, file),
    // A download (S3 object) has nothing to fetch: a small text file stands for it.
    streamUrl: (path: string, params: Record<string, string | string[]> = {}) =>
      `data:text/plain;charset=utf-8,${encodeURIComponent(`FrankenManager demo\n${path} ${JSON.stringify(params)}\n`)}`,
    stream: (path: string, params: Record<string, string | string[]> = {}) => openStream(path, params),
    wait,
  }
}

/** Routes the `demo:` addresses to the demo client (main.ts, before anything connects). */
export function registerDemo() {
  registerClientScheme(DEMO_SCHEME, () => demoClient())
}
