/**
 * Dashboard store: same exports as the POC's simulated store (frankenmanager-gui-poc), but the state comes from the
 * daemon API of the connected instance (`/api/v1`, see api/openapi.yaml), kept up to date by its event stream, and
 * the actions call it. When the daemon stops answering, the store retries with backoff and reopens its streams once
 * it is back; `state.docker` follows whether the daemon reaches Docker.
 */
import { computed, nextTick, reactive, ref } from 'vue'
import { ApiError, client, lastMessage, type Instance, type Client, type Container as ApiContainer, type HostInfo, type Operation, type Stat, type Status, type ApiResponse, type LogLine, type OpEvent, type Schema } from '../lib/api'
import { defaultRoot } from './caddyfile'
import { setLanguage, type LanguagePref } from '../i18n'

export type Php = '8.2' | '8.3' | '8.4' | '8.5'
export const PHP_VERSIONS: Php[] = ['8.5', '8.4', '8.3', '8.2']
export const DEPRECATED_PHP: Php[] = ['8.2']

export type DomainStatus = 'online' | 'offline' | 'restarting'
export interface Domain {
  name: string
  kind: 'php' | 'proxy' | 'alias' | 'static'
  php?: Php
  root?: string
  spa?: boolean
  target?: string
  aliasOf?: string
  websocket?: boolean
  archived: boolean
}

export type ContainerKind = 'php' | 'caddy' | 'redis' | 'soketi' | 's3' | 'mariadb' | 'postgres' | 'admin'
export type ContainerStatus = 'running' | 'stopped' | 'restarting'
export interface Container {
  id: string
  label: string
  port: string
  cpu: number
  /** Memory in MB. */
  mem: number
  status: ContainerStatus
  kind: ContainerKind
  php?: Php
}

export type LogLevel = 'INFO' | 'WARN' | 'ERROR'
export interface LogEntry { id: number; time: string; level: LogLevel; source: string; msg: string }

export type ServiceName = 'dns' | 'redis' | 's3' | 'soketi'
export const SERVICES: Record<ServiceName, { key: string; label: string; desc: string; icon: string; to: string }> = {
  dns: { key: 'DNS_SERVER', label: 'Serveur DNS LAN', desc: 'CoreDNS intégré à FrankenManager · résout les .test pour le réseau local', icon: 'i-lucide-globe', to: '/dns' },
  redis: { key: 'REDIS_ENABLED', label: 'Cache Redis', desc: 'Conteneur franken_redis', icon: 'i-lucide-zap', to: '/redis' },
  s3: { key: 'S3_ENABLED', label: 'Stockage S3 (Garage)', desc: 'Conteneurs franken_s3 et franken_s3_gateway · https://s3.frankenmanager.test', icon: 'i-lucide-hard-drive', to: '/s3' },
  soketi: { key: 'SOKETI_ENABLED', label: 'WebSocket (Soketi)', desc: 'Conteneur franken_soketi · wss://ws.frankenmanager.test', icon: 'i-lucide-radio', to: '/websocket' },
}

export function versionAtLeast(v: string, min: string) {
  const a = v.split('.').map(Number)
  const b = min.split('.').map(Number)
  for (let i = 0; i < 3; i++) if ((a[i] ?? 0) !== (b[i] ?? 0)) return (a[i] ?? 0) > (b[i] ?? 0)
  return true
}

export interface Prefs {
  theme: 'light' | 'dark' | 'system'
  primary: string
  accent: string
  autoRefresh: boolean
  notifications: boolean
  reduceMotion: boolean
  /** Interface language, or the system's (stored per device, like the other preferences). */
  language: LanguagePref
}
export const DEFAULT_PREFS: Prefs = {
  theme: 'system', primary: '#2563EB', accent: '#8B5CF6',
  autoRefresh: true, notifications: true, reduceMotion: false, language: 'system',
}

export const PROJECTS_ROOT = '/projects'
export const S3_DOMAIN = 's3.frankenmanager.test'
export const DOMAIN_RE = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*\.test$/
/** Traffic lines, hidden from recent activity and history. */
export const LIVE_NOISE = /^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TLS|A )\b|"(GET|POST|PUT|PATCH|DELETE) /

export type FieldErrors = Partial<Record<string, string>>
export interface NewProject { name: string; type: 'php' | 'static'; root: string; php: Php; spa: boolean; alias?: string; ssl: boolean }

const blank = () => ({
  stack: 'stopped' as 'running' | 'stopped' | 'starting' | 'stopping',
  docker: true,
  /** First load finished. */
  loaded: false,
  /** Last connection error to the instance, empty if it responds. */
  error: '',
  /** Event stream connected. */
  live: false,
  domains: [] as Domain[],
  containers: [] as Container[],
  logs: [] as LogEntry[],
  cpuHistory: Array(12).fill(0) as number[],
  memGo: 0,
  memTotalGo: 0,
  dns: { enabled: false, status: 'stopped' as 'running' | 'stopped' | 'restarting', ip: '', port: 53, connected: 0, listen: '0.0.0.0', upstream: '' },
  dnsClients: [] as DnsClient[],
  /** caUrl: where the other devices download the root CA while it is shared (daemon's caserver, any path serves it). */
  ssl: { caShared: false, caPort: 0, caUrl: '', regenerating: null as string | null },
  config: {} as Record<string, string>,
  /** Modified keys waiting for a stack restart. */
  configPending: [] as string[],
  redis: { keys: 0, memMB: 0, maxMB: 0, hit: 0, hits: 0, misses: 0, port: 6379, version: '', clients: 0, sample: [] as RedisKey[] },
  databases: [] as Db[],
  dbUsers: [] as DbUser[],
  adminUrl: '',
  s3: { buckets: [] as S3Bucket[], keys: [] as S3Key[], objects: {} as Record<string, S3Object[]>, folders: {} as Record<string, string[]> },
  access: { sessions: [] as Session[], log: [] as AccessEvent[], passwordSet: false, remoteActive: false, remoteUrl: '' },
  /** Site Caddyfiles read by the editor (`GET /api/v1/hosts/{name}/caddyfile`). */
  siteFiles: {} as Record<string, { path: string; content: string; template: string; modified: boolean }>,
  connection: { mode: 'remote' as 'local' | 'remote', host: '', address: '', client: '', signedIn: true },
  app: { version: '', latest: null as string | null },
  prefs: loadPrefs(),
})

function loadPrefs(): Prefs {
  try {
    const p = localStorage.getItem('fm-prefs')
    // `lang` (before the translations, when the choice was only simulated) is dropped: the system language applies.
    const { lang: _lang, ...saved } = p ? JSON.parse(p) : {}
    return { ...DEFAULT_PREFS, ...saved }
  } catch {
    return { ...DEFAULT_PREFS }
  }
}

export const state = reactive(blank())
setLanguage(state.prefs.language)

// ---------- Notifications (wired to Nuxt UI's useToast() in App.vue) ----------
type ToastKind = 'success' | 'error' | 'warning' | 'info'
type Notify = (t: { id?: string; title: string; description?: string; color?: ToastKind | 'neutral'; icon?: string; duration?: number; ui?: Record<string, string> }) => void
let notify: Notify = () => {}
let dismiss: (id: string) => void = () => {}
export function bindNotifier(n: Notify, d: (id: string) => void) { notify = n; dismiss = d }

export function toast(color: ToastKind, title: string, description?: string) {
  if (!state.prefs.notifications && color !== 'error') return
  notify({ title, description, color, icon: TOAST_ICON[color], duration: color === 'error' ? 7000 : 4500 })
}

const TOAST_ICON: Record<ToastKind, string> = { success: 'i-lucide-circle-check', error: 'i-lucide-circle-alert', warning: 'i-lucide-triangle-alert', info: 'i-lucide-info' }

let seq = 0
/**
 * Persistent toast following a daemon operation: each step replaces the description, then it turns into the result
 * in place, with its duration. Replacing it with a new toast overlapped two animations (leave and enter) and made the
 * stack jump.
 */
