/**
 * Dashboard translations, shared by the embedded dashboard and the mobile app: one vue-i18n instance (Composition
 * API), installed by both main.ts. Messages live in locales/<lang>.json with semantic keys (`overview.restart`); the
 * two catalogs must keep exactly the same keys (`pnpm --dir web i18n:check`).
 *
 * Language: the one chosen in Settings (per device, stored with the other preferences), or else the system's
 * (navigator.languages; the mobile app passes the phone's through setSystemLanguages()).
 *
 * The full vue-i18n build compiles messages at runtime into an AST it interprets (JIT): no eval nor `new Function`.
 */
import { computed, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { en as uiEn, fr as uiFr } from '@nuxt/ui/locale'
import fr from './locales/fr.json'
import en from './locales/en.json'

export type Language = 'fr' | 'en'
/** Preference stored in Settings: a language, or the system's. */
export type LanguagePref = Language | 'system'

export const LANGUAGES: Language[] = ['fr', 'en']

/** Default regional tag of each language, used for dates and numbers when the system has none for it. */
const DEFAULT_TAG: Record<Language, string> = { fr: 'fr-FR', en: 'en-US' }

const languageOf = (tag: string): Language | undefined => LANGUAGES.find((l) => tag.toLowerCase().split(/[-_]/)[0] === l)

/** System language tags, in order of preference. */
const systemTags = ref<string[]>(readNavigatorLanguages())

function readNavigatorLanguages(): string[] {
  if (typeof navigator === 'undefined') return []
  return [...(navigator.languages?.length ? navigator.languages : [navigator.language])].filter(Boolean)
}
if (typeof window !== 'undefined') window.addEventListener('languagechange', () => { systemTags.value = readNavigatorLanguages() })

/** Replaces the system languages (mobile app: the phone's, which the WebView may not report). */
export function setSystemLanguages(tags: string[]) {
  if (tags.length) systemTags.value = tags
}

/** First supported language among the system's, English otherwise. */
export const systemLanguage = computed<Language>(() => {
  for (const tag of systemTags.value) {
    const lang = languageOf(tag)
    if (lang) return lang
  }
  return 'en'
})

export const resolveLanguage = (pref: LanguagePref | undefined): Language => (pref && pref !== 'system' && LANGUAGES.includes(pref) ? pref : systemLanguage.value)

/**
 * French plural forms: 0 and 1 are singular. With three choices (`aucun | un | {n}`), the choice is 0, 1 or more,
 * as in vue-i18n's default rule.
 */
const frenchPlural = (choice: number, choices: number) => {
  const n = Math.abs(choice)
  if (choices === 3) return Math.min(n, 2)
  return n <= 1 ? 0 : 1
}

export const i18n = createI18n({
  legacy: false,
  locale: systemLanguage.value,
  fallbackLocale: 'en',
  messages: { fr, en },
  pluralRules: { fr: frenchPlural },
  missingWarn: import.meta.env.DEV,
  fallbackWarn: import.meta.env.DEV,
})

/** Translation outside components (store, router, native shell). */
export const t = i18n.global.t

/** Active language. */
export const language = computed<Language>(() => i18n.global.locale.value as Language)

/** Switches the interface language, and `<html lang>`. */
export function setLanguage(pref: LanguagePref | undefined) {
  const lang = resolveLanguage(pref)
  if (i18n.global.locale.value !== lang) i18n.global.locale.value = lang
  if (typeof document !== 'undefined') document.documentElement.lang = lang
}

/** Nuxt UI texts (pagination, command palette, close buttons…) in the active language, for `<UApp :locale>`. */
export const uiLocale = computed(() => (language.value === 'fr' ? uiFr : uiEn))

/** Regional tag for Intl: the system's when it is in the active language (en-GB, fr-CA…), else the default one. */
export const localeTag = computed(() => systemTags.value.find((tag) => languageOf(tag) === language.value) ?? DEFAULT_TAG[language.value])

// ---------- Formatting in the active language ----------

export const formatNumber = (n: number, options?: Intl.NumberFormatOptions) => n.toLocaleString(localeTag.value, options)

const toDate = (d: Date | string | number) => (d instanceof Date ? d : new Date(d))
export const formatDate = (d: Date | string | number, options?: Intl.DateTimeFormatOptions) => toDate(d).toLocaleDateString(localeTag.value, options)
export const formatTime = (d: Date | string | number, options?: Intl.DateTimeFormatOptions) => toDate(d).toLocaleTimeString(localeTag.value, options)
export const formatDateTime = (d: Date | string | number, options?: Intl.DateTimeFormatOptions) => toDate(d).toLocaleString(localeTag.value, options)

/** Size in bytes: 512 o / 12 Ko / 3,4 Mo (B, KB, MB in English). */
export const formatBytes = (n: number) => (n >= 1_000_000
  ? t('units.mb', { n: formatNumber(n / 1_000_000, { maximumFractionDigits: 1 }) })
  : n >= 1000 ? t('units.kb', { n: formatNumber(Math.round(n / 1000)) }) : t('units.bytes', { n: formatNumber(n) }))

/** Size in MB: 850 Mo, or 1,2 Go from 1000 MB on. */
export const formatMegabytes = (mb: number) => (mb >= 1000
  ? t('units.gb', { n: formatNumber(mb / 1000, { maximumFractionDigits: 1 }) })
  : t('units.mb', { n: formatNumber(mb) }))

/** Percentage: « 12,5 % » in French, "12.5%" in English. */
export const formatPercent = (n: number, options?: Intl.NumberFormatOptions) => t('units.percent', { n: formatNumber(n, options) })
