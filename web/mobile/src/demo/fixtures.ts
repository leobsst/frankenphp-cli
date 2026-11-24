import type { Schema } from '@app/lib/api'

/**
 * Fictional dataset of the demo mode, inspired by the dashboard prototype (frankenmanager-gui-poc): a developer's
 * laptop serving a few local projects. Everything here is data a daemon would return (domain names, container
 * names, log lines, setting descriptions), so it stays in English like the daemon's own; the interface texts come
 * from the catalogs. No address belongs to a real machine.
 */

export const DEMO_VERSION = '0.15.0'
export const DEMO_HOSTNAME = 'MacBook-Pro'
/** Fake LAN address of the demo machine (private range, never the user's). */
export const DEMO_LAN_IP = '192.168.1.20'
export const DEMO_REMOTE_PORT = 9443
export const DEMO_REMOTE_URL = `https://${DEMO_LAN_IP}:${DEMO_REMOTE_PORT}`

type Host = Schema<'Host'>
type Setting = Schema<'Setting'>

export const HOSTS = (): Host[] => [
  { name: 'shop.test', kind: 'php', php: '8.4', root: 'shop/public' },
  { name: 'www.shop.test', kind: 'alias', target: 'shop.test' },
  { name: 'api.test', kind: 'php', php: '8.5', root: 'monorepo/apps/api/public' },
  { name: 'legacy.test', kind: 'php', php: '8.2', root: 'legacy/public' },
  { name: 'studio.test', kind: 'proxy', target: '127.0.0.1:3000', websocket: true },
  { name: 'front.test', kind: 'static', root: 'front/apps/web/dist', spa: true },
  { name: 'docs.test', kind: 'static', root: 'docs', spa: false },
]

export const ARCHIVED = (): Schema<'ArchivedHost'>[] => [{ domain: 'old-blog.test', kind: 'php', php: '8.4' }]

