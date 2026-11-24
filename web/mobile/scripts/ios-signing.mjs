/**
 * Writes ios/signing.local.xcconfig (not committed) from the environment variables, or .env.local:
 *
 *   IOS_TEAM_ID    signing team (Xcode › Settings › Accounts, or developer.apple.com › Membership)
 *   IOS_BUNDLE_ID  app identifier, unique to your team (dev.frankenmanager.mobile by default)
 *
 * Without IOS_TEAM_ID, the file is left alone: a signing.local.xcconfig already written stays in place.
 */
import { existsSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const envFile = join(root, '.env.local')
if (existsSync(envFile)) process.loadEnvFile(envFile)

const team = process.env.IOS_TEAM_ID?.trim()
const bundle = process.env.IOS_BUNDLE_ID?.trim()
const out = join(root, 'ios', 'signing.local.xcconfig')

if (!team) {
  console.log(existsSync(out)
    ? 'ios-signing: no IOS_TEAM_ID, existing signing.local.xcconfig kept.'
    : 'ios-signing: no IOS_TEAM_ID, the app will not be signed for a device (see .env.example).')
  process.exit(0)
}
if (!/^[A-Z0-9]{10}$/.test(team)) {
  console.error(`ios-signing: invalid IOS_TEAM_ID ("${team}"): 10 uppercase letters or digits expected.`)
  process.exit(1)
}
if (bundle && !/^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$/.test(bundle)) {
  console.error(`ios-signing: invalid IOS_BUNDLE_ID ("${bundle}").`)
  process.exit(1)
}

const lines = ['// Written by scripts/ios-signing.mjs, not committed.', `DEVELOPMENT_TEAM = ${team}`]
if (bundle) lines.push(`PRODUCT_BUNDLE_IDENTIFIER = ${bundle}`)
writeFileSync(out, lines.join('\n') + '\n')
console.log(`ios-signing: team ${team}${bundle ? `, ${bundle}` : ''}.`)
