<script setup lang="ts">
import { onBeforeUnmount, useTemplateRef, watch } from 'vue'
import { useRoute } from 'vue-router'
import AppSidebar from '@app/components/AppSidebar.vue'
import AppHeader from '@app/components/AppHeader.vue'
import BottomNav from '@app/components/BottomNav.vue'
import { sidebarCollapsed } from '@app/composables/useSidebar'
import { disconnect, loadPage, pause, refresh, resume, state } from '@app/composables/useFm'

// Dashboard layout (POC), on the instance connected by session.ts.
const route = useRoute()

// The content scrolls in its own container, below the header: it is scrolled back to the top on page change.
const scroller = useTemplateRef<HTMLElement>('scroller')
watch(() => route.path, () => scroller.value?.scrollTo({ top: 0 }))

// Page-specific data (Redis, databases, S3, DNS devices, access…): read when opening it, once the instance is loaded.
watch([() => route.name, () => state.loaded], ([name, loaded]) => { if (loaded) void loadPage(String(name ?? '')) }, { immediate: true })

// Hidden tab: the streams stop, and resume on return.
const onVisibility = () => (document.hidden ? pause() : resume())
document.addEventListener('visibilitychange', onVisibility)
onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibility)
  disconnect()
})
</script>

<template>
  <div class="flex h-dvh flex-col overflow-hidden">
    <AppSidebar />
    <div class="flex min-h-0 flex-1 flex-col" :class="sidebarCollapsed ? 'md:ml-[72px]' : 'md:ml-[228px]'">
      <AppHeader />
      <div ref="scroller" class="min-h-0 flex-1 overflow-y-auto">
        <main class="mx-auto w-full max-w-7xl px-5 pt-6 pb-28 md:px-6 md:pt-7 md:pb-10 xl:px-8 xl:pt-[30px]">
          <UAlert
            v-if="state.error"
            color="error" variant="soft" icon="i-lucide-wifi-off" class="mb-5"
            title="Le daemon ne répond pas" :description="`${state.error} Nouvelle tentative automatique en cours…`"
            :actions="[{ label: 'Réessayer', color: 'error', variant: 'outline', onClick: () => void refresh() }]"
          />
          <div v-if="!state.loaded" class="flex flex-col gap-4">
            <USkeleton class="h-16 w-2/3" />
            <div class="grid grid-cols-2 gap-2.5 md:gap-4 xl:grid-cols-4"><USkeleton v-for="i in 4" :key="i" class="h-28" /></div>
            <USkeleton class="h-72" />
          </div>
          <RouterView v-else-if="state.containers.length || !state.error" />
        </main>
      </div>
    </div>
    <BottomNav />
  </div>
</template>