function progressToast(title: string, description: string) {
  const id = `p-${++seq}`
  const show = (d: string) => notify({ id, title, description: d, color: 'neutral', icon: 'i-lucide-loader-circle', duration: 0, ui: { icon: 'animate-spin' } })
  show(description)
  return {
    step: (d: string) => show(d),
    done: (color: ToastKind, t: string, d: string) => {
      // Notifications turned off (Settings): only a failure stays displayed. Nuxt UI ignores removing a toast updated
      // in the same tick: the removal waits for the next one.
      if (!state.prefs.notifications && color !== 'error') { void nextTick(() => dismiss(id)); return }
      notify({ id, title: t, description: d, color, icon: TOAST_ICON[color], duration: color === 'error' ? 7000 : 4500, ui: { icon: '' } })
    },
  }
}

// ---------- Password for destructive actions ----------
/**
 * Remotely, a destructive action asks for the password again (X-FM-Password). ConfirmModal collects it along with the
 * confirmation: it is kept for the next request. Without it, the API client asks for it on the 428 response.
 */
let pendingPassword = ''
export function checkPassword(password: string) {
  pendingPassword = password
  return true
}
const takePassword = () => {
  const p = pendingPassword
  pendingPassword = ''
  return p || undefined
}

// ---------- Derived ----------
export const active = computed(() => state.domains.filter((d) => !d.archived))
export const archived = computed(() => state.domains.filter((d) => d.archived))
export const running = computed(() => state.containers.filter((c) => c.status === 'running'))
export const phpVersions = computed(() => [...new Set(active.value.filter((d) => d.php && d.kind !== 'alias').map((d) => d.php!))].sort())

export const aliasTarget = (d: Domain) => (d.kind === 'alias' ? state.domains.find((x) => x.name === d.aliasOf) : undefined)
export const isProxied = (d: Domain) => d.kind === 'proxy' || aliasTarget(d)?.kind === 'proxy'
export const envLabel = (d: Domain) => (isProxied(d) ? 'Proxy HTTP' : d.kind === 'static' ? `Statique${d.spa ? ' · SPA' : ''}` : `PHP ${d.php}`)
export const domainIcon = (d: Domain) => (d.kind === 'proxy' ? 'i-lucide-arrow-up-right' : d.kind === 'static' ? 'i-lucide-file-code-2' : 'i-lucide-globe')
export const servedPath = (d: Domain) => `${PROJECTS_ROOT}/${d.root ?? defaultRoot(d.name, d.kind === 'static')}`
/** Only PHP projects and static hosts have their own Caddyfile. */
export const hasSiteCaddyfile = (d: Domain) => d.kind === 'php' || d.kind === 'static'
export const restoreCommand = (d: Domain, php?: Php) => `restore-host ${d.name} ${d.kind === 'static' ? '--static' : `--php ${php ?? d.php}`}`

export function containerFor(d: Domain): Container | undefined {
  return isProxied(d) || d.kind === 'static' ? state.containers.find((c) => c.kind === 'caddy') : state.containers.find((c) => c.php === (aliasTarget(d)?.php ?? d.php))
}

export function domainStatus(d: Domain): DomainStatus {
  const caddy = state.containers.find((c) => c.kind === 'caddy')
  const c = containerFor(d)
  if (caddy?.status === 'stopped' || !c || c.status === 'stopped') return 'offline'
  if (caddy?.status === 'restarting' || c.status === 'restarting') return 'restarting'
  return 'online'
}

export const serviceEnabled = (s: ServiceName) => state.config[SERVICES[s].key] === 'true'
const serviceBusy = reactive<Partial<Record<ServiceName, boolean>>>({})
export const isServiceBusy = (s: ServiceName) => !!serviceBusy[s]

// ---------- API response conversion ----------
/** Daemon container → POC row (label, kind, port read from the settings). */
function toContainer(c: ApiContainer, prev?: Container): Container {
  const cfg = state.config
  const base = { id: c.name, cpu: prev?.cpu ?? 0, mem: prev?.mem ?? 0, status: (c.state === 'running' ? 'running' : c.state === 'restarting' ? 'restarting' : 'stopped') as ContainerStatus }
  const php = /^frankenphp-(\d\.\d)$/.exec(c.name)?.[1] as Php | undefined
  if (php) return { ...base, kind: 'php', php, label: 'FrankenPHP', port: '' }
  const known: Record<string, [ContainerKind, string, string]> = {
    'franken-caddy-proxy': ['caddy', 'Reverse proxy', `${cfg.WEB_HTTP_PORT || 80} / :${cfg.WEB_HTTPS_PORT || 443}`],
    franken_redis: ['redis', 'Redis', cfg.REDIS_PORT || '6379'],
    franken_soketi: ['soketi', 'Soketi', cfg.SOKETI_PORT || '6001'],
    franken_s3: ['s3', 'Garage (S3)', cfg.S3_ADMIN_PORT || '3903'],
    franken_s3_gateway: ['s3', 'Passerelle ACL S3', cfg.S3_PORT || '3900'],
    franken_mariadb: ['mariadb', 'MariaDB', cfg.DB_PORT || '3306'],
    franken_mysql: ['mariadb', 'MySQL', cfg.MYSQL_PORT || '3307'],
    franken_postgresql: ['postgres', 'PostgreSQL', cfg.POSTGRES_PORT || '5432'],
    franken_phpmyadmin: ['admin', 'phpMyAdmin', cfg.PMA_PORT || '8080'],
    franken_pgadmin: ['admin', 'pgAdmin', cfg.PGADMIN_PORT || '8081'],
  }
  const [kind, label, port] = known[c.name] ?? ['admin', c.name, '']
  return { ...base, kind, label, port }
}

/** Active (/status) and archived (/hosts/archived) hosts → POC domains. */
function toDomains(hosts: Status['hosts'], archives: { domain: string; kind: string; php?: string }[]): Domain[] {
  const out: Domain[] = hosts.map((h) => ({
    name: h.name, kind: h.kind, php: h.php as Php | undefined, root: h.root || undefined, spa: h.spa,
    target: h.kind === 'proxy' ? h.target : undefined, aliasOf: h.kind === 'alias' ? h.target : undefined,
    websocket: h.websocket, archived: false,
  }))
  // An alias follows the PHP version of its target.
  out.forEach((d) => { if (d.kind === 'alias') d.php = out.find((x) => x.name === d.aliasOf)?.php })
  for (const a of archives) {
    if (out.some((d) => d.name === a.domain)) continue
    out.push({ name: a.domain, kind: a.kind === 'static' ? 'static' : 'php', php: a.php as Php | undefined, archived: true })
  }
  return out
}

/** "52.3MiB" → bytes. */
function bytes(s: string) {
  const m = /([\d.]+)\s*([KMGT]?i?B)/i.exec(s)
  if (!m) return 0
  const unit = m[2]!.toUpperCase()
  const pow = { B: 0, KB: 1, KIB: 1, MB: 2, MIB: 2, GB: 3, GIB: 3, TB: 4, TIB: 4 }[unit] ?? 0
  return parseFloat(m[1]!) * (unit.includes('I') ? 1024 : 1000) ** pow
}

// ---------- Connection to an instance ----------
let api: Client | null = null
let events: EventSource | null = null
let logStream: EventSource | null = null
let timers: ReturnType<typeof setInterval>[] = []
let refreshSoon: ReturnType<typeof setTimeout> | undefined
/** Retry after a failed refresh (daemon gone): sooner than the 30 s poll, backing off up to RETRY_MAX. */
let retryTimer: ReturnType<typeof setTimeout> | undefined
let retryDelay = 0
const RETRY_MIN = 2_000
const RETRY_MAX = 30_000
let logSeq = 0
/** Last timestamp read per container: a reopened stream sends back its last lines, already displayed. */
let lastLogAt: Record<string, string> = {}

const sourceOf = (name: string) => {
  const c = state.containers.find((x) => x.id === name)
  return !c ? name : c.kind === 'php' ? `PHP ${c.php}` : c.kind === 'caddy' ? 'Caddy' : c.label
}

