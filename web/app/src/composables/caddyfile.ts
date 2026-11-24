/**
 * Caddyfile of each site, as the CLI generates them (internal/core/caddyfile):
 * - PHP project: `~/.frankenmanager/caddy/sites/php-<version>/<domain without TLD>_Caddyfile`;
 * - static host (v0.9.0): `~/.frankenmanager/caddy/sites/static/<domain without TLD>_Caddyfile`.
 * Aliases and proxy hosts have no Caddyfile of their own.
 */

import { t } from '../i18n'

/** Copy of the CLI's `caddy/Caddyfile.template` (v0.4.0). */
export const SITE_TEMPLATE = "full_domain {\n\t# TLS\n\ttls /certs/full_domain.pem /certs/full_domain-key.pem\n\t\n\t# Root project\n\troot * /{$CUSTOM_PATH}/custom_domain/public/\n\n\t# Compression Brotli and Gzip (if not using websocket)\n\tencode br gzip\n\n\t# If using websocket don't use any compression\n\t# @notWebsocket {\n\t#\t not path /app*\n\t# }\n\t# encode @notWebsocket br gzip\n\n\t# Logging\n\tlog {\n\t\toutput file /var/log/caddy/custom_domain_access.log\n\t\tformat console\n\t}\n\n\t# Cache and security headers\n\t@static path /css/* /js/* /images/* /fonts/* /favicon.ico\n\theader @static Cache-Control \"public, max-age=31536000, immutable\"\n\n\theader {\n\t\t# Security headers\n\t\tStrict-Transport-Security \"max-age=15552000; includeSubDomains; preload\"\n\t\tX-Frame-Options \"SAMEORIGIN\"\n\t\t#X-Content-Type-Options \"nosniff\"\n\t\tX-XSS-Protection \"1; mode=block\"\n\t\tReferrer-Policy \"no-referrer\"\n\t\t\n\t\t# Remove server and PHP version headers\n\t\t-Server\n\t\t-X-Powered-By\n\t}\n\n\t# @websocket {\n\t# \tpath /app*\n\t# }\n\t# handle @websocket {\n\t# \treverse_proxy 127.0.0.1:7006\n\t# }\n\n\t# Cache for robots.txt file\n\t# @robots {\n\t# \tpath /robots.txt\n\t# }\n\t# handle @robots {\n\t# \theader Cache-Control \"public, max-age=86400\"\n\t# \tfile_server\n\t# }\n\n\t# Manage PHP requests with FrankenPHP\n\tphp_server {\n\t\tindex index.php\n\t\tresolve_root_symlink\n\t}\n}"

/** Domain without its TLD ("my.app.test" -> "my.app"): name of the site's file and log. */
export const simpleDomain = (domain: string) => (domain.includes('.') ? domain.slice(0, domain.lastIndexOf('.')) : domain)

/** Default served folder, relative to the projects folder: `<domain without TLD>/public` for PHP, `<domain without TLD>` for static. */
export const defaultRoot = (domain: string, isStatic = false) => (isStatic ? simpleDomain(domain) : `${simpleDomain(domain)}/public`)

/** Root as seen by Caddy (the CLI's `RootPath`): the projects folder is mounted on `/{$CUSTOM_PATH}`. */
export const rootPath = (root: string) => {
  const r = root.replaceAll('\\', '/').replace(/^\/+|\/+$/g, '')
  return !r || r === '.' ? '/{$CUSTOM_PATH}/' : `/{$CUSTOM_PATH}/${r}/`
}

/** No `php`: static host, in `sites/static/`. */
export const siteCaddyfilePath = (domain: string, php?: string) => `~/.frankenmanager/caddy/sites/${php ? `php-${php}` : 'static'}/${simpleDomain(domain)}_Caddyfile`

/**
 * File generated for a PHP project: the template with full_domain and custom_domain replaced and the root given by
 * `--root` (v0.9.0), served over HTTP (TLS terminated by the reverse proxy): `http://` before the address, `tls` lines commented out.
 */
