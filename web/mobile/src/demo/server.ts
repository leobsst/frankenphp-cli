import { ApiError, type Schema } from '@app/lib/api'
import { defaultRoot, generateSiteCaddyfile, generateStaticCaddyfile, siteCaddyfilePath, validateCaddyfile } from '@app/composables/caddyfile'
import { t } from '@app/i18n'
import {
  ACCESS_LOG, ARCHIVED, BUCKETS, DATABASES, DEMO_HOSTNAME, DEMO_LAN_IP, DEMO_REMOTE_PORT, DEMO_VERSION, DNS_CLIENTS, HOSTS,
  LOAD, LOG_LINES, REDIS_KEYS, S3_KEYS, SESSIONS, SETTINGS, type DemoBucket,
} from './fixtures'
import { DemoEventStream } from './stream'

/**
 * In-memory fake of the daemon API (`/api/v1`) behind the demo mode. It answers the routes the store and the pages
 * call with the fixtures, and runs the actions as operations that progress (queued, running, succeeded) then change
 * the fixtures, so the interface follows as it does with a real daemon. Its event and log streams emit the same
 * messages as the daemon's SSE streams. Nothing leaves the device; the data lives as long as the app runs.
 */

type Host = Schema<'Host'>
type Operation = Schema<'Operation'>
type ServiceName = Schema<'Service'>['name']

const SERVICE_KEYS: Record<ServiceName, string> = { dns: 'DNS_SERVER', redis: 'REDIS_ENABLED', s3: 'S3_ENABLED', soketi: 'SOKETI_ENABLED' }

function createWorld() {
  return {
    running: true,
    hosts: HOSTS(),
    archived: ARCHIVED(),
    settings: Object.fromEntries(SETTINGS().map((s) => [s.key, s.value])) as Record<string, string>,
    /** Containers stopped one by one, and those restarting right now. */
    stopped: new Set<string>(),
    restarting: new Set<string>(),
    ca: { sharing: true, port: 9445 },
    dnsClients: DNS_CLIENTS(),
    redisKeys: REDIS_KEYS(),
    redisHits: 18_422,
    redisMisses: 377,
    servers: DATABASES(),
    buckets: BUCKETS(),
    keys: S3_KEYS(),
    sessions: SESSIONS(),
    log: ACCESS_LOG(),
    caddyfiles: {} as Record<string, string>,
  }
}
let world = createWorld()

/** Starts again from the fixtures (a demo instance added again). */
export function resetDemo() {
  world = createWorld()
}

const enabled = (s: ServiceName) => world.settings[SERVICE_KEYS[s]] === 'true'
const accessMode = () => (world.settings.DASHBOARD_ACCESS || 'local') as 'local' | 'lan' | 'internet'

// ---------- Derived state ----------
function containerNames() {
  const php = [...new Set(world.hosts.filter((h) => h.kind === 'php' && h.php).map((h) => h.php!))].sort().reverse()
  const engines = world.servers.map((s) => (s.engine === 'postgresql' ? 'franken_postgresql' : `franken_${s.engine}`))
  return [
    ...php.map((v) => `frankenphp-${v}`),
    'franken-caddy-proxy',
    ...(enabled('redis') ? ['franken_redis'] : []),
    ...(enabled('soketi') ? ['franken_soketi'] : []),
    ...(enabled('s3') ? ['franken_s3', 'franken_s3_gateway'] : []),
    ...engines,
    'franken_phpmyadmin',
  ]
}

function containers(): Schema<'Container'>[] {
  return containerNames().map((name) => {
    const state = !world.running || world.stopped.has(name) ? 'exited' : world.restarting.has(name) ? 'restarting' : 'running'
    return { name, state, health: state === 'running' ? 'healthy' : 'N/A' }
  })
}
const isUp = (name: string) => containers().some((c) => c.name === name && c.state === 'running')

const jitter = (base: number, spread: number) => Math.max(0.1, base + (Math.random() * 2 - 1) * spread)

function stats(): Schema<'Stat'>[] {
  return containers().filter((c) => c.state === 'running').map((c) => {
    const [cpu, mem] = LOAD[c.name] ?? [0.5, 32]
    const mib = jitter(mem, mem * 0.04)
    return {
      name: c.name, cpu_percent: Math.round(jitter(cpu, cpu * 0.6 + 0.4) * 100) / 100, mem_percent: Math.round((mib / 16_000) * 10_000) / 100,
      mem_usage: `${mib.toFixed(1)}MiB / 15.6GiB`, net_io: '12.4MB / 8.1MB', block_io: '0B / 0B',
    }
  })
}

