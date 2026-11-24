/**
 * Version and build number of the app, a single source for iOS, Android and the interface:
 *
 *   version  APP_VERSION, otherwise the last mobile-vX.Y.Z git tag, otherwise the version of package.json
 *   build    APP_BUILD, otherwise the git commit count (increasing, as the App Store and the Play Store require)
 *
 * Variables read from the environment or from .env.local. Run on its own (pnpm version:sync, and before each native
 * build), it writes the uncommitted files read by Xcode and Gradle:
 *   ios/version.local.xcconfig           MARKETING_VERSION, CURRENT_PROJECT_VERSION
 *   android/version.local.properties     versionName, versionCode
 */
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const envFile = join(root, '.env.local')
if (existsSync(envFile)) process.loadEnvFile(envFile)

const git = (...args) => {
  try {
    return execFileSync('git', args, { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim()
  } catch {
    return ''
  }
}

/** X.Y.Z version: the only form CFBundleShortVersionString and the Play Store accept. */
const VERSION = /^\d+(\.\d+){0,2}$/

export function appVersion() {
  const env = process.env.APP_VERSION?.trim()
  const tag = git('describe', '--tags', '--abbrev=0', '--match', 'mobile-v*').replace(/^mobile-v/, '')
  const pkg = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8')).version
  const version = env || tag || pkg
  if (!VERSION.test(version)) throw new Error(`Invalid version "${version}": X.Y.Z expected (APP_VERSION or mobile-vX.Y.Z tag).`)

  const build = process.env.APP_BUILD?.trim() || git('rev-list', '--count', 'HEAD') || '1'
  // Android versionCode: positive integer, 2,100,000,000 at most.
  if (!/^\d+$/.test(build) || +build < 1 || +build > 2_100_000_000) throw new Error(`Invalid build number "${build}" (APP_BUILD).`)

  return {
    version,
    build: +build,
    source: { version: env ? 'APP_VERSION' : tag ? `tag mobile-v${tag}` : 'package.json', build: process.env.APP_BUILD ? 'APP_BUILD' : 'git commit count' },
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    const v = appVersion()
    const header = 'Written by scripts/app-version.mjs, not committed.'
    writeFileSync(join(root, 'ios', 'version.local.xcconfig'), `// ${header}\nMARKETING_VERSION = ${v.version}\nCURRENT_PROJECT_VERSION = ${v.build}\n`)
    writeFileSync(join(root, 'android', 'version.local.properties'), `# ${header}\nversionName=${v.version}\nversionCode=${v.build}\n`)
    console.log(`app-version: ${v.version} (${v.build}), from ${v.source.version} and ${v.source.build}.`)
  } catch (e) {
    console.error(`app-version: ${e.message}`)
    process.exit(1)
  }
}