function pushLog(level: LogLevel, source: string, msg: string, at = new Date()) {
  state.logs.unshift({ id: ++logSeq, time: at.toLocaleTimeString('fr-FR', { hour12: false }), level, source, msg })
  if (state.logs.length > 400) state.logs.length = 400
}

/** `docker logs` line → log entry: Caddy JSON, or text whose level is guessed. */
function parseLine(container: string, line: string, time: string) {
  let level: LogLevel = /\b(error|fatal|panic|exception|critical)\b/i.test(line) ? 'ERROR' : /\bwarn(ing)?\b/i.test(line) ? 'WARN' : 'INFO'
  let msg = line
  if (line.startsWith('{')) {
    try {
      const j = JSON.parse(line) as { level?: string; msg?: string; request?: { method?: string; host?: string; uri?: string }; status?: number }
      level = j.level === 'error' || j.level === 'fatal' ? 'ERROR' : j.level === 'warn' ? 'WARN' : 'INFO'
      msg = j.request ? `${j.request.method} https://${j.request.host}${j.request.uri} ${j.status ?? ''}`.trim() : j.msg ?? line
    } catch { /* line kept as is */ }
  }
  pushLog(level, sourceOf(container), msg, time ? new Date(time) : new Date())
}

export async function refresh() {
  if (!api) return
  try {
    const [host, status, list, archives, settings] = await Promise.all([
      api.get<HostInfo>('/api/v1/host'),
      api.get<Status>('/api/v1/status'),
      api.get<ApiContainer[]>('/api/v1/containers'),
      api.get<ApiResponse<'/api/v1/hosts/archived'>>('/api/v1/hosts/archived'),
      api.get<ApiSetting[]>('/api/v1/settings?show_secrets=true'),
    ])
    // In place: some pages keep a reference to state.config.
    for (const k of Object.keys(state.config)) if (!settings.some((x) => x.key === k)) delete state.config[k]
    for (const x of settings) state.config[x.key] = x.value
    if (!CONFIG_DEFS.length) CONFIG_DEFS.push(...settings.map(toConfigDef))
    if (state.stack !== 'starting' && state.stack !== 'stopping') state.stack = status.running ? 'running' : 'stopped'
    state.domains = toDomains(status.hosts, archives)
    state.containers = list.map((c) => toContainer(c, state.containers.find((x) => x.id === c.name)))
    Object.assign(state.connection, { host: host.hostname, address: host.lan_ip ?? '' })
    state.app.version = host.version
    state.dns.ip = host.lan_ip ?? ''
    state.dns.port = Number(state.config.DNS_PORT) || 53
    // A daemon without the field (older release) is assumed to reach Docker.
    state.docker = status.docker !== false
    const recovering = !!state.error || retryDelay > 0
    state.error = ''
    clearTimeout(retryTimer)
    retryTimer = undefined
    retryDelay = 0
    reopenStreams(recovering)
    void refreshExtras()
  } catch (e) {
    // 401: the session is gone, the host signs in again (setUnauthorizedHandler).
    if (e instanceof ApiError && e.status === 401) return
    state.error = e instanceof Error ? e.message : String(e)
    scheduleRetry()
  } finally {
    state.loaded = true
  }
}

/** LAN DNS and CA sharing: secondary, without blocking the display. */
async function refreshExtras() {
  if (!api) return
  const [dns, clients, ca] = await Promise.all([
    api.get<ApiResponse<'/api/v1/dns'>>('/api/v1/dns').catch(() => null),
    api.get<ApiResponse<'/api/v1/dns/clients'>>('/api/v1/dns/clients').catch(() => null),
    api.get<ApiResponse<'/api/v1/ca'>>('/api/v1/ca').catch(() => null),
  ])
  if (dns) Object.assign(state.dns, { enabled: dns.enabled, status: dns.running ? 'running' : 'stopped' })
  if (clients) state.dns.connected = clients.connected
  if (ca) {
    // The daemon gives the URL (its LAN address); an older one only the port: same URL, on the host's LAN address.
    const port = ca.port ?? 0
    const url = ca.url || (ca.sharing && port && state.connection.address ? `http://${state.connection.address}:${port}/` : '')
    Object.assign(state.ssl, { caShared: ca.sharing, caPort: port, caUrl: ca.sharing ? url : '' })
  }
  Object.assign(state.dns, { listen: state.config.DNS_LISTEN_IP || '0.0.0.0', upstream: state.config.DNS_UPSTREAM ?? '' })
}

async function refreshStats() {
  if (!api || state.stack !== 'running') {
    state.cpuHistory = [...state.cpuHistory.slice(1), 0]
    return
  }
  const list = await api.get<Stat[] | null>('/api/v1/stats').catch(() => null)
  if (!list) return
  let used = 0
  let total = 0
  for (const s of list) {
    const c = state.containers.find((x) => x.id === s.name)
    const [u, t] = s.mem_usage.split('/')
    const mb = bytes(u ?? '') / 1024 ** 2
    if (c) Object.assign(c, { cpu: Math.round(s.cpu_percent * 10) / 10, mem: Math.round(mb) })
    used += mb
    total = Math.max(total, bytes(t ?? '') / 1024 ** 3)
  }
  const cpu = Math.round(list.reduce((a, s) => a + s.cpu_percent, 0))
  state.cpuHistory = [...state.cpuHistory.slice(1), cpu]
  state.memGo = Math.round((used / 1024) * 10) / 10
  state.memTotalGo = Math.round(total * 10) / 10
}

/** Log stream of all containers: feeds the recent activity and the Logs page. */
function openLogs() {
  logStream?.close()
  if (!api || state.stack !== 'running') return
  logStream = new EventSource(api.streamUrl('/api/v1/logs', { tail: '30' }))
  logStream.addEventListener('log', (e) => {
    const l = JSON.parse((e as MessageEvent).data) as LogLine
    const last = lastLogAt[l.container]
    if (last && l.time && l.time <= last) return
    if (l.time) lastLogAt[l.container] = l.time
    if (state.prefs.autoRefresh) parseLine(l.container, l.line, l.time)
  })
}

/** The daemon did not answer: refresh again after a growing delay (2 s, 4 s… 30 s). */
function scheduleRetry() {
  if (!api || retryTimer) return
  retryDelay = Math.min(retryDelay ? retryDelay * 2 : RETRY_MIN, RETRY_MAX)
  retryTimer = setTimeout(() => {
    retryTimer = undefined
    void refresh()
  }, retryDelay)
}

/**
 * Reopens the event stream when closed for good; after an outage (daemon restarted, updated), also the streams still
 * reconnecting, and the log stream.
 */
function reopenStreams(recovering: boolean) {
  if (!api) return
  if (!events || events.readyState === EventSource.CLOSED || (recovering && events.readyState !== EventSource.OPEN)) openEvents()
  if (recovering && state.stack === 'running' && logStream?.readyState !== EventSource.OPEN) openLogs()
}

/** Daemon event stream: operations, container and Docker changes. */
function openEvents() {
  events?.close()
  if (!api) return
  const es = new EventSource(api.streamUrl('/api/v1/events'))
  events = es
  // Set by an error: messages may have been missed, the state is read again once reconnected.
  let stale = false
  es.onopen = () => {
    state.live = true
    if (stale) void refresh()
    stale = false
  }
  es.onerror = () => {
    state.live = false
    stale = true
    // Closed for good (daemon restarted without the session…), or reconnecting: either way, check the daemon.
    if (es.readyState === EventSource.CLOSED && events === es) events = null
    scheduleRetry()
  }
  es.addEventListener('containers', (e) => {
    const list = JSON.parse((e as MessageEvent).data) as ApiContainer[]
    state.containers = list.map((c) => toContainer(c, state.containers.find((x) => x.id === c.name)))
  })
  es.addEventListener('docker', (e) => {
    const { reachable } = JSON.parse((e as MessageEvent).data) as Schema<'DockerState'>
    if (reachable === state.docker) return
    state.docker = reachable
    // Stack state and log stream follow Docker.
    void refresh().then(openLogs)
  })
  es.addEventListener('operation', (e) => {
    const op = JSON.parse((e as MessageEvent).data) as Operation
    if (op.status === 'succeeded' || op.status === 'failed') {
      clearTimeout(refreshSoon)
      refreshSoon = setTimeout(() => { void refresh().then(openLogs) }, 400)
    }
  })
  es.addEventListener('paired', (e) => onPaired(JSON.parse((e as MessageEvent).data) as PairedEvent))
  es.addEventListener('event', (e) => {
    const ev = JSON.parse((e as MessageEvent).data) as OpEvent
    if (ev.message && ev.kind !== 'output') pushLog(ev.level === 'error' ? 'ERROR' : ev.level === 'warning' ? 'WARN' : 'INFO', 'FrankenManager', ev.message)
  })
}

