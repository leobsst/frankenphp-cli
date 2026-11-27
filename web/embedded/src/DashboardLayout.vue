<script setup lang="ts">
import { onBeforeUnmount, onMounted, useTemplateRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { pageAvailable } from '@app/nav'
import { useI18n } from 'vue-i18n'
import AppSidebar from '@app/components/AppSidebar.vue'
import AppHeader from '@app/components/AppHeader.vue'
import BottomNav from '@app/components/BottomNav.vue'
import { sidebarCollapsed } from '@app/composables/useSidebar'
import { loadPage, pause, refresh, resume, state, toast } from '@app/composables/useFm'
import { shell } from '@app/lib/shell'
import { currentInstance, displayAddress, displayName, INSTANCES_PATH, noteVersion, signInPath } from '@app/lib/instances'
import { closeInstance, openInstance } from './session'

// Dashboard layout (web/app pages), on the instance chosen in the list: this machine by default.
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const inst = currentInstance()!

// What the shared sidebar shows: the instance (this machine: its name and address, read from the API), and the way
// to the list.
shell.name = inst.home ? '' : displayName(inst)
shell.address = inst.home ? '' : displayAddress(inst)
shell.switchTo = INSTANCES_PATH

// Hidden tab: the streams stop, and resume on return.
const onVisibility = () => (document.hidden ? pause() : resume())
onMounted(() => {
  openInstance(inst)
  document.addEventListener('visibilitychange', onVisibility)
})
onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibility)
  closeInstance()
  shell.name = ''
  shell.address = ''
  shell.switchTo = null
})

// The content scrolls in its own container, below the header: it is scrolled back to the top on page change.
const scroller = useTemplateRef<HTMLElement>('scroller')
watch(() => route.path, () => scroller.value?.scrollTo({ top: 0 }))

// Page-specific data (Redis, databases, S3, DNS devices, access…): read when opening it, once the instance is loaded.
watch([() => route.name, () => state.loaded], ([name, loaded]) => { if (loaded) void loadPage(String(name ?? '')) }, { immediate: true })

// Page hidden while its service is off (nav.ts): back to Containers, where it is turned on.
watch(() => state.loaded && !pageAvailable(String(route.name ?? '')), (off) => { if (off) void router.replace({ name: 'containers' }) }, { immediate: true })

// Daemon version read with the status (updated daemon): the instance list shows it right away.
watch(() => state.app.version, (version) => void noteVersion(inst.id, version))

// Another instance whose session was revoked or expired (401, marked by session.ts): back to its sign-in. This
// machine signs in again by itself (session.ts).
watch(() => inst.expired, (expired) => {
  if (!expired) return
  toast('error', t('mobile.session.expired'), t('mobile.session.expiredHelp'))
  void router.replace(signInPath(inst.id))
})
</script>

<template>
  <div class="fixed inset-0 flex flex-col overflow-hidden">
    <AppSidebar />
    <div class="flex min-h-0 flex-1 flex-col" :class="sidebarCollapsed ? 'md:ml-[72px]' : 'md:ml-[228px]'">
      <AppHeader />
      <div ref="scroller" class="min-h-0 flex-1 overflow-y-auto">
        <main class="mx-auto w-full max-w-7xl px-5 pt-6 pb-28 md:px-6 md:pt-7 md:pb-10 xl:px-8 xl:pt-[30px]">
          <UAlert
            v-if="state.error"
            color="error" variant="soft" icon="i-lucide-wifi-off" class="mb-5"
            :title="inst.home ? t('app.daemonDown.short') : t('mobile.layout.unreachable', { name: displayName(inst) })"
            :description="inst.home ? t('app.daemonDown.retrying', { error: state.error }) : state.error"
            :actions="[{ label: t('common.retry'), color: 'error', variant: 'outline', onClick: () => void refresh() }]"
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
