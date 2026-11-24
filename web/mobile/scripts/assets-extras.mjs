/**
 * Completes what `capacitor-assets generate` produces from assets/, and erases on each generation:
 *
 *   Android  foreground of the adaptive icons, logo enlarged to the size of the standard icon (within the safe
 *            circle), and its monochrome layer (themed icons of Android 13 and later)
 *   iOS      dark and tinted variants of the icon (iOS 18 and later), logo at the size of the standard icon
 *   Web      real WebP images (the generator writes PNG under the .webp extension), manifest fields, and images of
 *            the HTML splash screen (public/splash/)
 *
 * Run by `pnpm assets`, after the generator. The sources stay assets/icon-foreground.png (logo on a transparent
 * background) and assets/icon-only.png.
 */
import { existsSync, mkdirSync, readdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import sharp from 'sharp'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const foreground = join(root, 'assets', 'icon-foreground.png')
const log = (msg) => console.log(`assets-extras: ${msg}`)

// ---------- Logo size ----------
// icon-foreground.png carries the logo smaller than icon-only.png (the iOS icon): it is brought back to the same size,
// measured on the sources, so that the icon keeps the same presence everywhere. Android has a limit: its round icons
// cut whatever goes beyond the safe circle (66 dp out of 108, i.e. 92% of the visible area), the logo stops there.

/** Bounding box of the pixels where `a` and `b` (same size) differ, or of the opaque pixels of `a` without `b`. */
async function bbox(a, b) {
  const ra = await sharp(a).ensureAlpha().raw().toBuffer({ resolveWithObject: true })
  const rb = b ? await sharp(b).resize(ra.info.width, ra.info.height).ensureAlpha().raw().toBuffer() : null
  const { width, height } = ra.info
  let [x0, y0, x1, y1] = [width, height, -1, -1]
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const i = (y * width + x) * 4
      const hit = rb
        ? Math.abs(ra.data[i] - rb[i]) + Math.abs(ra.data[i + 1] - rb[i + 1]) + Math.abs(ra.data[i + 2] - rb[i + 2]) > 60
        : ra.data[i + 3] > 127
      if (hit) { x0 = Math.min(x0, x); y0 = Math.min(y0, y); x1 = Math.max(x1, x); y1 = Math.max(y1, y) }
    }
  }
  return { w: x1 - x0 + 1, h: y1 - y0 + 1, size: width }
}
const logoFg = await bbox(foreground)
const logoIcon = await bbox(join(root, 'assets', 'icon-only.png'), join(root, 'assets', 'icon-background.png'))
const iosScale = Math.max(1, (logoIcon.w / logoIcon.size) / (logoFg.w / logoFg.size))
// Half-diagonal of the enlarged logo ≤ 46% of the visible area (the safe circle), with a small margin.
const androidScale = Math.max(1, Math.min(iosScale, (0.46 * 0.96 * logoFg.size * 2) / Math.hypot(logoFg.w, logoFg.h)))

/** The logo of icon-foreground.png enlarged by `scale` around the centre, on a square of `size` pixels. */
async function scaledForeground(scale, size = 1024) {
  const big = Math.round(size * scale)
  const offset = Math.floor((big - size) / 2)
  return sharp(await sharp(foreground).resize(big, big).png().toBuffer()).extract({ left: offset, top: offset, width: size, height: size }).png().toBuffer()
}

/** White silhouette (the alpha channel only) of an image. */
async function silhouette(image) {
  const { width, height } = await sharp(image).metadata()
  const alpha = await sharp(image).ensureAlpha().extractChannel('alpha').toBuffer()
  return sharp({ create: { width, height, channels: 3, background: '#ffffff' } }).joinChannel(alpha).png().toBuffer()
}

