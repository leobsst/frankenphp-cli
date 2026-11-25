<script setup lang="ts">
import { onBeforeUnmount, onMounted, useTemplateRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { PluginListenerHandle } from '@capacitor/core'
import { App as CapApp } from '@capacitor/app'
import AppSidebar from '@app/components/AppSidebar.vue'
import AppHeader from '@app/components/AppHeader.vue'
import BottomNav from '@app/components/BottomNav.vue'
import { sidebarCollapsed } from '@app/composables/useSidebar'
import { connect, disconnect, loadPage, pause, refresh, resume, state, toast } from '@app/composables/useFm'
import { shell } from '@app/lib/shell'
import { deviceLabel } from './lib/device'
import { currentInstance, displayAddress, displayName, isDemo } from './lib/instances'
import { mountNativeTabBar, nativeTabs } from './lib/nativeTabBar'
import { INSTANCES } from './router'

// Dashboard layout (web/app pages), on the instance chosen in the list.
const { t } = useI18n()
const router = useRouter()
const inst = currentInstance()!

// What the shared sidebar shows: this instance, and the way back to the list.
shell.name = displayName(inst)
shell.address = displayAddress(inst)
shell.switchTo = INSTANCES.path

let appState: PluginListenerHandle | undefined
let unmountTabBar: (() => void) | undefined
onMounted(async () => {
  // The demo runs on the phone itself: like the dashboard on its own machine, no password asked again.
  connect(inst, { mode: isDemo(inst) ? 'local' : 'remote', client: deviceLabel() })
  // On iPhone, the native tab bar (Liquid Glass since iOS 26) replaces BottomNav.
  void mountNativeTabBar(router).then((fn) => { unmountTabBar = fn })
  // Streams paused when the app goes to the background, resumed on return.
  appState = await CapApp.addListener('appStateChange', ({ isActive }) => (isActive ? resume() : pause()))
})
onBeforeUnmount(() => {
  disconnect()
  unmountTabBar?.()
  void appState?.remove()
  shell.name = ''
  shell.address = ''
  shell.switchTo = null
})

// Page-specific data (Redis, databases, S3, DNS devices, access…): read when opening it, once the instance is loaded.
const route = useRoute()
// The content scrolls in its own container: it is scrolled back to the top on page change.
const scroller = useTemplateRef<HTMLElement>('scroller')
watch(() => route.path, () => scroller.value?.scrollTo({ top: 0 }))
watch([() => route.name, () => state.loaded], ([name, loaded]) => { if (loaded) void loadPage(String(name ?? '')) }, { immediate: true })

// Session revoked or expired (401, marked by main.ts): back to the sign-in.
watch(() => inst.expired, (expired) => {
  if (!expired) return
  toast('error', t('mobile.session.expired'), t('mobile.session.expiredHelp'))
  void router.replace(`/instances/${inst.id}/connexion`)
})
</script>

<template>
  <!--
    Only the content scrolls, in its own container: the header stays in place, even when the content bounces at the
    top or bottom (iOS). On a phone, the bottom padding leaves the navigation bar and the home indicator area below
    the content.
  -->
  <div class="fixed inset-0 flex flex-col overflow-hidden">
    <AppSidebar />
    <div class="flex min-h-0 flex-1 flex-col" :class="sidebarCollapsed ? 'md:ml-[72px]' : 'md:ml-[228px]'">
      <AppHeader />
      <div ref="scroller" class="min-h-0 flex-1 overflow-y-auto">
        <main
          class="mx-auto w-full max-w-7xl px-5 pt-6 md:px-6 md:pt-7 md:pb-[calc(2.5rem+var(--sab))] xl:px-8 xl:pt-[30px]"
          :class="nativeTabs ? 'pb-[calc(var(--native-tabbar-height)+1.5rem)]' : 'pb-[calc(7.5rem+var(--sab))]'"
        >
          <UAlert
            v-if="state.error"
            color="error" variant="soft" icon="i-lucide-wifi-off" class="mb-5"
            :title="t('mobile.layout.unreachable', { name: displayName(inst) })" :description="state.error"
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
    <BottomNav v-if="!nativeTabs" />
  </div>
</template>