/** Opens an instance: state reset, then read, event stream and refreshes. */
export function connect(inst: Instance, opts: { mode?: 'local' | 'remote'; client?: string } = {}) {
  disconnect()
  const prefs = state.prefs
  Object.assign(state, blank(), { prefs })
  lastLogAt = {}
  // Local: destructive actions do not ask for the password again (ConfirmModal does not show its field).
  state.connection.mode = opts.mode ?? 'remote'
  state.connection.client = opts.client ?? 'App FrankenManager'
  api = client(inst)
  resetUpdates()
  resetPairing()
  resume()
}

/** Resumes the streams (opening, back to the foreground). */
export function resume() {
  if (!api) return
  pause()
  openEvents()
  void refresh().then(() => { openLogs(); void refreshStats() })
  timers = [setInterval(refreshStats, 5_000), setInterval(refresh, 30_000)]
  void loadUpdates()
  timers.push(setInterval(() => void loadUpdates(), UPDATE_POLL_MS))
}

/**
 * Same instance, new session (local re-sign-in after a daemon restart lost it): the state is kept, the requests and
 * streams go on with the new token.
 */
export function reconnect(inst: Instance) {
  api = client(inst)
  resume()
}

/** Suspends the streams (background) without forgetting the instance. */
export function pause() {
  events?.close()
  logStream?.close()
  events = logStream = null
  state.live = false
  timers.forEach(clearInterval)
  timers = []
  clearTimeout(refreshSoon)
  clearTimeout(retryTimer)
  retryTimer = undefined
  retryDelay = 0
}

export function disconnect() {
  pause()
  api = null
}

// ---------- Actions ----------
/**
 * Starts a daemon operation (202 response) and follows it to the end in a progress toast. Returns the operation's
 * error, null if it succeeded.
 */
async function operate(title: string, submit: (c: Client) => Promise<Operation>, success: string): Promise<string | null> {
  if (!api) return 'Aucune instance.'
  const c = api
  const t = progressToast(title, 'Envoi…')
  try {
    const op = await submit(c)
    t.step('En file…')
    const done = await c.wait(op, (o) => t.step(lastMessage(o) ?? 'En cours…'))
    if (done.status === 'failed') {
      const err = done.error || lastMessage(done) || 'Échec.'
      t.done('error', title, err)
      return err
    }
    t.done('success', title, lastMessage(done) ?? success)
    return null
  } catch (e) {
    const err = e instanceof Error ? e.message : String(e)
    t.done('error', title, e instanceof ApiError && e.status === 403 && /password|passe/i.test(err) ? 'Mot de passe erroné.' : err)
    return err
  } finally {
    void refresh()
  }
}

async function startStack() {
  state.stack = 'starting'
  await operate('Démarrage de la stack', (c) => c.post('/api/v1/stack/start', {}), 'La stack a démarré.')
  state.stack = 'stopped'
  await refresh()
  openLogs()
}

async function stopStack() {
  state.stack = 'stopping'
  const password = takePassword()
  await operate('Arrêt de la stack', (c) => c.post('/api/v1/stack/stop', {}, { action: 'Arrêter la stack', password }), 'Les volumes et les données sont conservés.')
  state.stack = 'running'
  await refresh()
}

async function restartStack() {
  state.stack = 'starting'
  await operate('Redémarrage de la stack', (c) => c.post('/api/v1/stack/restart', {}), 'La stack a redémarré.')
  state.stack = 'stopped'
  await refresh()
  openLogs()
}

async function restartContainer(id: string, opts: { rebuild?: boolean; action?: 'restart' | 'stop' | 'start' } = {}) {
  if (opts.rebuild) {
    // `frankenmanager restart --caddy --build`: the images of every active PHP version are rebuilt, their containers
    // recreated, then the FrankenPHP containers and the reverse proxy restart; the other services keep running.
    for (const c of state.containers) if (c.kind === 'php') c.status = 'restarting'
    await operate('Reconstruction des images PHP', (cl) => cl.post('/api/v1/stack/restart', { targets: ['caddy'], build: true }), 'Images PHP reconstruites, conteneurs recréés.')
    return
  }
  const action = opts.action ?? 'restart'
  // The POC names the reverse proxy "caddy"; its container is franken-caddy-proxy.
  if (id === 'caddy') id = state.containers.find((x) => x.kind === 'caddy')?.id ?? id
  const c = state.containers.find((x) => x.id === id)
  if (c && action !== 'stop') c.status = 'restarting'
  const label = { restart: 'Redémarrage', stop: 'Arrêt', start: 'Démarrage' }[action]
  await operate(`${label} de ${id}`, (cl) => cl.post(`/api/v1/containers/${encodeURIComponent(id)}/${action}`, {}, { action: `Arrêter ${id}` }), 'Terminé.')
}

async function addProject(p: NewProject): Promise<FieldErrors | null> {
  if (!DOMAIN_RE.test(p.name)) return { name: 'Utilisez un domaine local en .test (ex. blog.test).' }
  if (state.domains.some((d) => d.name === p.name && !d.archived)) return { name: 'Ce domaine existe déjà. Choisissez un autre nom.' }
  if (p.alias && !DOMAIN_RE.test(p.alias)) return { alias: 'Utilisez un domaine local en .test (ex. www.blog.test).' }
  const isStatic = p.type === 'static'
  const body = { domains: [p.name], root: p.root.trim(), ...(isStatic ? { static: true, spa: p.spa } : { php: p.php }) }
  const err = await operate(isStatic ? 'Ajout de l’hôte statique' : 'Création du projet', (c) => c.post('/api/v1/hosts', body), `${p.name} est prêt en HTTPS.`)
  if (err) return /root|dossier|folder|director/i.test(err) ? { root: err } : { name: err }
  if (p.alias) {
    const aErr = await addAlias(p.name, p.alias)
    if (aErr) return { alias: aErr }
  }
  return null
}

async function addAlias(target: string, alias: string): Promise<string | null> {
  if (!DOMAIN_RE.test(alias)) return 'Utilisez un domaine local en .test (ex. www.blog.test).'
  return operate('Ajout de l’alias', (c) => c.post(`/api/v1/hosts/${encodeURIComponent(target)}/aliases`, { aliases: [alias] }), `${alias} pointe vers ${target}.`)
}

async function archiveDomain(name: string) {
  const password = takePassword()
  const d = state.domains.find((x) => x.name === name)
  await operate(d?.kind === 'alias' ? 'Suppression de l’alias' : 'Archivage', (c) => c.post('/api/v1/hosts/remove', { domains: [name] }, { action: `Archiver ${name}`, password }), `${name} est archivé.`)
}

async function restoreDomain(name: string, php?: Php) {
  const d = state.domains.find((x) => x.name === name)
  const body = d?.kind === 'static' ? { domains: [name], static: true } : { domains: [name], php: php ?? d?.php ?? '' }
  await operate('Restauration', (c) => c.post('/api/v1/hosts/restore', body), `${name} est de nouveau en ligne.`)
}

async function switchPhp(name: string, php: Php) {
  await operate('Changement de version PHP', (c) => c.put(`/api/v1/hosts/${encodeURIComponent(name)}/php`, { php }), `${name} utilise désormais PHP ${php}.`)
}