function status(): Schema<'Status'> {
  return {
    running: world.running,
    docker: true,
    hosts: world.hosts.map((h) => ({ ...h })),
    services: (Object.keys(SERVICE_KEYS) as ServiceName[]).map((name) => ({
      name, setting: SERVICE_KEYS[name], enabled: enabled(name),
      state: !enabled(name) ? 'disabled' : name === 'dns' || world.running ? 'running' : 'stopped',
    })),
  }
}

function hostInfo(): Schema<'HostInfo'> {
  const mode = accessMode()
  return {
    hostname: DEMO_HOSTNAME, os: 'darwin', arch: 'arm64', version: DEMO_VERSION, lan_ip: DEMO_LAN_IP, remote: true, mode,
    remote_url: mode === 'local' ? undefined : `https://${DEMO_LAN_IP}:${world.settings.DASHBOARD_REMOTE_PORT || DEMO_REMOTE_PORT}`,
  }
}

/** The demo Mac in detail (Machine page); its uptimes count from when the demo started. */
const demoStart = Date.now()
function hostSystem(): Schema<'HostSystem'> {
  const since = Math.round((Date.now() - demoStart) / 1000)
  return {
    hostname: DEMO_HOSTNAME, os: 'darwin', os_name: 'macOS 26.1', kernel: '25.1.0', arch: 'arm64',
    cpu: { model: 'Apple M3 Pro', cores: 12 }, memory: { total: 36 * 1024 ** 3 }, uptime: 4 * 86_400 + 5 * 3600 + since,
    disks: [
      { label: 'data', path: '/Users/demo/Library/Application Support/FrankenManager', total: 994_662_584_320, free: 412_318_253_056 },
      { label: 'projects', path: '/Users/demo/Developer', total: 994_662_584_320, free: 412_318_253_056 },
    ],
    daemon: { version: DEMO_VERSION, go: 'go1.26.0', pid: 4812, started_at: new Date(demoStart - 2 * 86_400_000).toISOString() },
    docker: { version: '29.0.1', os: 'Docker Desktop', cpus: 12, memory: 16 * 1024 ** 3 },
    network: {
      lan_ip: DEMO_LAN_IP,
      interfaces: [
        { name: 'en0', mac: 'a4:83:e7:12:9c:4f', up: true, addresses: [`${DEMO_LAN_IP}/24`, 'fe80::1c2d:3e4f:5a6b:7c8d/64'] },
        { name: 'bridge100', mac: 'a6:83:e7:4b:21:64', up: true, addresses: ['192.168.64.1/24'] },
      ],
    },
  }
}

function bucketInfo(b: DemoBucket): Schema<'Bucket'> {
  return {
    name: b.name, public: b.public, objects: b.objects.length, public_objects: b.objects.filter((o) => b.public || o.public).length,
    bytes: b.objects.reduce((a, o) => a + o.size, 0), keys: world.keys.filter((k) => k.grants.some((g) => g.bucket === b.name)).length,
    url: `https://${b.name}.s3.frankenmanager.test`,
  }
}

function siteFile(name: string): Schema<'SiteFile'> {
  const h = world.hosts.find((x) => x.name === name)
  if (!h || (h.kind !== 'php' && h.kind !== 'static')) throw new ApiError(404, `no Caddyfile for ${name}`)
  const isStatic = h.kind === 'static'
  const template = isStatic ? generateStaticCaddyfile(name, h.root ?? defaultRoot(name, true), !!h.spa) : generateSiteCaddyfile(name, h.root)
  const content = world.caddyfiles[name] ?? template
  return {
    host: name, kind: h.kind as 'php' | 'static', php: h.php, path: siteCaddyfilePath(name, h.php), content, template,
    modified: content !== template,
  }
}

// ---------- Streams ----------
const streams = new Set<DemoEventStream>()
let logTimer: ReturnType<typeof setInterval> | undefined

