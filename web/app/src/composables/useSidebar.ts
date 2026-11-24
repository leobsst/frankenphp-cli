import { computed, reactive, watch } from 'vue'
import { useMediaQuery } from '@vueuse/core'

/**
 * Side navigation state, shared by the sidebar, the header (burger) and the layouts (content offset).
 *
 * - phone (< 720px): no sidebar, the header burger opens the full navigation;
 * - tablet (720-1179px): icon-only bar by default, expandable;
 * - desktop (>= 1180px): full bar by default, collapsible.
 *
 * The choice is remembered per breakpoint, in this browser only.
 */
const STORAGE_KEY = 'fm.sidebar'

const tablet = useMediaQuery('(min-width: 720px) and (max-width: 1179px)')
const desktop = useMediaQuery('(min-width: 1180px)')

/** Expanded (full bar) per breakpoint. */
const expanded = reactive({ tablet: false, desktop: true })
try {
  const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}') as Partial<typeof expanded>
  if (typeof saved.tablet === 'boolean') expanded.tablet = saved.tablet
  if (typeof saved.desktop === 'boolean') expanded.desktop = saved.desktop
} catch {
  // Storage unavailable (private browsing, blocked site data) or unreadable: defaults.
}
watch(expanded, (v) => {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(v))
  } catch {
    // Not remembered: the choice still applies until the page is reloaded.
  }
})

/** The sidebar is displayed (tablet and desktop); on a phone, only the burger gives the navigation. */
export const sidebarShown = computed(() => tablet.value || desktop.value)

/** Icon-only sidebar on the current breakpoint. */
export const sidebarCollapsed = computed(() => (desktop.value ? !expanded.desktop : !expanded.tablet))

/**
 * Header burger (full navigation in a slideover): on the narrow screens whose sidebar is hidden (phone) or icon-only
 * (tablet). On a desktop the sidebar toggle is enough.
 */
export const navBurger = computed(() => !sidebarShown.value || (tablet.value && sidebarCollapsed.value))

/** Expands or collapses the sidebar on the current breakpoint. */
export function toggleSidebar() {
  if (desktop.value) expanded.desktop = !expanded.desktop
  else expanded.tablet = !expanded.tablet
}
