<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { sidebarCollapsed, toggleSidebar } from '../composables/useSidebar'
import SidebarContent from './SidebarContent.vue'

const { t } = useI18n()

// Tablet: 72px icon bar by default; desktop: full 228px bar by default. The toggle switches between both, and the
// choice is remembered per breakpoint (useSidebar).
</script>

<template>
  <aside
    class="fixed inset-y-0 left-0 z-20 hidden flex-col border-r border-default bg-default px-3 pt-[calc(1.25rem+var(--sat))] pb-(--sab) md:flex xl:pt-[calc(26px+var(--sat))]"
    :class="sidebarCollapsed ? 'w-[72px]' : 'w-[228px]'"
  >
    <SidebarContent :collapsed="sidebarCollapsed">
      <template #header-trailing>
        <UTooltip :text="sidebarCollapsed ? t('sidebar.expand') : t('sidebar.collapse')" :content="{ side: 'right' }">
          <UButton
            color="neutral" variant="ghost" size="sm"
            :icon="sidebarCollapsed ? 'i-lucide-panel-left-open' : 'i-lucide-panel-left-close'"
            :class="!sidebarCollapsed && '-mr-1.5 ml-auto'"
            :aria-label="sidebarCollapsed ? t('sidebar.expand') : t('sidebar.collapse')"
            :aria-expanded="!sidebarCollapsed"
            @click="toggleSidebar"
          />
        </UTooltip>
      </template>
    </SidebarContent>
  </aside>
</template>
