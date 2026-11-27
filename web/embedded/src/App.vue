<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useToast } from '@nuxt/ui/composables'
import ConfirmPassword from '@app/components/ConfirmPassword.vue'
import { uiLocale } from '@app/i18n'
import { bindNotifier } from '@app/composables/useFm'
import { useApplyPrefs } from '@app/composables/usePrefs'
import { currentInstance, INSTANCES_PATH } from '@app/lib/instances'
import SignIn from './SignIn.vue'
import { setupServiceWorker } from './pwa'
import { boot, session } from './session'

const { t } = useI18n()

// The dashboard of this machine waits for its session (sign-in, or its daemon answering); the instance pages and the
// other instances do not.
const route = useRoute()
const waitsForHome = computed(() => !!route.meta.dashboard && !!currentInstance()?.home && session.phase !== 'ready')

// Store notifications (operation progress), as in the POC.
const toast = useToast()
bindNotifier(
  (n) => (n.id && toast.toasts.value.some((x) => x.id === n.id) ? toast.update(n.id, n) : toast.add(n)),
  (id) => toast.remove(id),
)
useApplyPrefs()

// Service worker (installable dashboard), once /config.json told the mode: remote access or this machine.
let pwa = false
watch(
  () => session.phase,
  (phase) => {
    if (pwa || phase === 'loading' || phase === 'error') return
    pwa = true
    setupServiceWorker(session.mode, (apply) => {
      if (toast.toasts.value.some((x) => x.id === 'pwa-update')) return
      toast.add({
        id: 'pwa-update',
        title: t('app.update.title'),
        description: t('app.update.description'),
        icon: 'i-lucide-rotate-cw',
        duration: 0,
        actions: [{ label: t('app.update.reload'), icon: 'i-lucide-rotate-cw', onClick: apply }],
      })
    })
  },
)
</script>

<template>
  <UApp :locale="uiLocale" :toaster="{ position: 'bottom-right', max: 4 }">
    <RouterView v-if="!waitsForHome" />
    <SignIn v-else-if="session.phase === 'login'" />
    <div v-else class="grid min-h-dvh place-items-center bg-muted p-6">
      <div v-if="session.phase === 'error'" class="max-w-md space-y-4 text-center">
        <UIcon name="i-lucide-plug-zap" class="mx-auto size-10 text-error" />
        <h1 class="text-lg font-semibold text-highlighted">{{ t('app.daemonDown.title') }}</h1>
        <p class="text-sm text-muted">{{ session.error }}</p>
        <i18n-t keypath="app.daemonDown.help" tag="p" scope="global" class="text-sm text-muted">
          <template #cmd><code class="font-mono">frankenmanager dashboard</code></template>
        </i18n-t>
        <div class="flex flex-wrap justify-center gap-2">
          <UButton icon="i-lucide-rotate-cw" :label="t('common.retry')" @click="boot()" />
          <!-- The other instances stay reachable while this machine's daemon is down. -->
          <UButton icon="i-lucide-server-cog" :label="t('sidebar.switchInstance')" color="neutral" variant="outline" :to="INSTANCES_PATH" />
        </div>
      </div>
      <UIcon v-else name="i-lucide-loader-circle" class="size-8 animate-spin text-primary" :aria-label="t('common.loading')" />
    </div>
    <ConfirmPassword />
  </UApp>
</template>
