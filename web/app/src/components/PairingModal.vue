<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { closePairing, createPairing, pairing, toast } from '../composables/useFm'
import QrCode from './QrCode.vue'

// One-time pairing of the mobile app (POST /api/v1/auth/pairing): the app scans the QR code and gets a session
// without the password. The daemon reports the device that used the code on the event stream ("paired").
const open = defineModel<boolean>('open', { default: false })
const { t } = useI18n()

const now = ref(Date.now())
let tick: ReturnType<typeof setInterval> | undefined
watch(open, (o) => {
  clearInterval(tick)
  if (o) {
    now.value = Date.now()
    tick = setInterval(() => { now.value = Date.now() }, 1_000)
    void createPairing()
  } else {
    closePairing()
  }
}, { immediate: true })
onBeforeUnmount(() => { clearInterval(tick); closePairing() })

const code = computed(() => pairing.code)
const left = computed(() => (code.value ? Math.max(0, Math.round((Date.parse(code.value.expires_at) - now.value) / 1000)) : 0))
const expired = computed(() => !!code.value && left.value === 0)
const countdown = computed(() => `${Math.floor(left.value / 60)}:${String(left.value % 60).padStart(2, '0')}`)
/** Short form of the CA fingerprint, to compare with what the app shows: the first 8 bytes. */
const caShort = computed(() => (code.value?.ca_fingerprint ?? '').slice(0, 16).toUpperCase().match(/../g)?.join(':') ?? '')

function copy() {
  if (!code.value) return
  navigator.clipboard?.writeText(code.value.code)
  toast('info', t('pairingModal.copied'), t('pairingModal.singleUse'))
}
</script>

<template>
  <UModal v-model:open="open" :title="t('pairingModal.title')" :description="t('pairingModal.description')" :ui="{ content: 'sm:max-w-[440px]', footer: 'justify-end gap-2' }">
    <template #body>
      <div v-if="pairing.paired" class="flex flex-col items-center gap-3 py-6 text-center">
        <UIcon name="i-lucide-circle-check" class="size-10 text-primary" />
        <p class="text-sm font-medium text-highlighted">{{ t('pairingModal.paired', { device: pairing.paired }) }}</p>
        <p class="text-xs text-muted">{{ t('pairingModal.pairedHelp') }}</p>
      </div>
      <UAlert v-else-if="pairing.error" color="error" variant="soft" :title="t('pairingModal.failed')" :description="pairing.error" :ui="{ description: 'text-xs' }" />
      <div v-else-if="!code" class="flex justify-center py-16">
        <UIcon name="i-lucide-loader-circle" class="size-6 animate-spin text-muted" />
      </div>
      <div v-else class="flex flex-col items-center gap-4">
        <div class="relative">
          <QrCode :value="code.qr" :label="t('pairingModal.qrLabel')" class="size-56" :class="expired ? 'opacity-20' : ''" />
          <div v-if="expired" class="absolute inset-0 flex items-center justify-center">
            <UBadge :label="t('pairingModal.expiredBadge')" color="warning" variant="solid" />
          </div>
        </div>
        <p class="text-xs" :class="expired ? 'text-warning' : 'text-muted'">
          {{ expired ? t('pairingModal.expired') : t('pairingModal.validFor', { time: countdown }) }}
        </p>
        <div class="w-full rounded-lg border border-default bg-muted p-3 text-xs">
          <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5">
            <dt class="text-muted">{{ t('pairingModal.address') }}</dt>
            <dd class="truncate font-mono text-default">{{ code.url }}</dd>
            <dt class="text-muted">{{ t('pairingModal.code') }}</dt>
            <dd class="flex min-w-0 items-center gap-1">
              <code class="truncate font-mono text-default select-all">{{ code.code }}</code>
              <UButton size="xs" color="neutral" variant="ghost" icon="i-lucide-copy" :aria-label="t('pairingModal.copyCode')" @click="copy" />
            </dd>
            <template v-if="caShort">
              <dt class="text-muted">CA</dt>
              <dd class="truncate font-mono text-default" :title="code.ca_fingerprint">{{ caShort }}…</dd>
            </template>
          </dl>
          <p class="mt-2 text-muted">{{ t('pairingModal.noCamera') }} {{ caShort ? t('pairingModal.caCheck') : '' }}</p>
        </div>
      </div>
    </template>
    <template #footer="{ close }">
      <UButton color="neutral" variant="ghost" :label="t('common.close')" @click="close" />
      <UButton icon="i-lucide-refresh-cw" :label="pairing.paired ? t('pairingModal.pairAnother') : t('pairingModal.newCode')" :loading="pairing.creating" @click="createPairing" />
    </template>
  </UModal>
</template>