/** One domain (`…/certificate`), or all of them (`restart --force-ssl`). */
async function regenerateCert(name: string | 'all') {
  state.ssl.regenerating = name
  if (name === 'all') await operate('Régénération SSL', (c) => c.post('/api/v1/stack/restart', { force_ssl: true }), 'Tous les certificats sont régénérés.')
  else await operate('Régénération SSL', (c) => c.post(`/api/v1/hosts/${encodeURIComponent(name)}/certificate`, {}), `Certificat de ${name} régénéré.`)
  state.ssl.regenerating = null
}

async function setService(name: ServiceName, on: boolean) {
  serviceBusy[name] = true
  await operate(`${on ? 'Activation' : 'Désactivation'} : ${SERVICES[name].label}`, (c) => c.put(`/api/v1/services/${name}`, { enabled: on }), 'Terminé.')
  serviceBusy[name] = false
}

function savePrefs(p: Prefs) {
  state.prefs = { ...p }
  try { localStorage.setItem('fm-prefs', JSON.stringify(p)) } catch { /* storage unavailable */ }
}

// =====================================================================================================================
// Rest of the POC dashboard: DNS, SSL, Redis, databases, S3, Caddyfiles, configuration, remote access.
// =====================================================================================================================

/** LAN DNS device (`/api/v1/dns/clients?all=true`). */
export interface DnsClient { ip: string; name?: string; mac?: string; privateMac?: boolean; lastSeen: number; queries: number }
export const DNS_CLIENT_WINDOW_MS = 5 * 60_000
export const clock = ref(Date.now())
setInterval(() => { clock.value = Date.now() }, 5000)
export const dnsClientsConnected = computed(() => state.dnsClients.filter((c) => clock.value - c.lastSeen < DNS_CLIENT_WINDOW_MS))

export interface RedisKey { key: string; type: string; size: string; ttl: number }
export type DbEngine = 'MariaDB' | 'MySQL' | 'PostgreSQL'
export interface Db { name: string; engine: DbEngine; size: number; user: string }
export interface DbUser { name: string; engine: DbEngine; host: string; database?: string }
export interface S3Bucket { name: string; public: boolean; objects: number; publicObjects: number; size: number }
export interface S3Key { id: string; name: string; bucket: string; readOnly: boolean; secret: string }
export interface S3Object { key: string; size: number; modified: number; public: boolean; url?: string }
export interface Session { id: string; device: string; ip: string; remote: boolean; created: number; lastSeen: number; current?: boolean }
export interface AccessEvent { at: number; ip: string; event: string; device?: string; remote: boolean }

// ---------- Configuration: definitions taken from the daemon registry (`GET /api/v1/settings`) ----------
export type ConfigType = 'bool' | 'port' | 'text' | 'select' | 'secret'
export interface ConfigDef {
  key: string
  group: string
  type: ConfigType
  default: string
  help: string
  options?: string[]
  placeholder?: string
  restart?: boolean
  generated?: boolean
  validate?: (v: string) => string | null
}
type ApiSetting = Schema<'Setting'>

const GROUPS: [RegExp, string][] = [
  [/^(DB_|MARIADB_|MYSQL_|POSTGRES_|PMA_|PGADMIN_)/, 'Bases de données'], [/^REDIS_/, 'Cache Redis'], [/^SOKETI_/, 'WebSocket (Soketi)'],
  [/^S3_/, 'Stockage S3 (Garage)'], [/^DNS_/, 'Serveur DNS'], [/^(DASHBOARD_|DAEMON_)/, 'Dashboard'], [/^WEB_/, 'Serveur web'],
  [/^(PHP_IMAGE|INSTALL_|EXTRA_)/, 'Images PHP'],
]
/** Settings from the daemon registry: their description and possible values come from it. */
export const CONFIG_DEFS = reactive<ConfigDef[]>([])
/** Values generated at startup are not known to the app: none is flagged as such. */
export const GENERATED_CONFIG: Record<string, string> = {}
function toConfigDef(s: ApiSetting): ConfigDef {
  const bool = !!s.values && s.values.length === 2 && s.values.includes('true') && s.values.includes('false')
  const type: ConfigType = s.secret ? 'secret' : bool ? 'bool' : s.values?.length ? 'select' : /_PORT$/.test(s.key) ? 'port' : 'text'
  return {
    key: s.key, group: GROUPS.find(([re]) => re.test(s.key))?.[1] ?? 'Général', type, default: s.default,
    help: `${s.description}${s.applies ? ` · ${s.applies}` : ''}`, options: s.values, restart: !['now', 'live', 'daemon', 'alias'].includes(s.apply),
  }
}
export const displayConfigValue = (def: ConfigDef, v: string) => (!v ? '(vide)' : def.type === 'secret' ? '••••••••' : v)

// ---------- Loading when a page opens ----------
/** A page's own data is only read when opening it (DashboardLayout watches the route). */
export async function loadPage(name: string) {
  if (!api) return
  const loaders: Record<string, () => Promise<void>> = {
    dns: loadDns, ssl: refreshExtras, redis: loadRedis, databases: loadDatabases, s3: loadS3, access: loadAccess, overview: loadDatabases,
  }
  await loaders[name]?.().catch((e) => toast('error', 'Lecture impossible', e instanceof Error ? e.message : String(e)))
}

async function loadDns() {
  if (!api) return
  const r = await api.get<ApiResponse<'/api/v1/dns/clients'>>('/api/v1/dns/clients?all=true')
  state.dnsClients = r.devices.map((d) => ({ ip: d.ip, name: d.name || undefined, mac: d.mac || undefined, privateMac: d.randomized_mac, lastSeen: Date.parse(d.last_seen), queries: d.queries }))
  Object.assign(state.dns, { listen: state.config.DNS_LISTEN_IP || '0.0.0.0', upstream: state.config.DNS_UPSTREAM ?? '', ip: state.config.DNS_IP || state.dns.ip })
  await refreshExtras()
}

const TYPE_SIZE: Record<string, string> = { string: 'chaîne', hash: 'hash', list: 'liste', set: 'ensemble', zset: 'ensemble trié', stream: 'flux' }
async function loadRedis(pattern = '*', count = 200) {
  if (!api || !serviceEnabled('redis')) return
  const st = await api.get<ApiResponse<'/api/v1/redis'>>('/api/v1/redis')
  const i = st.info
  // In place: RedisPage keeps a reference to state.redis.
  Object.assign(state.redis, {
    port: Number(state.config.REDIS_PORT) || 6379, version: i?.version ?? '', clients: i?.connected_clients ?? 0, keys: i?.keys ?? 0,
    memMB: i ? Math.round((bytes(`${i.used_memory}B`) / 1024 ** 2) * 10) / 10 : 0, hits: i?.hits ?? 0, misses: i?.misses ?? 0,
    hit: i && i.hits + i.misses ? Math.round((i.hits / (i.hits + i.misses)) * 100) : 0,
  })
  if (!st.running) { state.redis.sample = []; return }
  const page = await api.get<ApiResponse<'/api/v1/redis/keys'>>(`/api/v1/redis/keys?pattern=${encodeURIComponent(pattern || '*')}&count=${count}`)
  state.redis.sample = page.keys.map((k) => ({ key: k.name, type: k.type, size: TYPE_SIZE[k.type] ?? k.type, ttl: k.ttl }))
}
/** `SCAN MATCH` pattern: `*` and `?` as glob (local filter of the keys already read). */
export function redisMatch(pattern: string, key: string) {
  const re = new RegExp(`^${(pattern || '*').replace(/[.+^${}()|[\]\\]/g, '\\$&').replaceAll('*', '.*').replaceAll('?', '.')}$`)
  return re.test(key)
}