/** Settings: [key, value, default, description, accepted values, secret, apply]. */
type Row = [string, string, string, string, string[]?, boolean?, Setting['apply']?]
const B = ['true', 'false']
const ROWS: Row[] = [
  ['APP_ENV', 'dev', 'dev', 'dev for local development; prod/production only on a public/cloud server', ['dev', 'prod', 'production'], false, 'recreate'],
  ['CLI_ALIAS', 'fkm', 'fkm', 'Short alias of the frankenmanager command (empty: no alias)', undefined, false, 'alias'],
  ['CLI_LANGUAGE', 'auto', 'auto', 'Language of the messages: auto follows the system language (LANG, LC_ALL...)', ['auto', 'en', 'fr'], false, 'now'],
  ['DEFAULT_PROJECT_PATH', '~/Sites', '~/Sites', 'Projects root used when start has no --path', undefined, false, 'now'],
  ['DEFAULT_PHP_VERSION', '8.4', '8.3', 'PHP version of new domains when --php is not given', ['8.2', '8.3', '8.4', '8.5'], false, 'now'],
  ['EXPOSE_SERVICES', 'false', 'false', 'Expose the service ports to the network instead of 127.0.0.1', B, false, 'recreate'],
  ['WEB_HTTP_PORT', '80', '80', 'HTTP port of the web server', undefined, false, 'recreate'],
  ['WEB_HTTPS_PORT', '443', '443', 'HTTPS port of the web server', undefined, false, 'recreate'],
  ['DB_ENGINES', 'mariadb,postgresql', 'mariadb', 'Database engines, comma-separated: mariadb, mysql, postgresql', undefined, false, 'recreate'],
  ['DB_PORT', '3306', '3306', 'MariaDB port', undefined, false, 'recreate'],
  ['MARIADB_VERSION', '10.11.9', '10.11.9', 'MariaDB image version', undefined, false, 'recreate'],
  ['MARIADB_ROOT_PASSWORD', 'demo-root-password', '', 'MariaDB root password (generated on start when empty)', undefined, true, 'restart_db'],
  ['MYSQL_MAX_ALLOWED_PACKET', '512M', '512M', 'Maximum packet size of MariaDB and MySQL', undefined, false, 'recreate'],
  ['POSTGRES_VERSION', '16', '16', 'PostgreSQL image version', undefined, false, 'recreate'],
  ['POSTGRES_PORT', '5432', '5432', 'PostgreSQL port', undefined, false, 'recreate'],
  ['PMA_PORT', '8080', '8080', 'phpMyAdmin port (dev only)', undefined, false, 'recreate'],
  ['DASHBOARD_PORT', '9081', '9081', 'Port of the dashboard API (frankenmanager daemon), on 127.0.0.1', undefined, false, 'now'],
  ['DASHBOARD_ACCESS', 'lan', 'local', 'Who reaches the dashboard: local (this machine), lan (also the local networks) or internet (also any address); lan and internet need DASHBOARD_PASSWORD', ['local', 'lan', 'internet'], false, 'daemon'],
  ['DASHBOARD_REMOTE_PORT', String(DEMO_REMOTE_PORT), '9443', 'HTTPS port of the remote access to the dashboard (lan, internet)', undefined, false, 'daemon'],
  ['DASHBOARD_PASSWORD', '$argon2id$v=19$demo', '', 'Password of the dashboard (stored hashed), required for the remote access', undefined, true, 'daemon'],
  ['DASHBOARD_ALLOWED_IPS', '', '', 'Addresses allowed to reach the dashboard remotely (IPv4, IPv6, CIDR, comma-separated); empty allows every address', undefined, false, 'daemon'],
  ['DASHBOARD_LOCAL_AUTH', 'false', 'false', 'Require the dashboard password on this machine too', B, false, 'daemon'],
  ['DASHBOARD_TLS_CERT', '', '', 'Certificate (PEM file) of the remote access; generated with mkcert when empty', undefined, false, 'daemon'],
  ['DASHBOARD_TLS_KEY', '', '', 'Private key (PEM file) of DASHBOARD_TLS_CERT', undefined, false, 'daemon'],
  ['DAEMON_AUTOSTART', 'true', 'true', 'Run the daemon (dashboard API, LAN DNS server) at login, through the OS service manager', B, false, 'now'],
  ['REDIS_ENABLED', 'true', 'false', 'Run a Redis cache server', B, false, 'live'],
  ['REDIS_PORT', '6379', '6379', 'Redis port', undefined, false, 'recreate'],
  ['SOKETI_ENABLED', 'false', 'false', 'Run a Soketi WebSocket server at wss://ws.frankenmanager.test', B, false, 'live'],
  ['SOKETI_PORT', '6001', '6001', 'Soketi port', undefined, false, 'recreate'],
  ['SOKETI_DEFAULT_APP_ID', 'demo-app', '', 'Soketi app ID (generated on start when empty)', undefined, false, 'recreate'],
  ['SOKETI_DEFAULT_APP_KEY', 'demo-key', '', 'Soketi app key (generated on start when empty)', undefined, false, 'recreate'],
  ['SOKETI_DEFAULT_APP_SECRET', 'demo-secret', '', 'Soketi app secret (generated on start when empty)', undefined, true, 'recreate'],
  ['S3_ENABLED', 'true', 'false', 'Run a Garage S3 server at https://s3.frankenmanager.test', B, false, 'live'],
  ['S3_DIR', './s3', './s3', 'S3 objects directory (relative to the data directory, or absolute)', undefined, false, 'recreate'],
  ['S3_PORT', '3900', '3900', 'S3 API port', undefined, false, 'recreate'],
  ['S3_ADMIN_PORT', '3903', '3903', 'S3 admin API port (always on 127.0.0.1)', undefined, false, 'recreate'],
  ['S3_REGION', 'us-east-1', 'us-east-1', 'S3 region', undefined, false, 'recreate'],
  ['S3_ACCESS_KEY_ID', 'GK3c9e0a7d51f24b86e1a0c7d2', '', 'Global S3 access key ID, allowed on every bucket (generated on start when empty)', undefined, false, 'recreate'],
  ['S3_SECRET_ACCESS_KEY', '9f2c4e6a8b0d1f3e5a7c9b1d3f5e7a9c0b2d4f6e8a1c3e5b7d9f0a2c4e6b8d0f', '', 'Global S3 secret access key (generated on start when empty)', undefined, true, 'recreate'],
  ['DNS_SERVER', 'true', 'false', 'Run a LAN DNS server resolving every domain to this machine', B, false, 'live'],
  ['DNS_IP', '', '', "IP the domains resolve to (empty: this machine's LAN IP, followed)", undefined, false, 'restart_dns'],
  ['DNS_LISTEN_IP', '', '', 'Address the DNS server listens on (empty: every interface)', undefined, false, 'restart_dns'],
  ['DNS_PORT', '53', '53', 'Port of the DNS server', undefined, false, 'restart_dns'],
  ['DNS_UPSTREAM', '1.1.1.1 9.9.9.9', '1.1.1.1 8.8.8.8', 'Space-separated resolvers for the other queries', undefined, false, 'restart_dns'],
  ['PHP_IMAGE_DISTRO', 'alpine', 'alpine', 'Base distribution of the PHP images', ['alpine', 'trixie'], false, 'rebuild'],
  ['INSTALL_PDFTK', 'false', 'false', 'Install pdftk in the PHP images (adds a Java runtime, ~200 MB)', B, false, 'rebuild'],
  ['EXTRA_APK_PACKAGES', 'imagemagick', '', 'Extra Alpine packages of every PHP image, space-separated', undefined, false, 'rebuild'],
]