function broadcast(path: string, type: string, data: unknown) {
  streams.forEach((s) => { if (s.path === path) s.emit(type, data) })
}
const emitContainers = () => broadcast('/api/v1/events', 'containers', containers())

/** A log line from a container, on the log streams. */
function log(container: string, line: string, at = new Date()) {
  broadcast('/api/v1/logs', 'log', { container, time: at.toISOString(), line } satisfies Schema<'LogLine'>)
}

function randomLine(): [string, string] | null {
  const live = Object.keys(LOG_LINES).filter(isUp)
  if (!live.length) return null
  const c = live[Math.floor(Math.random() * live.length)]!
  const lines = LOG_LINES[c]!
  return [c, lines[Math.floor(Math.random() * lines.length)]!]
}

function onOpen(s: DemoEventStream) {
  if (s.path !== '/api/v1/logs') return
  // The last lines first (tail), spread over the past minutes, then new ones every second or two.
  const tail = Math.min(Number(s.params.tail) || 30, 60)
  const now = Date.now()
  for (let i = tail; i > 0; i--) {
    const l = randomLine()
    if (l) s.emit('log', { container: l[0], time: new Date(now - i * 7_000).toISOString(), line: l[1] })
  }
  if (!logTimer) {
    logTimer = setInterval(() => {
      const l = Math.random() < 0.7 ? randomLine() : null
      if (l) log(l[0], l[1])
    }, 1_600)
  }
}

function onClose(s: DemoEventStream) {
  streams.delete(s)
  if (![...streams].some((x) => x.path === '/api/v1/logs')) {
    clearInterval(logTimer)
    logTimer = undefined
  }
}

export function openStream(path: string, params: Record<string, string | string[]>) {
  const s = new DemoEventStream(path, params, onClose, onOpen)
  streams.add(s)
  return s
}

// ---------- Operations ----------
const ops = new Map<string, Operation>()
let opSeq = 0

interface OpSpec {
  /** Applied when the operation starts running (transient states: restarting…). */
  start?: () => void
  /** Applied when it ends; throwing fails the operation with the error's message. */
  apply?: () => void
  /** Running time, in milliseconds. */
  ms?: number
  /** Log line written once done. */
  log?: [container: string, line: string]
}

/**
 * Queues an operation like the daemon does (202): running shortly after, then succeeded (or failed) once `ms` has
 * elapsed. The progress is followed by polling `GET /api/v1/operations/{id}` and announced on the event stream.
 */
function operation(kind: string, spec: OpSpec = {}): Operation {
  const id = `demo-${++opSeq}`
  const op: Operation = { id, kind, status: 'queued', events: [], created: new Date().toISOString() }
  ops.set(id, op)
  const announce = () => broadcast('/api/v1/events', 'operation', op)
  announce()
  setTimeout(() => {
    op.status = 'running'
    op.started = new Date().toISOString()
    op.events.push({ operation: id, kind: 'output', level: 'info', message: `$ frankenmanager ${kind}`, time: op.started })
    spec.start?.()
    announce()
    emitContainers()
    setTimeout(() => {
      try {
        spec.apply?.()
        op.status = 'succeeded'
        if (spec.log) log(...spec.log)
      } catch (e) {
        op.status = 'failed'
        op.error = e instanceof Error ? e.message : String(e)
      }
      op.finished = new Date().toISOString()
      announce()
      emitContainers()
    }, spec.ms ?? 1_400)
  }, 250)
  return { ...op }
}

const caddyLog = (msg: string): [string, string] => ['franken-caddy-proxy', JSON.stringify({ level: 'info', msg })]

// ---------- Routes ----------
/** Simulated network latency, so that loading states show briefly as they do with a real instance. */
const latency = () => new Promise((r) => setTimeout(r, 60 + Math.random() * 120))

function notFound(path: string): never {
  throw new ApiError(404, t('errors.http', { status: 404 }) + ` (${path})`)
}

const glob = (pattern: string) => new RegExp(`^${(pattern || '*').replace(/[.+^${}()|[\]\\]/g, '\\$&').replaceAll('*', '.*').replaceAll('?', '.')}$`)

