<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { CommandPaletteGroup, CommandPaletteItem } from '@nuxt/ui'
import { defineShortcuts } from '@nuxt/ui/composables'
import { NAV, SETTINGS } from '../nav'
import { domainIcon, envLabel, state } from '../composables/useFm'
import { navBurger } from '../composables/useSidebar'
import SidebarContent from './SidebarContent.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const searchOpen = ref(false)
// Phone (no sidebar) and tablet with the icon bar: the burger opens the full navigation, closed on navigation or
// when the burger is no longer offered (sidebar expanded, wider screen).
const menuOpen = ref(false)
watch(() => route.fullPath, () => { menuOpen.value = false })
watch(navBurger, (shown) => { if (!shown) menuOpen.value = false })

defineShortcuts({ meta_k: () => { searchOpen.value = true } })

const local = computed(() => state.connection.mode === 'local')
const connectionHint = computed(() => (local.value
  ? t('header.connectionLocal', { host: state.connection.host, address: state.connection.address })
  : t('header.connectionRemote', { host: state.connection.host, address: state.connection.address, client: state.connection.client })))

const groups = computed<CommandPaletteGroup<CommandPaletteItem>[]>(() => {
  const go = (path: string) => () => { router.push(path); searchOpen.value = false }
  return [
    { id: 'pages', label: t('header.pages'), items: [...NAV, SETTINGS].map((p) => ({ label: t(p.labelKey), icon: p.icon, onSelect: go(p.path) })) },
    {
      id: 'domains', label: t('header.domains'), items: state.domains.map((d) => ({
        label: d.name, suffix: d.archived ? t('header.archived') : envLabel(d),
        icon: domainIcon(d), onSelect: go('/projets'),
      })),
    },
    { id: 'containers', label: t('nav.containers'), items: state.containers.map((c) => ({ label: c.id, suffix: c.label, icon: 'i-lucide-box', onSelect: go('/conteneurs') })) },
  ]
})
</script>

<template>
  <header class="z-10 shrink-0 box-content flex h-(--ui-header-height) pt-safe items-center gap-5 border-b border-default bg-default px-gutter [--gutter:1.25rem] md:[--gutter:1.5rem] xl:[--gutter:2rem]">
    <!-- Burger (phone, tablet icon bar) + logo (phone only: the sidebar shows it elsewhere) -->
    <div v-if="navBurger" class="flex items-center gap-2.5">
      <UButton color="neutral" variant="ghost" icon="i-lucide-menu" class="-ml-1.5" :aria-label="t('header.openMenu')" :aria-expanded="menuOpen" @click="menuOpen = true" />
      <span class="grid size-8 place-items-center rounded-lg bg-primary text-inverted md:hidden"><UIcon name="i-lucide-square-terminal" class="size-5" /></span>
      <span class="text-[15px] font-semibold text-highlighted md:hidden">frankenmanager</span>
    </div>

    <UBreadcrumb
      color="neutral"
      class="max-md:hidden"
      :items="[{ label: local ? t('header.localHost') : state.connection.host }, { label: route.meta.labelKey ? t(String(route.meta.labelKey)) : '' }]"
      :ui="{ link: 'text-[13px]', separatorIcon: 'size-4' }"
    />

    <div class="ml-auto flex items-center gap-2 md:gap-5">
      <UTooltip :text="connectionHint">
        <UBadge
          :label="local ? t('common.local') : t('common.remote')" :icon="local ? 'i-lucide-monitor' : 'i-lucide-smartphone'"
          :color="local ? 'primary' : 'warning'" variant="soft" class="max-md:hidden"
        />
      </UTooltip>
      <UButton
        color="neutral" variant="outline" icon="i-lucide-search"
        class="w-[220px] flex-nowrap overflow-hidden whitespace-nowrap bg-muted text-xs font-normal text-muted max-md:hidden xl:w-[300px]"
        @click="searchOpen = true"
      >
        <span class="min-w-0 truncate">{{ t('header.searchPlaceholder') }}</span>
        <template #trailing><span class="ml-auto flex shrink-0 gap-0.5"><UKbd value="meta" /><UKbd value="K" /></span></template>
      </UButton>
      <UButton color="neutral" variant="ghost" icon="i-lucide-search" class="md:hidden" :aria-label="t('common.search')" @click="searchOpen = true" />
    </div>

    <USlideover v-model:open="menuOpen" side="left" :title="t('header.navigation')" :description="t('header.navigationDescription')" :ui="{ content: 'w-[280px] max-w-[85vw]' }">
      <!-- The slideover itself keeps out of the safe areas (shared theme). -->
      <template #content>
        <nav class="flex h-full flex-col overflow-y-auto px-3 pt-5">
          <SidebarContent>
            <template #header-trailing>
              <UButton color="neutral" variant="ghost" icon="i-lucide-x" class="ml-auto" :aria-label="t('header.closeMenu')" @click="menuOpen = false" />
            </template>
          </SidebarContent>
        </nav>
      </template>
    </USlideover>

    <UModal v-model:open="searchOpen" :ui="{ content: 'sm:max-w-xl' }" :title="t('header.searchTitle')" :description="t('header.searchDescription')">
      <template #content>
        <UCommandPalette :groups="groups" :placeholder="t('header.searchPlaceholder')" class="h-[420px]" close @update:open="searchOpen = $event" />
      </template>
    </UModal>
  </header>
</template>