export const SETTINGS = (): Setting[] =>
  ROWS.map(([key, value, def, description, values, secret = false, apply = 'now']) => ({
    key, value, default: def, description, values, secret, apply, custom: value !== def,
    applies: apply === 'recreate' ? 'stop/start' : apply === 'rebuild' ? 'restart --build' : apply === 'restart_dns' ? 'restart --dns' : '',
  }))

const ago = (ms: number) => new Date(Date.now() - ms).toISOString()
const MIN = 60_000
const HOUR = 60 * MIN
const DAY = 24 * HOUR

export const DNS_CLIENTS = (): Schema<'DNSClient'>[] => [
  { ip: '192.168.1.23', name: 'iPhone', mac: 'a6:3f:12:9c:e0:41', randomized_mac: true, first_seen: ago(9 * DAY), last_seen: ago(20_000), queries: 1284, status: 'connected' },
  { ip: '192.168.1.31', name: 'Pixel-8', mac: '7e:b2:05:4d:18:c3', randomized_mac: true, first_seen: ago(6 * DAY), last_seen: ago(95_000), queries: 642, status: 'connected' },
  { ip: '192.168.1.12', name: 'MacBook-Air', mac: '3c:22:fb:4a:91:0e', randomized_mac: false, first_seen: ago(20 * DAY), last_seen: ago(190_000), queries: 2310, status: 'connected' },
  { ip: '192.168.1.50', name: 'iPad', mac: '5a:90:c1:7e:22:b8', randomized_mac: true, first_seen: ago(3 * DAY), last_seen: ago(2 * HOUR), queries: 87, status: 'idle' },
  { ip: '192.168.1.58', name: 'DESKTOP-7K2QM', mac: 'd8:bb:c1:4e:73:09', randomized_mac: false, first_seen: ago(12 * DAY), last_seen: ago(140_000), queries: 341, status: 'connected' },
  { ip: '192.168.1.64', name: '', mac: '', randomized_mac: false, first_seen: ago(8 * DAY), last_seen: ago(6 * DAY), queries: 12, status: 'idle' },
]