const ENGINE_LABEL: Record<string, DbEngine> = { mariadb: 'MariaDB', mysql: 'MySQL', postgresql: 'PostgreSQL' }
const engineId = (e: DbEngine) => ({ MariaDB: 'mariadb', MySQL: 'mysql', PostgreSQL: 'postgresql' })[e]
async function loadDatabases() {
  if (!api) return
  const r = await api.get<ApiResponse<'/api/v1/databases'>>('/api/v1/databases')
  state.databases = (r.servers ?? []).flatMap((s) => (s.databases ?? []).map((name) => ({ name, engine: ENGINE_LABEL[s.engine] ?? 'MariaDB', size: 0, user: '' })))
  state.dbUsers = (r.servers ?? []).flatMap((s) => (s.users ?? []).map((u) => ({ name: u.name, engine: ENGINE_LABEL[s.engine] ?? 'MariaDB', host: u.host ?? '' })))
  state.adminUrl = r.admin_url ?? ''
}

export const BUCKET_RE = /^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$/
export const s3Up = () => state.containers.some((c) => c.id === 'franken_s3_gateway' && c.status === 'running')
export function validateBucket(name: string): string | null {
  if (!name) return 'Le nom du bucket est requis.'
  if (!BUCKET_RE.test(name)) return '3 à 63 minuscules, chiffres et tirets, sans point.'
  if (state.s3.buckets.some((b) => b.name === name)) return `Le bucket ${name} existe déjà.`
  return null
}
async function loadS3() {
  if (!api || !serviceEnabled('s3') || !s3Up()) return
  const [buckets, keys] = await Promise.all([
    api.get<ApiResponse<'/api/v1/s3/buckets'>>('/api/v1/s3/buckets'),
    api.get<ApiResponse<'/api/v1/s3/keys'>>('/api/v1/s3/keys'),
  ])
  // In place: S3Page keeps a reference to state.s3.
  state.s3.buckets = buckets.map((b) => ({ name: b.name, public: b.public, objects: b.objects, publicObjects: b.public_objects, size: Math.round((b.bytes / 1e6) * 10) / 10 }))
  const own = keys.filter((k) => !k.global && k.grants.length)
  const secrets = await Promise.all(own.map((k) => api!.get<ApiResponse<'/api/v1/s3/keys/{id}'>>(`/api/v1/s3/keys/${k.id}`).catch(() => ({ secret: '' }))))
  state.s3.keys = own.map((k, i) => ({ id: k.id, name: k.name, bucket: k.grants[0]!.bucket, readOnly: !k.grants[0]!.write, secret: secrets[i]!.secret ?? '' }))
}

/** A bucket folder (`?prefix=`): its objects, and its subfolders as "folder/" entries. */
export async function loadObjects(bucket: string, prefix: string) {
  if (!api || !bucket) return
  const page = await api.get<ApiResponse<'/api/v1/s3/buckets/{bucket}/objects'>>(
    `/api/v1/s3/buckets/${encodeURIComponent(bucket)}/objects?prefix=${encodeURIComponent(prefix)}`)
  state.s3.objects[bucket] = [
    ...(page.folders ?? []).map((f) => ({ key: f.endsWith('/') ? f : `${f}/`, size: 0, modified: 0, public: false })),
    ...(page.objects ?? []).map((o) => ({ key: o.key, size: o.size, modified: Date.parse(o.last_modified), public: o.public, url: o.url })),
  ]
}
export const bucketObjects = (bucket: string) => state.s3.objects[bucket] ?? []
export const objectPublic = (bucket: string, o: S3Object) => !!state.s3.buckets.find((b) => b.name === bucket)?.public || o.public
/** Public URL of a public object, otherwise a presigned link valid for `expires` seconds (`…/object/link`). */
export async function objectLink(bucket: string, o: S3Object, expires = 3600) {
  if (!api) throw new Error('Aucune instance.')
  const l = await api.get<ApiResponse<'/api/v1/s3/buckets/{bucket}/object/link'>>(
    `/api/v1/s3/buckets/${encodeURIComponent(bucket)}/object/link?key=${encodeURIComponent(o.key)}&expires=${expires}`)
  return { url: l.url, public: l.public, expiresAt: l.expires_at ? Date.parse(l.expires_at) : null }
}
/** Download streamed by the daemon: the token goes in the URL. */
export const objectDownloadUrl = (bucket: string, key: string) => api?.streamUrl(`/api/v1/s3/buckets/${encodeURIComponent(bucket)}/object`, { key }) ?? ''

async function loadAccess() {
  if (!api) return
  const [auth, sessions, log, host] = await Promise.all([
    api.get<ApiResponse<'/api/v1/auth'>>('/api/v1/auth'),
    api.get<ApiResponse<'/api/v1/auth/sessions'>>('/api/v1/auth/sessions'),
    api.get<ApiResponse<'/api/v1/auth/log'>>('/api/v1/auth/log?limit=100'),
    api.get<HostInfo>('/api/v1/host'),
  ])
  Object.assign(state.access, {
    passwordSet: auth.password_set, remoteActive: auth.remote_active, remoteUrl: host.remote_url ?? '',
    sessions: sessions.map((x) => ({ id: x.id, device: x.device, ip: x.ip, remote: x.remote, created: Date.parse(x.created), lastSeen: Date.parse(x.last_seen), current: x.current })),
    log: log.map((x) => ({ at: Date.parse(x.at), ip: x.ip, event: x.event, device: x.device, remote: x.remote })),
  })
}
export const accessMode = () => (state.config.DASHBOARD_ACCESS || 'local') as 'local' | 'lan' | 'internet'
export const passwordSet = () => state.access.passwordSet
export const remoteActive = () => state.access.remoteActive
export const remoteUrl = () => state.access.remoteUrl || `https://${state.dns.ip}:${state.config.DASHBOARD_REMOTE_PORT}`

// ---------- Site Caddyfiles ----------
export const caddyfilePathOf = (name: string) => state.siteFiles[name]?.path ?? ''
export const siteCaddyfile = (name: string) => state.siteFiles[name]?.content ?? ''
export const defaultCaddyfile = (name: string) => state.siteFiles[name]?.template ?? ''
export const isCaddyfileCustom = (name: string) => !!state.siteFiles[name]?.modified
export async function loadCaddyfile(name: string) {
  if (!api) return
  const f = await api.get<ApiResponse<'/api/v1/hosts/{name}/caddyfile'>>(`/api/v1/hosts/${encodeURIComponent(name)}/caddyfile`)
  state.siteFiles[name] = f
}