function get(path: string, q: URLSearchParams): unknown {
  let m: RegExpExecArray | null
  switch (path) {
    case '/api/health': return { version: DEMO_VERSION, api: 'v1', features: ['dashboard', 'update', 'docker', 'pairing'] }
    case '/api/v1/host': return hostInfo()
    case '/api/v1/host/system': return hostSystem()
    case '/api/v1/status': return status()
    case '/api/v1/containers': return containers()
    case '/api/v1/stats': return stats()
    case '/api/v1/hosts/archived': return world.archived.map((a) => ({ ...a }))
    case '/api/v1/services': return status().services
    case '/api/v1/settings': return SETTINGS().map((s) => ({ ...s, value: world.settings[s.key] ?? '', custom: (world.settings[s.key] ?? '') !== s.default }))
    case '/api/v1/dns': return { enabled: enabled('dns'), running: enabled('dns') }
    case '/api/v1/dns/clients': {
      // Devices seen lately keep querying while the page is open.
      for (const d of world.dnsClients) {
        if (d.status === 'connected' && Math.random() < 0.5) { d.queries += 1 + Math.floor(Math.random() * 5); d.last_seen = new Date().toISOString() }
      }
      const devices = enabled('dns') ? world.dnsClients : []
      return { enabled: enabled('dns'), state: enabled('dns') ? 'running' : 'stopped', connected: devices.filter((d) => d.status === 'connected').length, devices }
    }
    case '/api/v1/ca': return { sharing: world.ca.sharing, port: world.ca.port, url: world.ca.sharing ? `http://${DEMO_LAN_IP}:${world.ca.port}/` : undefined }
    case '/api/v1/update': return { version: DEMO_VERSION, latest: DEMO_VERSION, update_available: false, releases: [], command: 'sudo frankenmanager update', checked_at: new Date().toISOString() }
    case '/api/v1/redis': {
      const running = enabled('redis') && isUp('franken_redis')
      return {
        enabled: enabled('redis'), running,
        info: running ? { version: '7.4.1', uptime_seconds: 86_400 * 3, used_memory: String(24_600_000 + world.redisKeys.length * 2_048), connected_clients: 6, keys: world.redisKeys.length, hits: world.redisHits += 7, misses: world.redisMisses } : undefined,
      }
    }
    case '/api/v1/redis/keys': {
      const re = glob(q.get('pattern') ?? '*')
      return { cursor: '0', keys: world.redisKeys.filter((k) => re.test(k.name)) }
    }
    case '/api/v1/databases': return { servers: world.servers.map((s) => ({ ...s, running: isUp(s.container) })), admin_url: '' }
    case '/api/v1/s3': return { enabled: enabled('s3'), domain: 's3.frankenmanager.test', app_endpoint: 'http://franken_s3_gateway:3900', region: world.settings.S3_REGION ?? 'us-east-1', access_key_id: world.settings.S3_ACCESS_KEY_ID ?? '' }
    case '/api/v1/s3/buckets': return world.buckets.map(bucketInfo)
    case '/api/v1/s3/keys': return world.keys.map(({ secret: _secret, ...k }) => k)
    case '/api/v1/auth': return {
      mode: accessMode(), password_set: true, local_auth: world.settings.DASHBOARD_LOCAL_AUTH === 'true', remote_port: Number(world.settings.DASHBOARD_REMOTE_PORT) || DEMO_REMOTE_PORT,
      remote_active: accessMode() !== 'local', remote: true, session_required: true, authenticated: true,
    }
    case '/api/v1/auth/sessions': return world.sessions
    case '/api/v1/auth/log': return world.log
  }
  if ((m = /^\/api\/v1\/operations\/([^/]+)$/.exec(path))) {
    const op = ops.get(m[1]!)
    return op ? { ...op, events: [...op.events] } : notFound(path)
  }
  if ((m = /^\/api\/v1\/s3\/keys\/([^/]+)$/.exec(path))) return world.keys.find((k) => k.id === m![1]) ?? notFound(path)
  if ((m = /^\/api\/v1\/hosts\/([^/]+)\/caddyfile$/.exec(path))) return siteFile(decodeURIComponent(m[1]!))
  if ((m = /^\/api\/v1\/s3\/buckets\/([^/]+)\/objects$/.exec(path))) {
    const b = bucket(m[1]!)
    const prefix = q.get('prefix') ?? ''
    const inside = b.objects.filter((o) => o.key.startsWith(prefix))
    const folders = [...new Set(inside.map((o) => o.key.slice(prefix.length)).filter((k) => k.includes('/')).map((k) => `${prefix}${k.split('/')[0]}/`))]
    return {
      bucket: b.name, prefix, folders,
      objects: inside.filter((o) => !o.key.slice(prefix.length).includes('/')).map((o) => ({
        key: o.key, size: o.size, last_modified: new Date(o.modified).toISOString(), etag: `"${o.key.length.toString(16)}${o.size.toString(16)}"`,
        public: b.public || o.public, url: b.public || o.public ? `https://${b.name}.s3.frankenmanager.test/${o.key}` : undefined,
      })),
    }
  }
  if ((m = /^\/api\/v1\/s3\/buckets\/([^/]+)\/object\/link$/.exec(path))) {
    const b = bucket(m[1]!)
    const key = q.get('key') ?? ''
    const o = b.objects.find((x) => x.key === key) ?? notFound(path)
    if (b.public || o.public) return { url: `https://${b.name}.s3.frankenmanager.test/${key}`, public: true }
    const expires = Number(q.get('expires')) || 3600
    return { url: `https://s3.frankenmanager.test/${b.name}/${key}?X-Amz-Expires=${expires}&X-Amz-Signature=demo`, public: false, expires_at: new Date(Date.now() + expires * 1000).toISOString() }
  }
  return notFound(path)
}