export const REDIS_KEYS = () => [
  { name: 'shop:products', type: 'hash', ttl: -1 },
  { name: 'shop:categories', type: 'list', ttl: -1 },
  { name: 'shop:cart:7f3a', type: 'hash', ttl: 3540 },
  { name: 'api:rate-limit:192.168.1.23', type: 'string', ttl: 42 },
  { name: 'api:rate-limit:192.168.1.31', type: 'string', ttl: 17 },
  { name: 'laravel_cache:settings', type: 'string', ttl: 86100 },
  { name: 'laravel_database_queues:default', type: 'list', ttl: -1 },
  { name: 'sessions:active', type: 'set', ttl: -1 },
  { name: 'leaderboard:weekly', type: 'zset', ttl: 518400 },
  { name: 'orders:events', type: 'stream', ttl: -1 },
  { name: 'laravel_session:Zx81Qm', type: 'string', ttl: 7140 },
]

export const DATABASES = (): Schema<'DatabasesState'>['servers'] => [
  {
    engine: 'mariadb', container: 'franken_mariadb', running: true, databases: ['shop', 'legacy', 'reporting'],
    users: [{ name: 'shop', host: '%' }, { name: 'legacy', host: '%' }, { name: 'reporting', host: '%' }],
  },
  { engine: 'postgresql', container: 'franken_postgresql', running: true, databases: ['api'], users: [{ name: 'api' }] },
]

export interface DemoObject { key: string; size: number; modified: number; public: boolean }
export interface DemoBucket { name: string; public: boolean; objects: DemoObject[] }

export const BUCKETS = (): DemoBucket[] => {
  const now = Date.now()
  return [
    {
      name: 'media', public: false, objects: [
        { key: 'avatars/42.webp', size: 48_312, modified: now - HOUR, public: true },
        { key: 'avatars/57.webp', size: 51_904, modified: now - 2 * HOUR, public: true },
        { key: 'chat/media/42/photo.jpg', size: 1_284_662, modified: now - 10 * MIN, public: true },
        { key: 'chat/media/42/voice.ogg', size: 312_040, modified: now - 9 * MIN, public: false },
        { key: 'exports/orders-2026-11.csv', size: 884_120, modified: now - DAY, public: false },
        { key: 'logo.svg', size: 6_210, modified: now - 5 * DAY, public: true },
        { key: 'terms.pdf', size: 248_771, modified: now - 12 * DAY, public: false },
      ],
    },
    {
      name: 'invoices', public: false, objects: [
        { key: '2026/11/F-2026-0418.pdf', size: 92_014, modified: now - 2 * DAY, public: false },
        { key: '2026/11/F-2026-0419.pdf', size: 88_350, modified: now - DAY, public: false },
      ],
    },
    {
      name: 'docs-site', public: true, objects: [
        { key: 'index.html', size: 18_224, modified: now - 3 * DAY, public: true },
        { key: 'assets/app.js', size: 214_008, modified: now - 3 * DAY, public: true },
      ],
    },
  ]
}

export const S3_KEYS = (): (Schema<'AccessKey'> & { secret: string })[] => [
  { id: 'GK8b2e4f61a9c03d7e5b1f9a24', name: 'media/app', global: false, secret: '4b7e1c9a2f58d03e6c1b94a7f2d85e0c3b6a19f4e72d08c5b3a16e9f4d27c0b8', grants: [{ bucket: 'media', read: true, write: true, owner: false }] },
  { id: 'GK1d7c3a95e2f04b8c6a3e7d10', name: 'media/cdn', global: false, secret: 'c2e8a51f7b94d06e3a1c85f2b7d49e0a6c3f18b5d92e47a0c6b31f8e5a2d97c4', grants: [{ bucket: 'media', read: true, write: false, owner: false }] },
]

export const SESSIONS = (): Schema<'SessionInfo'>[] => [
  { id: 's-demo', device: 'FrankenManager app · iPhone', ip: '192.168.1.23', remote: true, created: ago(2 * HOUR), last_seen: ago(5_000), current: true },
  { id: 's-3f9a', device: 'Safari · MacBook-Pro', ip: '127.0.0.1', remote: false, created: ago(3 * DAY), last_seen: ago(MIN), current: false },
  { id: 's-c47d', device: 'Firefox · DESKTOP-7K2QM', ip: '192.168.1.58', remote: true, created: ago(20 * DAY), last_seen: ago(9 * DAY), current: false },
]

