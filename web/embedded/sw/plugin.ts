import { createHash } from 'node:crypto'
import { readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join, posix, relative, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { Plugin } from 'vite'

/** Files of the build the service worker leaves out: the page itself (precached as ./), itself, dot files. */
const skipped = (file: string) => file === 'index.html' || file === 'sw.js' || posix.basename(file).startsWith('.') || file.endsWith('.map')

function walk(dir: string, root = dir): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const full = join(dir, e.name)
    return e.isDirectory() ? walk(full, root) : [relative(root, full).split(sep).join('/')]
  })
}

/**
 * Writes dist/sw.js once the build is written (public files included): sw/sw.js preceded by the list of the files
 * to precache and a version, the hash of their contents and of index.html. A new build of the dashboard thus gives a
 * new sw.js, which the browsers detect as an update.
 */
export function serviceWorker(): Plugin {
  let outDir = ''
  return {
    name: 'frankenmanager-service-worker',
    apply: 'build',
    configResolved(config) {
      outDir = resolve(config.root, config.build.outDir)
    },
    // Also called when the build fails: nothing is written then, and the error of the build stays the one reported.
    closeBundle(error?: Error) {
      if (error) return
      const files = walk(outDir).filter((f) => !skipped(f)).sort()
      const hash = createHash('sha256')
      for (const file of ['index.html', ...files]) hash.update(`${file}\0`).update(readFileSync(join(outDir, file)))
      const source = readFileSync(fileURLToPath(new URL('./sw.js', import.meta.url)), 'utf8')
      const header = `const VERSION = ${JSON.stringify(hash.digest('hex').slice(0, 16))}\nconst PRECACHE = ${JSON.stringify(files)}\n\n`
      writeFileSync(join(outDir, 'sw.js'), header + source)
    },
  }
}
