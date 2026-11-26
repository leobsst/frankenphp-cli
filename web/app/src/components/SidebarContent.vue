<script setup lang="ts">
import { computed, ref, useTemplateRef } from 'vue'
import { useResizeObserver, useScroll } from '@vueuse/core'
import type { NavigationMenuItem } from '@nuxt/ui'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { SETTINGS, visibleNav } from '../nav'
import { state, updateAvailable, updates } from '../composables/useFm'
import { shell } from '../lib/shell'
import UpdateModal from './UpdateModal.vue'

// Side navigation content, shared by the sidebar (desktop / tablet)
// and the mobile side menu. `collapsed`: icon-only bar (tablet).
const props = defineProps<{ collapsed?: boolean }>()
const { t } = useI18n()

// Pages are child routes of "/" (dashboard layout): without `exact`, the overview link
// would stay active on every page.
const main = computed<NavigationMenuItem[]>(() => visibleNav.value.map((n) => ({ label: t(n.labelKey), icon: n.icon, to: n.path, exact: n.path === '/', tooltip: props.collapsed })))
const bottom = computed<NavigationMenuItem[]>(() => [
  { label: t(SETTINGS.labelKey), icon: SETTINGS.icon, to: SETTINGS.path, tooltip: props.collapsed },
  ...(shell.switchTo ? [{ label: t('sidebar.switchInstance'), icon: 'i-lucide-server-cog', to: shell.switchTo, tooltip: props.collapsed }] : []),
  { label: t('sidebar.docs'), icon: 'i-lucide-circle-help', to: 'https://github.com/leobsst/frankenphp-cli#readme', target: '_blank', tooltip: props.collapsed },
])

const router = useRouter()
/** Managed instance: its name in the app, or else the machine's name. */
const instName = computed(() => shell.name || state.connection.host)
const instAddress = computed(() => shell.address || state.connection.address)

const updateOpen = ref(false)
const updateTooltip = computed(() => {
  if (updateAvailable.value) return t('sidebar.updateAvailable', { version: state.app.latest })
  if (updates.info && !updates.info.error) return t('sidebar.upToDate', { version: state.app.version })
  return t('sidebar.checkUpdate')
})

// Fade at the top / bottom of the page list, only when there is content left to scroll on that side.
const scroller = useTemplateRef<HTMLElement>('scroller')
const { arrivedState, measure } = useScroll(scroller)
useResizeObserver(scroller, () => measure())
const FADE = '28px'
const fadeMask = computed(() => {
  const top = arrivedState.top ? '#000 0' : `transparent 0, #000 ${FADE}`
  const bottom = arrivedState.bottom ? '#000 100%' : `#000 calc(100% - ${FADE}), transparent 100%`
  const mask = `linear-gradient(to bottom, ${top}, ${bottom})`
  return { maskImage: mask, WebkitMaskImage: mask }
})

const navUi = {
  list: 'gap-1.5',
  link: 'h-[38px] px-3 gap-3 text-[13px]',
  linkLeadingIcon: 'size-[19px]',
}
</script>

<template>
  <!-- Logo, then the trailing control (sidebar toggle, menu close button): beside it, or below it on the icon bar. -->
  <div class="flex shrink-0 items-center" :class="collapsed ? 'flex-col gap-3' : 'h-8 px-2.5'">
    <RouterLink to="/" class="flex items-center gap-2.5 rounded-lg" :aria-label="t('sidebar.homeLabel')">
      <span class="grid size-8 shrink-0 place-items-center rounded-lg bg-primary text-inverted">
        <UIcon name="i-lucide-square-terminal" class="size-5" />
      </span>
      <span v-if="!collapsed" class="text-base font-semibold text-highlighted">frankenmanager</span>
    </RouterLink>
    <slot name="header-trailing" />
  </div>

  <!--
    Managed machine, only where several can be managed (the mobile app): it leads back to the instance list. The
    dashboard of the binary only manages its own machine.
  -->
  <div v-if="!collapsed && shell.switchTo" class="mt-7 shrink-0">
    <p class="mx-3 mb-2 text-[10px] font-semibold tracking-[1.4px] text-muted">{{ t('sidebar.hostMachine') }}</p>
    <UTooltip :text="t('sidebar.switchInstance')" :content="{ side: 'right' }">
      <button
        class="mx-1 flex h-14 w-[calc(100%-0.5rem)] items-center gap-2.5 rounded-lg border border-default bg-muted px-3 text-left text-muted hover:border-accented"
        @click="router.push(shell.switchTo)"
      >
        <UIcon name="i-lucide-server" class="size-5 shrink-0" />
        <span class="min-w-0 flex-1">
          <span class="block truncate text-[13px] font-medium text-highlighted">{{ instName }}</span>
          <span class="block truncate font-mono text-[11px]">{{ instAddress }}</span>
        </span>
      </button>
    </UTooltip>
  </div>

  <!-- Only the main pages scroll when height runs short; the header and footer stay visible. -->
  <div ref="scroller" class="min-h-0 flex-1 overflow-y-auto scrollbar-thin" :class="collapsed || !shell.switchTo ? 'mt-7' : 'mt-5'" :style="fadeMask">
    <UNavigationMenu :items="main" orientation="vertical" :collapsed="collapsed" :highlight="false" :ui="navUi" />
  </div>
  <UNavigationMenu :items="bottom" orientation="vertical" :collapsed="collapsed" :highlight="false" :ui="navUi" class="shrink-0 pt-4 pb-4" />

  <!-- Instance daemon version (opens the update modal), and event stream state. -->
  <UTooltip :text="updateTooltip" :content="{ side: 'right' }">
    <button
      class="mx-2 mb-1 flex items-center gap-2 rounded-lg px-1 py-2 text-left text-[11px] text-muted hover:text-highlighted"
      :class="collapsed && 'justify-center'"
      @click="updateOpen = true"
    >
      <UIcon :name="updateAvailable ? 'i-lucide-download' : 'i-lucide-badge-check'" class="size-4 shrink-0" :class="updateAvailable && 'text-warning'" />
      <span v-if="!collapsed" class="min-w-0 truncate">
        <span class="font-mono">v{{ state.app.version || '…' }}</span>
        <span v-if="updateAvailable" class="ml-1.5 font-medium text-warning">{{ t('sidebar.latestAvailable', { version: state.app.latest }) }}</span>
      </span>
    </button>
  </UTooltip>
  <UpdateModal v-model:open="updateOpen" />
  <div
    class="mx-2 flex items-center gap-2 border-t border-default py-6 text-xs font-medium"
    :class="[state.error ? 'text-error' : state.live ? 'text-primary' : 'text-muted', collapsed && 'justify-center']"
  >
    <span class="size-1.5 rounded-full bg-current" :class="!state.live && !state.error && 'animate-pulse'" />
    <span v-if="!collapsed">{{ state.error ? t('sidebar.unreachable') : state.live ? t('sidebar.live') : t('sidebar.connecting') }}</span>
  </div>
</template>