function bucket(name: string) {
  return world.buckets.find((b) => b.name === decodeURIComponent(name)) ?? notFound(`/api/v1/s3/buckets/${name}`)
}

const host = (name: string) => world.hosts.find((h) => h.name === decodeURIComponent(name)) ?? notFound(`/api/v1/hosts/${name}`)

function conflict(message: string): never {
  throw new ApiError(409, message)
}

/** POST, PUT and DELETE: most start an operation. */
function act(method: string, path: string, body: Record<string, unknown>): unknown {
  const names = (body.names ?? body.domains ?? body.keys ?? body.ids ?? []) as string[]
  let m: RegExpExecArray | null

  switch (`${method} ${path}`) {
    case 'POST /api/v1/stack/start':
      return operation('start', { ms: 2_400, apply: () => { world.running = true; world.stopped.clear() }, log: caddyLog('serving initial configuration') })
    case 'POST /api/v1/stack/stop':
      return operation('stop', { ms: 1_800, apply: () => { world.running = false } })
    case 'POST /api/v1/stack/restart': {
      const targets = (body.targets ?? []) as string[]
      const affected = targets.includes('caddy') && body.build
        ? containerNames().filter((n) => n.startsWith('frankenphp-') || n === 'franken-caddy-proxy')
        : targets.length ? ['franken-caddy-proxy'] : containerNames()
      return operation('restart', {
        ms: body.build ? 3_200 : 2_000,
        start: () => affected.forEach((n) => world.restarting.add(n)),
        apply: () => { world.running = true; affected.forEach((n) => { world.restarting.delete(n); world.stopped.delete(n) }) },
        log: caddyLog(body.force_ssl ? 'certificates renewed' : 'config reloaded'),
      })
    }
    case 'POST /api/v1/hosts': {
      const [name] = names
      if (!name) throw new ApiError(400, 'no domain given')
      if (world.hosts.some((h) => h.name === name)) conflict(`${name} is already registered`)
      const h: Host = body.target
        ? { name, kind: 'proxy', target: String(body.target), websocket: !!body.websocket }
        : body.static
          ? { name, kind: 'static', root: String(body.root || defaultRoot(name, true)), spa: !!body.spa }
          : { name, kind: 'php', php: String(body.php || world.settings.DEFAULT_PHP_VERSION || '8.4'), root: String(body.root || defaultRoot(name)) }
      return operation('add-host', {
        ms: 2_200,
        apply: () => { world.hosts.push(h); world.archived = world.archived.filter((a) => a.domain !== name) },
        log: caddyLog(`certificate obtained for ${name}`),
      })
    }
    case 'POST /api/v1/hosts/remove':
      return operation('remove-host', {
        apply: () => {
          for (const n of names) {
            const h = world.hosts.find((x) => x.name === n)
            if (!h) continue
            if (h.kind === 'php' || h.kind === 'static') world.archived.push({ domain: n, kind: h.kind, php: h.php })
            delete world.caddyfiles[n]
          }
          world.hosts = world.hosts.filter((h) => !names.includes(h.name) && !(h.kind === 'alias' && names.includes(h.target ?? '')))
        },
        log: caddyLog('config reloaded'),
      })
    case 'POST /api/v1/hosts/restore':
      return operation('restore-host', {
        ms: 1_800,
        apply: () => {
          for (const n of names) {
            const a = world.archived.find((x) => x.domain === n)
            if (!a || world.hosts.some((h) => h.name === n)) continue
            world.hosts.push(body.static || a.kind === 'static'
              ? { name: n, kind: 'static', root: defaultRoot(n, true), spa: false }
              : { name: n, kind: 'php', php: String(body.php || a.php || '8.4'), root: defaultRoot(n) })
          }
          world.archived = world.archived.filter((a) => !names.includes(a.domain))
        },
        log: caddyLog('config reloaded'),
      })
    case 'PUT /api/v1/ca':
      return operation('ca', { apply: () => { world.ca.sharing = !!body.enabled; if (body.port) world.ca.port = Number(body.port) } })
    case 'POST /api/v1/dns/apply':
      return operation('restart-dns', { ms: 900 })
    case 'POST /api/v1/redis/flush':
      return operation('redis-flush', { ms: 700, apply: () => { world.redisKeys = [] }, log: ['franken_redis', '1:M DB flushed'] })
    case 'POST /api/v1/redis/keys/delete':
      return operation('redis-delete', { ms: 500, apply: () => { world.redisKeys = world.redisKeys.filter((k) => !names.includes(k.name)) } })
    case 'POST /api/v1/s3/buckets':
      return operation('s3-bucket', {
        ms: 900,
        apply: () => {
          for (const n of names) {
            if (world.buckets.some((b) => b.name === n)) throw new Error(`bucket ${n} already exists`)
            world.buckets.push({ name: n, public: !!body.public, objects: [] })
          }
        },
        log: ['franken_s3', `INFO garage_api: bucket ${names.join(', ')} created`],
      })
    case 'POST /api/v1/s3/buckets/delete':
      return operation('s3-bucket-delete', { ms: 900, apply: () => { world.buckets = world.buckets.filter((b) => !names.includes(b.name)); world.keys = world.keys.filter((k) => !names.includes(k.grants[0]?.bucket ?? '')) } })
    case 'POST /api/v1/s3/buckets/empty':
      return operation('s3-bucket-empty', { ms: 900, apply: () => world.buckets.filter((b) => names.includes(b.name)).forEach((b) => { b.objects = [] }) })
    case 'POST /api/v1/s3/buckets/public':
      return operation('s3-bucket-public', { ms: 600, apply: () => world.buckets.filter((b) => names.includes(b.name)).forEach((b) => { b.public = !!body.public }) })
    case 'POST /api/v1/s3/keys': {
      const b = String(body.bucket ?? '')
      return operation('s3-key', {
        ms: 700,
        apply: () => {
          bucket(b)
          const hex = (n: number) => Array.from({ length: n }, () => Math.floor(Math.random() * 16).toString(16)).join('')
          world.keys.push({ id: `GK${hex(24)}`, name: String(body.name || `${b}/key`), global: false, secret: hex(64), grants: [{ bucket: b, read: true, write: !body.read_only, owner: false }] })
        },
      })
    }
    case 'POST /api/v1/s3/keys/delete':
      return operation('s3-key-delete', { ms: 500, apply: () => { world.keys = world.keys.filter((k) => !names.includes(k.id)) } })
    case 'PUT /api/v1/auth/password':
      return operation('password', { ms: 600 })
    case 'POST /api/v1/auth/logout':
      return {}
    case 'POST /api/v1/auth/pairing':
      return pairing()
  }

  if ((m = /^\/api\/v1\/containers\/([^/]+)\/(start|stop|restart)$/.exec(path))) {
    const [, rawName, action] = m
    const name = decodeURIComponent(rawName!)
    if (!containerNames().includes(name)) notFound(path)
    return operation(action!, {
      ms: action === 'stop' ? 1_000 : 1_600,
      start: () => { if (action !== 'stop') world.restarting.add(name) },
      apply: () => {
        world.restarting.delete(name)
        if (action === 'stop') world.stopped.add(name)
        else { world.stopped.delete(name); world.running = true }
      },
    })
  }
  if ((m = /^\/api\/v1\/hosts\/([^/]+)\/(aliases|php|certificate|websocket|caddyfile|caddyfile\/validate|caddyfile\/reset)$/.exec(path))) {
    const h = host(m[1]!)
    switch (`${method} ${m[2]}`) {
      case 'POST aliases': {
        const aliases = (body.aliases ?? []) as string[]
        for (const a of aliases) if (world.hosts.some((x) => x.name === a)) conflict(`${a} is already registered`)
        return operation('add-alias', { apply: () => aliases.forEach((a) => world.hosts.push({ name: a, kind: 'alias', target: h.name })), log: caddyLog(`certificate obtained for ${aliases.join(', ')}`) })
      }
      case 'PUT php': {
        const php = String(body.php ?? '')
        return operation('switch-php', {
          ms: 2_400,
          start: () => world.restarting.add(`frankenphp-${php}`),
          apply: () => { h.php = php; world.restarting.delete(`frankenphp-${php}`) },
          log: [`frankenphp-${php}`, `${h.name} now served by PHP ${php}`],
        })
      }
      case 'POST certificate':
        return operation('certificate', { ms: 1_200, log: caddyLog(`certificate renewed for ${h.name}`) })
      case 'PUT websocket':
        return operation('websocket', { ms: 900, apply: () => { h.websocket = !!body.enabled } })
      case 'PUT caddyfile': {
        const content = String(body.content ?? '')
        const err = validateCaddyfile(content, { static: h.kind === 'static' })
        if (err) throw new ApiError(422, `Caddyfile:${err.line}: ${err.message}`)
        return operation('caddyfile', { ms: 900, apply: () => { world.caddyfiles[h.name] = content }, log: caddyLog('config reloaded') })
      }
      case 'POST caddyfile/validate': {
        const err = validateCaddyfile(String(body.content ?? ''), { static: h.kind === 'static' })
        if (err) throw new ApiError(422, `Caddyfile:${err.line}: ${err.message}`)
        return { valid: true }
      }
      case 'POST caddyfile/reset':
        return operation('caddyfile-reset', { ms: 700, apply: () => { delete world.caddyfiles[h.name] }, log: caddyLog('config reloaded') })
    }
  }
  if ((m = /^\/api\/v1\/services\/([^/]+)$/.exec(path)) && method === 'PUT') {
    const name = m[1] as ServiceName
    if (!SERVICE_KEYS[name]) notFound(path)
    return operation(`service ${body.enabled ? 'enable' : 'disable'} ${name}`, { ms: 2_000, apply: () => { world.settings[SERVICE_KEYS[name]] = String(!!body.enabled) } })
  }
  if ((m = /^\/api\/v1\/settings\/([A-Z0-9_]+)$/.exec(path)) && method === 'PUT') {
    const key = m[1]!
    const def = SETTINGS().find((s) => s.key === key) ?? notFound(path)
    const value = body.value === null ? def.default : String(body.value ?? '')
    if (def.values && value && !def.values.includes(value)) throw new ApiError(400, `Invalid value '${value}' for ${key}: expected one of ${def.values.join(', ')}`)
    return operation('configuration', { ms: 600, apply: () => { world.settings[key] = value } })
  }
  if ((m = /^\/api\/v1\/databases\/(mariadb|mysql|postgresql)(\/delete|\/users|\/users\/delete)?$/.exec(path))) {
    const engine = m[1]!
    const server = world.servers.find((s) => s.engine === engine) ?? notFound(path)
    switch (m[2] ?? '') {
      case '': {
        const name = String(body.name ?? '')
        if (server.databases.includes(name)) conflict(`database ${name} already exists`)
        return operation('db-create', { ms: 900, apply: () => { server.databases.push(name) } })
      }
      case '/delete':
        return operation('db-drop', { ms: 900, apply: () => { server.databases = server.databases.filter((d) => !names.includes(d)) } })
      case '/users': {
        const name = String(body.name ?? '')
        return operation('db-user', { ms: 700, apply: () => { if (!server.users.some((u) => u.name === name)) server.users.push({ name, host: engine === 'postgresql' ? undefined : '%' }) } })
      }
      case '/users/delete':
        return operation('db-user-drop', { ms: 700, apply: () => { server.users = server.users.filter((u) => !names.includes(u.name)) } })
    }
  }
  if ((m = /^\/api\/v1\/s3\/buckets\/([^/]+)\/objects\/(delete|public)$/.exec(path))) {
    const b = bucket(m[1]!)
    const keys = (body.keys ?? []) as string[]
    const hit = (key: string) => keys.some((k) => key === k || (k.endsWith('/') && key.startsWith(k)))
    return m[2] === 'delete'
      ? operation('s3-objects-delete', { ms: 700, apply: () => { b.objects = b.objects.filter((o) => !hit(o.key)) } })
      : operation('s3-objects-public', { ms: 600, apply: () => b.objects.filter((o) => hit(o.key)).forEach((o) => { o.public = !!body.public }) })
  }
  if ((m = /^\/api\/v1\/auth\/sessions\/([^/]+)$/.exec(path)) && method === 'DELETE') {
    const s = world.sessions.find((x) => x.id === decodeURIComponent(m![1]!)) ?? notFound(path)
    if (s.current) throw new ApiError(400, 'use logout to close the current session')
    world.sessions = world.sessions.filter((x) => x !== s)
    world.log.unshift({ at: new Date().toISOString(), ip: s.ip, event: 'revoked', device: s.device, remote: s.remote })
    return {}
  }
  return notFound(path)
}