export const ACCESS_LOG = (): Schema<'AccessEntry'>[] => [
  { at: ago(2 * HOUR), ip: '192.168.1.23', event: 'paired', device: 'FrankenManager app · iPhone', remote: true },
  { at: ago(26 * HOUR), ip: '192.168.1.58', event: 'login_failed', remote: true },
  { at: ago(3 * DAY), ip: '127.0.0.1', event: 'login', device: 'Safari · MacBook-Pro', remote: false },
  { at: ago(9 * DAY), ip: '192.168.1.58', event: 'logout', device: 'Firefox · DESKTOP-7K2QM', remote: true },
  { at: ago(20 * DAY), ip: '192.168.1.58', event: 'login', device: 'Firefox · DESKTOP-7K2QM', remote: true },
]

/** Container → [base CPU %, base memory MiB]. */
export const LOAD: Record<string, [number, number]> = {
  'frankenphp-8.5': [3, 380], 'frankenphp-8.4': [4, 420], 'frankenphp-8.3': [2, 350], 'frankenphp-8.2': [1, 310],
  'franken-caddy-proxy': [1, 64], franken_redis: [0.6, 24], franken_soketi: [0.4, 48], franken_s3: [0.8, 96],
  franken_s3_gateway: [0.2, 12], franken_mariadb: [2, 780], franken_postgresql: [0.5, 390], franken_phpmyadmin: [0.1, 40],
}

/** Plausible log lines per container, picked at random by the log stream. */
export const LOG_LINES: Record<string, string[]> = {
  'franken-caddy-proxy': [
    '{"level":"info","msg":"handled request","request":{"method":"GET","host":"shop.test","uri":"/"},"status":200}',
    '{"level":"info","msg":"handled request","request":{"method":"GET","host":"front.test","uri":"/assets/index-4f2a.js"},"status":200}',
    '{"level":"info","msg":"handled request","request":{"method":"POST","host":"api.test","uri":"/v1/orders"},"status":201}',
    '{"level":"info","msg":"handled request","request":{"method":"GET","host":"docs.test","uri":"/getting-started"},"status":200}',
    '{"level":"info","msg":"handled request","request":{"method":"GET","host":"studio.test","uri":"/"},"status":200}',
    '{"level":"warn","msg":"handled request","request":{"method":"GET","host":"legacy.test","uri":"/old-page"},"status":404}',
  ],
  'frankenphp-8.4': [
    'shop.test "GET /cart HTTP/2.0" 200 38ms',
    'shop.test "POST /checkout HTTP/2.0" 302 112ms',
    'Processed App\\Jobs\\SendOrderConfirmation (54 ms)',
    'Cache hit: shop:products',
  ],
  'frankenphp-8.5': [
    'api.test "GET /v1/products?page=2 HTTP/2.0" 200 21ms',
    'api.test "POST /v1/orders HTTP/2.0" 201 54ms',
    'Worker ready: 4 threads, opcache enabled',
  ],
  'frankenphp-8.2': [
    'legacy.test "GET /admin HTTP/2.0" 200 112ms',
    'PHP Deprecated: Creation of dynamic property Cart::$total is deprecated in /projects/legacy/src/Cart.php on line 48',
  ],
  franken_redis: ['1:M Background saving started by pid 42', '1:M DB saved on disk', '1:M 100 changes in 300 seconds. Saving...'],
  franken_mariadb: ['[Note] InnoDB: Buffer pool(s) load completed', '[Note] Health check: OK', '[Warning] Aborted connection 118 to db: shop (Got timeout reading communication packets)'],
  franken_postgresql: ['LOG: checkpoint starting: time', 'LOG: checkpoint complete: wrote 42 buffers (0.3%)'],
  franken_s3: ['INFO garage_api: PUT media/chat/media/42/photo.jpg 200', 'INFO garage_block: resync queue empty'],
  franken_s3_gateway: ['GET docs-site/index.html 200 (public-read)'],
}
