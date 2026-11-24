<script setup lang="ts">
import { watch } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import ConfirmPassword from '@app/components/ConfirmPassword.vue'
import { uiLocale } from '@app/i18n'
import { bindNotifier } from '@app/composables/useFm'
import { useApplyPrefs } from '@app/composables/usePrefs'
import DashboardLayout from './DashboardLayout.vue'
import SignIn from './SignIn.vue'
import { setupServiceWorker } from './pwa'
import { boot, session } from './session'

// Store notifications (operation progress), as in the POC.
const toast = useToast()
bindNotifier(
  (t) => (t.id && toast.toasts.value.some((x) => x.id === t.id) ? toast.update(t.id, t) : toast.add(t)),
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
      if (toast.toasts.value.some((t) => t.id === 'pwa-update')) return
      toast.add({
        id: 'pwa-update',
        title: 'Nouvelle version disponible',
        description: 'Rechargez pour passer à la nouvelle version du tableau de bord.',
        icon: 'i-lucide-rotate-cw',
        duration: 0,
        actions: [{ label: 'Recharger', icon: 'i-lucide-rotate-cw', onClick: apply }],
      })
    })
  },
)
</script>

<template>
  <UApp :locale="uiLocale" :toaster="{ position: 'bottom-right', max: 4 }">
    <DashboardLayout v-if="session.phase === 'ready'" />
    <SignIn v-else-if="session.phase === 'login'" />
    <div v-else class="grid min-h-dvh place-items-center bg-muted p-6">
      <div v-if="session.phase === 'error'" class="max-w-md space-y-4 text-center">
        <UIcon name="i-lucide-plug-zap" class="mx-auto size-10 text-error" />
        <h1 class="text-lg font-semibold text-highlighted">Le daemon FrankenManager ne répond pas</h1>
        <p class="text-sm text-muted">{{ session.error }}</p>
        <p class="text-sm text-muted">Lancez <code class="font-mono">frankenmanager dashboard</code>, qui démarre le daemon s’il ne tourne pas. La page se reconnecte d’elle-même dès qu’il répond.</p>
        <UButton icon="i-lucide-rotate-cw" label="Réessayer" @click="boot()" />
      </div>
      <UIcon v-else name="i-lucide-loader-circle" class="size-8 animate-spin text-primary" aria-label="Chargement" />
    </div>
    <ConfirmPassword />
  </UApp>
</template>