// ---------- Actions of the other pages ----------
async function addRoute(r: { source: string; target: string; websocket: boolean }): Promise<FieldErrors | null> {
  if (!DOMAIN_RE.test(r.source)) return { source: 'Utilisez un domaine local en .test (ex. app.test).' }
  const target = r.target.replace(/^https?:\/\//, '')
  if (!/^[\w.-]+:\d{2,5}$/.test(target)) return { target: 'Adresse attendue au format hôte:port (ex. 127.0.0.1:5173).' }
  const err = await operate('Ajout de la route', (c) => c.post('/api/v1/hosts', { domains: [r.source], target, websocket: r.websocket }), `${r.source} est exposé en HTTPS.`)
  return err ? { source: err } : null
}
async function setWebsocket(name: string, on: boolean) {
  await operate(on ? 'WebSocket autorisé' : 'WebSocket refusé', (c) => c.put(`/api/v1/hosts/${encodeURIComponent(name)}/websocket`, { enabled: on }), on ? `${name} relaie les connexions WebSocket.` : `Une demande d’upgrade vers ${name} reçoit 403.`)
}

async function checkCaddyfile(name: string, text: string): Promise<{ line: number; message: string } | null> {
  if (!api) return { line: 1, message: 'Aucune instance.' }
  try {
    await api.post<ApiResponse<'/api/v1/hosts/{name}/caddyfile/validate', 'post'>>(`/api/v1/hosts/${encodeURIComponent(name)}/caddyfile/validate`, { content: text })
    return null
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    return { line: Number(/line (\d+)/i.exec(msg)?.[1] ?? /:(\d+)/.exec(msg)?.[1] ?? 1), message: msg }
  }
}
async function saveCaddyfile(name: string, text: string): Promise<string | null> {
  const err = await operate('Caddyfile enregistré', (c) => c.put(`/api/v1/hosts/${encodeURIComponent(name)}/caddyfile`, { content: text }), 'Vérifié par Caddy, le conteneur a été rechargé.')
  if (!err) await loadCaddyfile(name).catch(() => {})
  return err
}
async function resetCaddyfile(name: string) {
  const password = takePassword()
  await operate('Retour au modèle', (c) => c.post(`/api/v1/hosts/${encodeURIComponent(name)}/caddyfile/reset`, {}, { action: `Rétablir le Caddyfile de ${name}`, password }), 'Caddyfile régénéré depuis le modèle.')
  await loadCaddyfile(name).catch(() => {})
}

/** DNS_* settings: saved one by one, then the DNS server reloads its configuration. */
async function saveDns(dns: { ip: string; listen: string; port: number; upstream: string; enabled: boolean }) {
  const values: Record<string, string> = {
    DNS_IP: dns.ip === state.connection.address ? '' : dns.ip, DNS_LISTEN_IP: dns.listen === '0.0.0.0' ? '' : dns.listen,
    DNS_PORT: String(dns.port), DNS_UPSTREAM: dns.upstream,
  }
  for (const [k, v] of Object.entries(values)) {
    if ((state.config[k] ?? '') === v) continue
    if (await operate(`Réglage ${k}`, (c) => c.put(`/api/v1/settings/${k}`, { value: v }), `${k} enregistré.`)) return
  }
  if (dns.enabled !== state.dns.enabled) await setService('dns', dns.enabled)
  await loadDns()
}
async function restartDns() {
  state.dns.status = 'restarting'
  await operate('Redémarrage du DNS', (c) => c.post('/api/v1/dns/apply', {}), 'Le serveur DNS a relu sa configuration.')
  await loadDns()
}

async function setCaShared(on: boolean, port: number) {
  await operate(on ? 'Partage de la CA' : 'Arrêt du partage', (c) => c.put('/api/v1/ca', { enabled: on, port }), on ? 'Désactivez le partage après l’installation.' : 'La CA n’est plus exposée.')
  await refreshExtras()
}

async function flushRedis() {
  const password = takePassword()
  await operate('Vidage du cache', (c) => c.post('/api/v1/redis/flush', {}, { action: 'Vider Redis', password }), 'Toutes les clés sont supprimées.')
  await loadRedis()
}
async function deleteRedisKeys(keys: string[]) {
  const password = takePassword()
  await operate('Suppression de clés', (c) => c.post('/api/v1/redis/keys/delete', { keys }, { action: 'Supprimer des clés', password }), keys.length === 1 ? keys[0]! : `${keys.length} clés.`)
  await loadRedis()
}
async function scanRedis(pattern: string, count = 200) { await loadRedis(pattern, count) }

const DB_NAME = /^[A-Za-z0-9_]{1,63}$/
async function createDb(d: { engine: DbEngine; name: string; user: string; password: string; grant: boolean }): Promise<FieldErrors | null> {
  if (!DB_NAME.test(d.name)) return { name: 'Lettres, chiffres et _ uniquement, 63 au plus.' }
  if (d.grant) {
    if (!DB_NAME.test(d.user)) return { user: 'Lettres, chiffres et _ uniquement, 63 au plus.' }
    if (!d.password) return { password: 'Un mot de passe est obligatoire.' }
  }
  const err = await operate('Création de la base', (c) => c.post(`/api/v1/databases/${engineId(d.engine)}`, { name: d.name }), `${d.name} (${d.engine}) est prête.`)
  if (err) { await loadDatabases(); return { name: err } }
  if (d.grant) return createDbUser({ engine: d.engine, name: d.user, password: d.password, database: d.name })
  await loadDatabases()
  return null
}
async function dropDb(name: string, engine: DbEngine) {
  const password = takePassword()
  await operate('Suppression de la base', (c) => c.post(`/api/v1/databases/${engineId(engine)}/delete`, { names: [name] }, { action: `Supprimer ${name}`, password }), `${name} a été supprimée.`)
  await loadDatabases()
}
async function createDbUser(u: { engine: DbEngine; name: string; password: string; database: string }): Promise<FieldErrors | null> {
  if (!DB_NAME.test(u.name)) return { user: 'Lettres, chiffres et _ uniquement, 63 au plus.' }
  if (!u.password) return { password: 'Un mot de passe est obligatoire.' }
  const err = await operate('Création de l’utilisateur', (c) => c.post(`/api/v1/databases/${engineId(u.engine)}/users`, { name: u.name, password: u.password, database: u.database }), u.database ? `${u.name} a tous les droits sur ${u.database}.` : `${u.name} est créé.`)
  await loadDatabases()
  return err ? { user: err } : null
}
async function dropDbUser(name: string, engine: DbEngine) {
  const password = takePassword()
  await operate('Suppression de l’utilisateur', (c) => c.post(`/api/v1/databases/${engineId(engine)}/users/delete`, { names: [name] }, { action: `Supprimer ${name}`, password }), `${name} ne peut plus se connecter.`)
  await loadDatabases()
}

async function createBucket(name: string, pub: boolean): Promise<string | null> {
  const err = validateBucket(name) ?? await operate('Création du bucket', (c) => c.post('/api/v1/s3/buckets', { names: [name], public: pub }), pub ? 'Public : ses objets sont lisibles sans identifiants.' : 'Privé.')
  await loadS3()
  return err
}
async function setBucketPublic(name: string, pub: boolean) {
  await operate(pub ? 'Bucket public' : 'Bucket privé', (c) => c.post('/api/v1/s3/buckets/public', { names: [name], public: pub }), name)
  await loadS3()
}
async function emptyBucket(name: string) {
  const password = takePassword()
  await operate('Vidage du bucket', (c) => c.post('/api/v1/s3/buckets/empty', { names: [name] }, { action: `Vider ${name}`, password }), `${name} est vide.`)
  await loadS3()
}
async function deleteBucket(name: string) {
  const password = takePassword()
  await operate('Suppression du bucket', (c) => c.post('/api/v1/s3/buckets/delete', { names: [name], force: true }, { action: `Supprimer ${name}`, password }), `${name} et ses clés sont supprimés.`)
  await loadS3()
}
async function createS3Key(bucket: string, name: string, readOnly: boolean): Promise<FieldErrors | null> {
  if (!bucket) return { bucket: 'Choisissez un bucket existant.' }
  const err = await operate('Création de la clé', (c) => c.post('/api/v1/s3/keys', { bucket, name: name.trim(), read_only: readOnly }), `Clé créée pour ${bucket}.`)
  await loadS3()
  return err ? { name: err } : null
}
async function deleteS3Key(id: string) {
  const password = takePassword()
  await operate('Suppression de la clé', (c) => c.post('/api/v1/s3/keys/delete', { ids: [id] }, { action: `Supprimer ${id}`, password }), id)
  await loadS3()
}
async function uploadObjects(bucket: string, prefix: string, files: File[], pub: boolean) {
  for (const f of files) {
    const key = `${prefix}${f.name}`
    const err = await operate('Téléversement', (c) => c.upload(`/api/v1/s3/buckets/${encodeURIComponent(bucket)}/object?key=${encodeURIComponent(key)}&public=${pub}`, f), key)
    if (err) break
  }
  await Promise.all([loadObjects(bucket, prefix), loadS3()])
}
async function deleteObjects(bucket: string, keys: string[], prefix = '') {
  const password = takePassword()
  await operate('Suppression d’objets', (c) => c.post(`/api/v1/s3/buckets/${encodeURIComponent(bucket)}/objects/delete`, { keys }, { action: 'Supprimer des objets', password }), `${keys.length} objet(s) supprimé(s).`)
  await Promise.all([loadObjects(bucket, prefix), loadS3()])
}
async function setObjectsPublic(bucket: string, keys: string[], pub: boolean, prefix = '') {
  await operate(pub ? 'Objet(s) public(s)' : 'Objet(s) privé(s)', (c) => c.post(`/api/v1/s3/buckets/${encodeURIComponent(bucket)}/objects/public`, { keys, public: pub }), pub ? 'public-read' : 'private')
  await Promise.all([loadObjects(bucket, prefix), loadS3()])
}

/** `PUT /api/v1/settings/{key}`; a service key (REDIS_ENABLED…) acts as service enable|disable. */
async function setConfig(key: string, raw: string): Promise<string | null> {
  const def = CONFIG_DEFS.find((d) => d.key === key)
  if (!def) return 'Clé inconnue.'
  const v = raw.trim()
  if (def.type === 'port' && (!/^\d+$/.test(v) || +v < 1 || +v > 65535)) return 'Port invalide (1-65535).'
  if (def.type === 'select' && def.options && !def.options.includes(v)) return `Valeurs possibles : ${def.options.join(', ')}.`
  const password = key.startsWith('DASHBOARD_') ? takePassword() : undefined
  const err = await operate('Configuration enregistrée', (c) => c.put(`/api/v1/settings/${key}`, { value: v }, { action: `Modifier ${key}`, password }), `${key} = ${displayConfigValue(def, v)}`)
  if (!err && def.restart && !state.configPending.includes(key)) state.configPending.push(key)
  return err
}
async function resetConfig(key: string) {
  const def = CONFIG_DEFS.find((d) => d.key === key)
  const password = key.startsWith('DASHBOARD_') ? takePassword() : undefined
  const err = await operate('Valeur par défaut rétablie', (c) => c.put(`/api/v1/settings/${key}`, { value: null }, { action: `Rétablir ${key}`, password }), key)
  if (!err && def?.restart && !state.configPending.includes(key)) state.configPending.push(key)
}

const ACCESS_KEYS = ['DASHBOARD_ACCESS', 'DASHBOARD_REMOTE_PORT', 'DASHBOARD_ALLOWED_IPS', 'DASHBOARD_LOCAL_AUTH', 'DASHBOARD_TLS_CERT', 'DASHBOARD_TLS_KEY'] as const
/** Modified DASHBOARD_* settings, saved one by one with the password entered once. */
async function setAccess(values: Partial<Record<(typeof ACCESS_KEYS)[number], string>>): Promise<FieldErrors | null> {
  const password = takePassword()
  for (const [k, v] of Object.entries(values)) {
    if ((state.config[k] ?? '') === (v ?? '')) continue
    const err = await operate(`Réglage ${k}`, (c) => c.put(`/api/v1/settings/${k}`, { value: v ?? '' }, { action: `Modifier ${k}`, password }), `${k} enregistré.`)
    if (err) return { [k]: err }
  }
  await loadAccess()
  return null
}
async function setPassword(current: string, next: string): Promise<FieldErrors | null> {
  if (next.length < 10) return { password: 'Au moins 10 caractères.' }
  const err = await operate('Mot de passe changé', (c) => c.put('/api/v1/auth/password', { current, password: next }), 'Tous les appareils sont déconnectés, celui-ci aussi : reconnectez-vous.')
  if (err) return /passe|password/i.test(err) ? { current: err } : { password: err }
  return null
}
async function revokeSession(id: string) {
  if (!api) return
  try {
    await api.del(`/api/v1/auth/sessions/${encodeURIComponent(id)}`)
    toast('success', 'Appareil déconnecté', 'Il devra se reconnecter.')
  } catch (e) {
    toast('error', 'Déconnexion impossible', e instanceof Error ? e.message : String(e))
  }
  await loadAccess()
}

export const actions = {
  startStack, stopStack, restartStack, restartContainer, addProject, addAlias, archiveDomain, restoreDomain,
  switchPhp, regenerateCert, setService, savePrefs,
  addRoute, setWebsocket, checkCaddyfile, saveCaddyfile, resetCaddyfile, saveDns, restartDns, setCaShared,
  flushRedis, deleteRedisKeys, scanRedis, createDb, dropDb, createDbUser, dropDbUser,
  createBucket, setBucketPublic, emptyBucket, deleteBucket, createS3Key, deleteS3Key, uploadObjects, deleteObjects, setObjectsPublic,
  setConfig, resetConfig, setAccess, setPassword, revokeSession,
  /** With the app, sign-in happens from the instance list. */
  signIn: async (_password: string): Promise<string | null> => null,
  /** The domain only resolves on a device that uses the instance's LAN DNS. */
  openSite: (name: string) => { window.open(`https://${name}`, '_blank') },
}

// ---------- FrankenManager updates (GET /api/v1/update) ----------
/** A release newer than the instance's daemon, with its GitHub notes (Markdown). */
export type ReleaseNotes = Schema<'ReleaseNotes'>
/** `GET /api/v1/update`: installed and latest versions, notes of the newer releases. */
export type UpdateInfo = ApiResponse<'/api/v1/update'>

/** The daemon caches the lookup (one hour): polling it rarely is enough. */
const UPDATE_POLL_MS = 30 * 60_000
const blankUpdates = () => ({ info: null as UpdateInfo | null, checking: false, error: '' })
/** Lookup of the instance's daemon; `error`: no answer at all (GitHub unreachable, rate limit). */
export const updates = reactive(blankUpdates())
export const updateAvailable = computed(() => !!updates.info?.update_available)

function resetUpdates() {
  Object.assign(updates, blankUpdates())
}

/** Reads the lookup; `refresh` bypasses the daemon's cache. Returns the error, null when answered. */
async function loadUpdates(refresh = false): Promise<string | null> {
  if (!api) return 'Aucune instance.'
  const c = api
  updates.checking = true
  try {
    const info = await c.get<UpdateInfo>(`/api/v1/update${refresh ? '?refresh=true' : ''}`)
    if (c !== api) return null
    updates.info = info
    updates.error = ''
    state.app.latest = info.latest
    return null
  } catch (e) {
    const err = e instanceof Error ? e.message : String(e)
    if (c === api) updates.error = err
    return err
  } finally {
    if (c === api) updates.checking = false
  }
}

/** "Vérifier": looks up GitHub again (`frankenmanager update --check`). */
export async function checkUpdate() {
  const err = await loadUpdates(true)
  const info = updates.info
  if (err || !info) toast('error', 'Vérification impossible', err ?? undefined)
  else if (info.error) toast('warning', 'Vérification impossible', info.error)
  else if (info.update_available) toast('info', 'Mise à jour disponible', `FrankenManager ${info.latest} est disponible.`)
  else toast('success', 'FrankenManager est à jour', `Version ${info.version}.`)
}

// ---------- Device pairing (POST /api/v1/auth/pairing, "paired" event) ----------
/** A one-time code for the mobile app: `qr` is the text of the QR code to show. */
export type Pairing = ApiResponse<'/api/v1/auth/pairing', 'post'>
type PairedEvent = Schema<'PairedEvent'>

const blankPairing = () => ({ code: null as Pairing | null, creating: false, error: '', paired: '' })
/** The code shown by the pairing modal; `paired`: the device that used it. */
export const pairing = reactive(blankPairing())

function resetPairing() {
  Object.assign(pairing, blankPairing())
}

/** New code (the previous one stays valid until it expires). Remotely, the daemon asks for the password again. */
export async function createPairing() {
  if (!api) return
  const c = api
  Object.assign(pairing, { creating: true, error: '', paired: '' })
  try {
    const code = await c.post<Pairing>('/api/v1/auth/pairing', {}, { action: 'Appairer un appareil' })
    if (c === api) pairing.code = code
  } catch (e) {
    if (c === api) Object.assign(pairing, { code: null, error: e instanceof Error ? e.message : String(e) })
  } finally {
    if (c === api) pairing.creating = false
  }
}

/** Modal closed: the code is forgotten here (it expires on the daemon side). */
export const closePairing = resetPairing

/** A device used a code: the modal tells which one if it is the code shown, the sessions are read again. */
function onPaired(ev: PairedEvent) {
  if (pairing.code?.id === ev.id) Object.assign(pairing, { code: null, paired: ev.device })
  toast('success', 'Appareil appairé', ev.device)
  loadAccess().catch(() => {})
}