/** A pairing code; a fictional tablet "scans" it a little later, as a real device would. */
let pairTimer: ReturnType<typeof setTimeout> | undefined
function pairing(): Schema<'Pairing'> {
  const id = `pair-${++opSeq}`
  const code = Array.from({ length: 26 }, () => 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789'[Math.floor(Math.random() * 32)]).join('')
  const url = `https://${DEMO_LAN_IP}:${world.settings.DASHBOARD_REMOTE_PORT || DEMO_REMOTE_PORT}`
  const ca = 'a3f19c0e5b7d2e48c6a1f09b3d7e5c2a8b4f61d09e3c7a5b2f8d4e6c1a9b3f70'
  clearTimeout(pairTimer)
  pairTimer = setTimeout(() => {
    const device = 'FrankenManager app · iPad'
    const session = `s-${opSeq}`
    world.sessions.push({ id: session, device, ip: '192.168.1.50', remote: true, created: new Date().toISOString(), last_seen: new Date().toISOString(), current: false })
    world.log.unshift({ at: new Date().toISOString(), ip: '192.168.1.50', event: 'paired', device, remote: true })
    broadcast('/api/v1/events', 'paired', { id, device, session } satisfies Schema<'PairedEvent'>)
  }, 25_000)
  return {
    id, code, url, name: DEMO_HOSTNAME, ca_fingerprint: ca, expires_at: new Date(Date.now() + 5 * 60_000).toISOString(),
    qr: `frankenmanager://pair?v=1&url=${encodeURIComponent(url)}&code=${code}&name=${DEMO_HOSTNAME}&ca=${ca}`,
  }
}

/** One request to the demo daemon: a GET reads, anything else acts. */
export async function handle(method: string, rawPath: string, body: unknown): Promise<unknown> {
  await latency()
  const u = new URL(rawPath, 'demo://daemon')
  const result = method === 'GET' ? get(u.pathname, u.searchParams) : act(method, u.pathname, (body ?? {}) as Record<string, unknown>)
  // A copy, as if it went through JSON: the store never holds references into the demo's state.
  return result === undefined ? null : JSON.parse(JSON.stringify(result))
}

/** S3 upload (`PUT …/object?key=…`): the file is only counted, not kept. */
export function upload(rawPath: string, file: Blob): Operation {
  const u = new URL(rawPath, 'demo://daemon')
  const m = /^\/api\/v1\/s3\/buckets\/([^/]+)\/object$/.exec(u.pathname) ?? notFound(u.pathname)
  const b = bucket(m[1]!)
  const key = u.searchParams.get('key') ?? 'upload.bin'
  return operation('s3-upload', {
    ms: 800,
    apply: () => {
      b.objects = b.objects.filter((o) => o.key !== key)
      b.objects.push({ key, size: file.size, modified: Date.now(), public: u.searchParams.get('public') === 'true' })
    },
    log: ['franken_s3', `INFO garage_api: PUT ${b.name}/${key} 200`],
  })
}
