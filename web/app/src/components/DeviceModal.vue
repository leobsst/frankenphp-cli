<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { actions, dnsClientsConnected, loadPage, state } from '../composables/useFm'
import { formatNumber } from '../i18n'
import QrCode from './QrCode.vue'

const open = defineModel<boolean>('open', { default: false })
const { t } = useI18n()

// The CA is downloaded from the daemon's sharing server (state.ssl.caUrl, on the host's LAN address): the phone
// opens that URL and receives the certificate file. Off: no QR code, the sharing is offered instead.
const toggling = ref(false)
async function share(on: boolean) {
  toggling.value = true
  try {
    await actions.setCaShared(on, state.ssl.caPort)
  } finally {
    toggling.value = false
  }
}

// The LAN DNS devices and the CA sharing state are reloaded on open.
watch(open, (o) => { if (o) void loadPage('dns') })
</script>

<template>
  <UModal v-model:open="open" :title="t('deviceModal.title')" :description="t('deviceModal.description')" :ui="{ footer: 'justify-end' }">
    <template #body>
      <ol class="flex flex-col gap-4 text-sm">
        <li>
          <p class="font-medium text-highlighted">1. DNS</p>
          <i18n-t keypath="deviceModal.dnsHelp" tag="p" scope="global" class="text-xs text-muted">
            <template #ip><code class="font-mono text-default">{{ state.dns.ip }}</code></template>
          </i18n-t>
          <p v-if="!state.dns.enabled" class="mt-2 flex items-center gap-2 text-xs text-warning">
            <UIcon name="i-lucide-triangle-alert" class="size-4" />{{ t('deviceModal.dnsDisabled') }}
            <UButton size="xs" variant="soft" :label="t('deviceModal.enable')" @click="actions.setService('dns', true)" />
          </p>
          <div v-else class="mt-3 rounded-lg border border-default bg-muted p-3 text-xs">
            <p class="flex items-center gap-2 font-medium text-highlighted">
              <span class="size-1.5 rounded-full" :class="state.dns.status === 'running' ? 'bg-primary animate-pulse' : 'bg-error'" />
              {{ t('deviceModal.dnsClients', { n: formatNumber(dnsClientsConnected.length) }, dnsClientsConnected.length) }}
            </p>
            <p class="mt-1 truncate text-muted">{{ dnsClientsConnected.slice(0, 4).map((c) => c.name ?? c.ip).join(' · ') || t('deviceModal.waiting') }}</p>
          </div>
        </li>
        <li>
          <p class="font-medium text-highlighted">{{ t('deviceModal.caStep') }}</p>
          <template v-if="state.ssl.caShared">
            <template v-if="state.ssl.caUrl">
              <i18n-t keypath="deviceModal.caOpen" tag="p" scope="global" class="text-xs text-muted">
                <template #url><code class="font-mono break-all text-default">{{ state.ssl.caUrl }}</code></template>
              </i18n-t>
              <div class="mt-3 flex items-center gap-4 rounded-lg border border-default bg-muted p-3">
                <QrCode :value="state.ssl.caUrl" :label="t('deviceModal.caQrLabel')" class="size-28 shrink-0" />
                <p class="text-xs text-muted">{{ t('deviceModal.caScan') }}</p>
              </div>
            </template>
            <p v-else class="mt-2 flex items-center gap-2 text-xs text-warning">
              <UIcon name="i-lucide-triangle-alert" class="size-4 shrink-0" />{{ t('deviceModal.caNoAddress', { port: state.ssl.caPort }) }}
            </p>
            <UButton class="mt-3" size="xs" color="neutral" variant="outline" icon="i-lucide-circle-stop" :label="t('deviceModal.stopSharing')" :loading="toggling" @click="share(false)" />
          </template>
          <template v-else>
            <p class="text-xs text-muted">{{ t('deviceModal.caSharingHelp') }}</p>
            <p class="mt-2 flex flex-wrap items-center gap-2 text-xs text-warning">
              <UIcon name="i-lucide-triangle-alert" class="size-4 shrink-0" />{{ t('deviceModal.caSharingDisabled') }}
              <UButton size="xs" variant="soft" :label="t('deviceModal.startSharing')" :loading="toggling" @click="share(true)" />
            </p>
          </template>
        </li>
        <li>
          <p class="font-medium text-highlighted">{{ t('deviceModal.trustStep') }}</p>
          <p class="text-xs text-muted">{{ t('deviceModal.trustHelp') }}</p>
        </li>
      </ol>
    </template>
    <template #footer="{ close }">
      <UButton :label="t('deviceModal.done')" @click="close" />
    </template>
  </UModal>
</template>
