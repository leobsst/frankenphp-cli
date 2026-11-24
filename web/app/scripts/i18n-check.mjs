#!/usr/bin/env node
/**
 * Keeps the dashboard translations (web/app/src/locales/*.json) from drifting. Fails when:
 *   - the catalogs do not have exactly the same keys, or a value is not a non-empty string;
 *   - a message does not compile (vue-i18n syntax: `{name}`, plurals with `|`, `@` and braces escaped as `{'@'}`);
 *   - the same message uses different placeholders in two languages;
 *   - a static key used in the code (t('…'), $t('…'), i18n.global.t('…'), keypath="…") is missing.
 * Keys that nothing seems to use are reported as warnings: keys built at runtime (t(`nav.${name}`)) count as used
 * through their static prefix, and keys written as plain strings ('nav.overview') through that string.
 *
 * Usage: pnpm --dir web i18n:check
 */
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const appDir = join(dirname(fileURLToPath(import.meta.url)), '..')
const webDir = join(appDir, '..')
const localesDir = join(appDir, 'src', 'locales')
const sources = ['app/src', 'embedded/src', 'mobile/src'].map((d) => join(webDir, d))

// The vue-i18n message parser, reached through vue-i18n's own dependencies (pnpm does not hoist them).
const requireFrom = (from) => createRequire(from)
const coreBase = requireFrom(requireFrom(join(appDir, 'package.json')).resolve('vue-i18n')).resolve('@intlify/core-base')
const { createParser } = requireFrom(coreBase)('@intlify/message-compiler')

const errors = []
const warnings = []

/** Flattens a nested catalog into "a.b.c" → value. */
function flatten(obj, prefix = '', out = new Map()) {
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object' && !Array.isArray(v)) flatten(v, key, out)
    else out.set(key, v)
  }
  return out
}

/** Syntax errors of a message, and its placeholders. */
function inspect(message) {
  const problems = []
  const parser = createParser({ onError: (e) => problems.push(e.message) })
  try { parser.parse(message) } catch (e) { problems.push(e.message) }
  // The parser lets an unclosed brace at the end through.
  const bare = message.replace(/\{'(?:[^'\\]|\\.)*'\}/g, '')
  let depth = 0
  for (const c of bare) {
    if (c === '{') depth++
    else if (c === '}') depth--
    if (depth < 0 || depth > 1) break
  }
  if (depth !== 0) problems.push('unbalanced braces')
  const placeholders = new Set([...bare.matchAll(/\{\s*([A-Za-z_][\w]*|\d+)\s*\}/g)].map((m) => m[1]))
  return { problems: [...new Set(problems)], placeholders }
}

// ---------- Catalogs ----------
const catalogs = Object.fromEntries(
  readdirSync(localesDir).filter((f) => f.endsWith('.json')).sort().map((f) => [f.replace(/\.json$/, ''), flatten(JSON.parse(readFileSync(join(localesDir, f), 'utf8')))]),
)
const langs = Object.keys(catalogs)
if (langs.length < 2) errors.push(`expected at least two catalogs in ${relative(webDir, localesDir)}, found ${langs.join(', ') || 'none'}`)
const allKeys = new Set(langs.flatMap((l) => [...catalogs[l].keys()]))

for (const key of [...allKeys].sort()) {
  const seen = {}
  for (const lang of langs) {
    const value = catalogs[lang].get(key)
    if (value === undefined) { errors.push(`${lang}: missing key ${key}`); continue }
    if (typeof value !== 'string') { errors.push(`${lang}: ${key} is not a string`); continue }
    if (!value.trim()) { errors.push(`${lang}: empty value for ${key}`); continue }
    const { problems, placeholders } = inspect(value)
    for (const p of problems) errors.push(`${lang}: ${key} does not compile (${p}): ${JSON.stringify(value)}`)
    seen[lang] = placeholders
  }
  const withValues = Object.entries(seen)
  for (const [lang, set] of withValues.slice(1)) {
    const [refLang, ref] = withValues[0]
    const a = [...ref].sort().join(', ')
    const b = [...set].sort().join(', ')
    if (a !== b) errors.push(`${key}: placeholders differ between ${refLang} {${a}} and ${lang} {${b}}`)
  }
}

// ---------- Keys used in the code ----------
function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) walk(path, out)
    else if (/\.(vue|ts|mjs)$/.test(name) && !name.endsWith('.d.ts') && name !== 'api.gen.ts') out.push(path)
  }
  return out
}

const STATIC_CALL = /(?:^|[^\w$.])(?:\$t|t|tc|te|i18n\.global\.t|i18n\.t)\(\s*(['"])([^'"\n]+?)\1/g
const KEYPATH = /\bkeypath=(['"])([^'"]+)\1/g
const TEMPLATE_CALL = /(?:^|[^\w$.])(?:\$t|t|te|i18n\.global\.t|i18n\.t)\(\s*`([^`$]*)\$\{/g
const STRING = /(['"`])([\w-]+(?:\.[\w-]+)+)\1/g

const used = new Map()
const prefixes = new Set()
const literals = new Set()
for (const file of sources.flatMap((d) => walk(d))) {
  const code = readFileSync(file, 'utf8')
  const where = relative(webDir, file)
  for (const re of [STATIC_CALL, KEYPATH]) for (const m of code.matchAll(re)) {
    if (!used.has(m[2])) used.set(m[2], where)
  }
  for (const m of code.matchAll(TEMPLATE_CALL)) if (m[1]) prefixes.add(m[1])
  for (const m of code.matchAll(STRING)) literals.add(m[2])
}

const reference = catalogs[langs[0]] ?? new Map()
for (const [key, where] of used) {
  if (reference.has(key)) continue
  // A namespace passed whole (tm('…')) or a key pointing at a sub-tree.
  if ([...reference.keys()].some((k) => k.startsWith(`${key}.`))) continue
  errors.push(`${where}: key ${key} is not in the catalogs`)
}
for (const key of reference.keys()) {
  if (used.has(key) || literals.has(key) || [...prefixes].some((p) => key.startsWith(p))) continue
  warnings.push(`unused key? ${key}`)
}

// ---------- Report ----------
for (const w of warnings) console.warn(`warning: ${w}`)
for (const e of errors) console.error(`error: ${e}`)
console.log(`${langs.join(', ')}: ${reference.size} keys, ${used.size} static keys used, ${warnings.length} warning(s), ${errors.length} error(s)`)
process.exit(errors.length ? 1 : 0)
