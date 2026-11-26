// Hero image of each device and language (first store image): hero.html rendered at the size of the device, with the
// overview in light and the containers in dark taken by capture.sh. Usage: node hero.mjs <raw dir> <screenshots dir>
// Writes <screenshots dir>/<locale>/<lang>_<device>_00_hero.png. Needs Chromium: `pnpm exec playwright install chromium`.
import { chromium } from 'playwright'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { join, resolve } from 'node:path'

const [raw, out] = process.argv.slice(2).map((p) => resolve(p))
const here = fileURLToPath(new URL('.', import.meta.url))
const icon = resolve(here, '../../ios/App/App/Assets.xcassets/AppIcon.appiconset/AppIcon-512@2x.png')
const DEVICES = {
  iphone63: { w: 1206, h: 2622, kind: 'phone' },
  iphone69: { w: 1320, h: 2868, kind: 'phone' },
  iphoneduo: { w: 1398, h: 2034, kind: 'duo' },
  ipad13: { w: 2064, h: 2752, kind: 'tablet' },
}
const LOCALES = { en: 'en-US', fr: 'fr-FR' }

const browser = await chromium.launch()
for (const [key, d] of Object.entries(DEVICES)) {
  const page = await browser.newPage({ viewport: { width: d.w, height: d.h }, deviceScaleFactor: 1 })
  for (const [lang, locale] of Object.entries(LOCALES)) {
    const shot = (n, theme) => pathToFileURL(join(raw, `${lang}_${key}_${n}_${theme}.png`)).href
    const q = new URLSearchParams({
      w: d.w, h: d.h, lang, kind: d.kind, ratio: d.w / d.h, icon: pathToFileURL(icon).href,
      light: shot('01_overview', 'light'), dark: shot('03_containers', 'dark'),
    })
    await page.goto(`${pathToFileURL(join(here, 'hero.html')).href}?${q}`)
    await page.waitForLoadState('networkidle')
    await page.screenshot({ path: join(out, locale, `${lang}_${key}_00_hero.png`) })
    console.log(`${lang}_${key}_00_hero.png`)
  }
  await page.close()
}
await browser.close()
