/**
 * Pinch zoom outside iOS. The viewport of index.html has maximum-scale=1 for iOS, which zooms in on a focused field
 * whose text is under 16px (see theme.ts); Safari and the iOS app still let the user pinch past it. Elsewhere
 * (Android, Chrome) the limit would forbid pinching and serves no purpose: it is removed.
 */
export function allowPinchZoom() {
  const ios = /iP(hone|ad|od)/.test(navigator.userAgent) || (navigator.userAgent.includes('Macintosh') && navigator.maxTouchPoints > 1)
  if (ios) return
  const meta = document.querySelector<HTMLMetaElement>('meta[name="viewport"]')
  if (meta) meta.content = meta.content.replace(/,\s*maximum-scale=[\d.]+/, '')
}