export function generateSiteCaddyfile(domain: string, root = defaultRoot(domain)) {
  const content = SITE_TEMPLATE.replaceAll('full_domain', domain).replaceAll('custom_domain', simpleDomain(domain))
    .replace(/root \* [^\n]+/, () => `root * ${rootPath(root)}`)
  return content.replace(/^([^\s#][^\n{]*?)\s*\{/m, (_, addr: string) => `http://${addr.trim()} {`).replace(/(\t)(tls \/certs\/[^\n]+)/g, '$1# $2')
}

/**
 * File of a static host (the CLI's `staticSite`, v0.9.0): served over HTTPS by the reverse proxy itself, which
 * keeps its `tls` line. zstd and gzip compression since v0.9.1: the reverse proxy's Caddy image has no Brotli encoder. Without `--spa`, the fallback to /index.html stays commented out.
 */
export function generateStaticCaddyfile(domain: string, root: string, spa: boolean) {
  const lines = [
    `${domain} {`,
    `\ttls /certs/${domain}.pem /certs/${domain}-key.pem`,
    '',
    `\troot * ${rootPath(root)}`,
    '',
    '\tencode zstd gzip',
    '',
    '\tlog {',
    `\t\toutput file /var/log/caddy/${simpleDomain(domain)}_access.log {`,
    '\t\t\troll_size 10mb',
    '\t\t\troll_keep 5',
    '\t\t\troll_keep_for 720h',
    '\t\t}',
    '\t\tformat console',
    '\t}',
    '',
    '\theader {',
    '\t\tX-Frame-Options "SAMEORIGIN"',
    '\t\tReferrer-Policy "no-referrer"',
    '\t\t-Server',
    '\t\t-X-Powered-By',
    '\t}',
    '',
    ...(spa
      ? [
          '\t# The entry page is never cached: it references the built assets',
          '\t@html path / /index.html',
          '\theader @html Cache-Control "no-cache"',
          '',
          '\t# Single-page app: unknown paths fall back to /index.html',
          '\ttry_files {path} /index.html',
        ]
      : ['\t# Single-page app: uncomment to fall back to /index.html', '\t# try_files {path} /index.html']),
    '\tfile_server',
    '}',
  ]
  return lines.join('\n') + '\n'
}

/** Checks that braces are balanced, ignoring comments; returns the faulty line. */
export function validateCaddyfile(text: string, opts: { static?: boolean } = {}): { line: number; message: string } | null {
  if (!text.trim()) return { line: 1, message: t('errors.caddyfile.empty') }
  // v0.9.1: the reverse proxy (standard Caddy image) has no Brotli encoder; Caddy would refuse to start.
  if (opts.static) {
    const i = text.split('\n').findIndex((l) => /^\s*encode\b[^\n{#]*\sbr\b/.test(l))
    if (i >= 0) return { line: i + 1, message: t('errors.caddyfile.brotli', { line: i + 1 }) }
  }
  const open: number[] = []
  const lines = text.split('\n')
  for (const [i, raw] of lines.entries()) {
    const line = raw.replace(/(^|\s)#.*$/, '')
    for (const ch of line) {
      if (ch === '{') open.push(i + 1)
      else if (ch === '}' && !open.pop()) return { line: i + 1, message: t('errors.caddyfile.unopened', { line: i + 1 }) }
    }
  }
  if (open.length) return { line: open[open.length - 1], message: t('errors.caddyfile.unclosed', { line: open[open.length - 1] }) }
  return null
}

// WebSocket toggle, following the steps of the CLI README: compression without /app*, then the @websocket block.
const WS_SWAPS: [commented: string, active: string][] = [
  ['\tencode br gzip\n', '\t# encode br gzip\n'],
  ['\t# @notWebsocket {\n\t#\t not path /app*\n\t# }\n\t# encode @notWebsocket br gzip', '\t@notWebsocket {\n\t\tnot path /app*\n\t}\n\tencode @notWebsocket br gzip'],
  ['\t# @websocket {\n\t# \tpath /app*\n\t# }\n\t# handle @websocket {\n\t# \treverse_proxy 127.0.0.1:7006\n\t# }', '\t@websocket {\n\t\tpath /app*\n\t}\n\thandle @websocket {\n\t\treverse_proxy 127.0.0.1:7006\n\t}'],
]

export const hasWebsocket = (text: string) => /^\s*@websocket\s*\{/m.test(text)

/** Enables or disables WebSocket; returns null if the template blocks were edited by hand. */
export function toggleWebsocket(text: string, on: boolean): string | null {
  let out = text
  for (const [off, active] of WS_SWAPS) {
    const [from, to] = on ? [off, active] : [active, off]
    if (!out.includes(from)) return null
    out = out.replace(from, to)
  }
  return out
}