// ---------- Android: enlarged foreground and monochrome layer ----------
const res = join(root, 'android', 'app', 'src', 'main', 'res')
const densities = readdirSync(res).filter((d) => d.startsWith('mipmap-') && existsSync(join(res, d, 'ic_launcher_foreground.png')))
const androidFg = await scaledForeground(androidScale)
for (const d of densities) {
  const { width } = await sharp(join(res, d, 'ic_launcher_foreground.png')).metadata()
  const fg = await sharp(androidFg).resize(width, width).png().toBuffer()
  // The foreground is also the icon of the launch screen of Android 12 and later (styles.xml).
  writeFileSync(join(res, d, 'ic_launcher_foreground.png'), fg)
  writeFileSync(join(res, d, 'ic_launcher_monochrome.png'), await silhouette(fg))
}
const MONOCHROME = '    <monochrome>\n        <inset android:drawable="@mipmap/ic_launcher_monochrome" android:inset="16.7%" />\n    </monochrome>\n'
for (const name of ['ic_launcher.xml', 'ic_launcher_round.xml']) {
  const file = join(res, 'mipmap-anydpi-v26', name)
  const xml = readFileSync(file, 'utf8')
  if (!xml.includes('<monochrome>')) writeFileSync(file, xml.replace('</adaptive-icon>', `${MONOCHROME}</adaptive-icon>`))
}
log(`Android adaptive icon, logo ×${androidScale.toFixed(2)}, and monochrome layer (${densities.length} densities)`)

// ---------- iOS: dark and tinted variants ----------
const appIcon = join(root, 'ios', 'App', 'App', 'Assets.xcassets', 'AppIcon.appiconset')
// The logo at the size of the standard icon. Dark: on a transparent background, iOS adds its dark background.
// Tinted: greyscale on a black background, iOS applies the tint chosen by the user.
const iosFg = await scaledForeground(iosScale)
await sharp(iosFg).png().toFile(join(appIcon, 'AppIcon-dark.png'))
await sharp(iosFg).grayscale().flatten({ background: '#000000' }).png().toFile(join(appIcon, 'AppIcon-tinted.png'))
const contents = JSON.parse(readFileSync(join(appIcon, 'Contents.json'), 'utf8'))
const base = { idiom: 'universal', platform: 'ios', size: '1024x1024' }
contents.images = [
  ...contents.images.filter((i) => !i.appearances),
  { ...base, filename: 'AppIcon-dark.png', appearances: [{ appearance: 'luminosity', value: 'dark' }] },
  { ...base, filename: 'AppIcon-tinted.png', appearances: [{ appearance: 'luminosity', value: 'tinted' }] },
]
writeFileSync(join(appIcon, 'Contents.json'), `${JSON.stringify(contents, null, 2)}\n`)
log(`iOS dark and tinted icons, logo ×${iosScale.toFixed(2)}`)

// ---------- Web: HTML splash screen (index.html) ----------
// Same images as the native splash screen: the page takes over without a visible transition, with a spinner.
const splashDir = join(root, 'public', 'splash')
mkdirSync(splashDir, { recursive: true })
for (const [src, out] of [['splash.png', 'splash.webp'], ['splash-dark.png', 'splash-dark.webp']]) {
  await sharp(join(root, 'assets', src)).resize(2048, 2048).webp({ quality: 82 }).toFile(join(splashDir, out))
}
log('web splash screen')

// ---------- Web: WebP and manifest ----------
const pub = join(root, 'public')
const manifestPath = join(pub, 'manifest.webmanifest')
const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'))
for (const icon of manifest.icons ?? []) {
  const file = join(pub, icon.src)
  if (!existsSync(file)) continue
  const { format } = await sharp(file).metadata()
  if (format !== 'webp') {
    await sharp(file).webp({ quality: 90 }).toFile(`${file}.tmp`)
    renameSync(`${file}.tmp`, file)
  }
  icon.type = 'image/webp'
}
const ordered = {
  name: 'FrankenManager',
  short_name: 'FrankenManager',
  description: 'Pilotez vos instances FrankenManager depuis votre téléphone.',
  lang: 'fr',
  start_url: './',
  scope: './',
  display: 'standalone',
  theme_color: '#4F1895',
  background_color: '#4F1895',
  ...manifest,
}
writeFileSync(manifestPath, `${JSON.stringify(ordered, null, 2)}\n`)
log(`web manifest and ${manifest.icons?.length ?? 0} WebP icons`)
