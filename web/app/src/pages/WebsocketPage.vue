<script setup lang="ts">
import { computed, ref } from 'vue'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import TwoColumns from '../components/TwoColumns.vue'
import InfoBanner from '../components/InfoBanner.vue'
import { useI18n } from 'vue-i18n'
import { actions, isServiceBusy, serviceEnabled, state, toast } from '../composables/useFm'

const { t } = useI18n()

// Soketi (Pusher protocol): `frankenmanager service enable|disable soketi`.
const reveal = ref(false)
const enabled = computed(() => serviceEnabled('soketi'))
const busy = computed(() => isServiceBusy('soketi'))
const container = computed(() => state.containers.find((c) => c.kind === 'soketi'))
const up = computed(() => container.value?.status === 'running')
const c = state.config

const stats = computed(() => [
  {
    label: t('common.status'),
    value: up.value ? t('websocket.stats.online') : enabled.value ? t('websocket.stats.stopped') : t('websocket.stats.disabled'),
    sub: up.value ? 'franken_soketi' : enabled.value ? t('websocket.stats.stoppedSub') : 'service enable soketi',
  },
  { label: t('websocket.stats.browsers'), value: 'ws.frankenmanager.test', sub: t('websocket.stats.browsersSub') },
  { label: t('websocket.stats.apps'), value: `127.0.0.1:${c.SOKETI_PORT}`, sub: t('websocket.stats.appsSub') },
])

const mask = (v: string) => (reveal.value ? v : '••••••••')
const credentials = computed(() => [
  { key: 'SOKETI_DEFAULT_APP_ID', label: 'App ID', value: c.SOKETI_DEFAULT_APP_ID, secret: false },
  { key: 'SOKETI_DEFAULT_APP_KEY', label: 'App key', value: c.SOKETI_DEFAULT_APP_KEY, secret: false },
  { key: 'SOKETI_DEFAULT_APP_SECRET', label: 'App secret', value: c.SOKETI_DEFAULT_APP_SECRET, secret: true },
])

/** Laravel variables (`BROADCAST_CONNECTION=pusher`) documented in the CLI README. */
const envFile = (secret: (v: string) => string) => [
  'BROADCAST_CONNECTION=pusher',
  `PUSHER_APP_ID=${c.SOKETI_DEFAULT_APP_ID}`,
  `PUSHER_APP_KEY=${c.SOKETI_DEFAULT_APP_KEY}`,
  `PUSHER_APP_SECRET=${secret(c.SOKETI_DEFAULT_APP_SECRET)}`,
  `# ${t('websocket.env.serverSide')}`,
  'PUSHER_HOST=127.0.0.1',
  `PUSHER_PORT=${c.SOKETI_PORT}`,
  'PUSHER_SCHEME=http',
  'PUSHER_APP_CLUSTER=mt1',
  '',
  `# ${t('websocket.env.browserSide')}`,
  'VITE_PUSHER_APP_KEY="${PUSHER_APP_KEY}"',
  'VITE_PUSHER_HOST=ws.frankenmanager.test',
  'VITE_PUSHER_PORT=443',
  'VITE_PUSHER_SCHEME=https',
  'VITE_PUSHER_APP_CLUSTER="${PUSHER_APP_CLUSTER}"',
].join('\n')

function copyEnv() {
  navigator.clipboard?.writeText(envFile((v) => v))
  toast('info', t('websocket.laravel.copied'), t('websocket.laravel.copiedHelp'))
}
</script>

<template>
  <PageHeader title="WebSocket (Soketi)" :subtitle="t('websocket.subtitle')">
    <UButton
      v-if="enabled" color="neutral" variant="outline" icon="i-lucide-power-off" :label="t('websocket.disable')" size="lg" class="md:w-[205px]"
      :loading="busy" @click="actions.setService('soketi', false)"
    />
    <UButton v-else icon="i-lucide-power" :label="t('websocket.enable')" size="lg" class="md:w-[205px]" :loading="busy" @click="actions.setService('soketi', true)" />
  </PageHeader>
  <MiniStats :items="stats" />

  <TwoColumns class="mt-6">
    <UCard :ui="{ body: 'p-5 sm:p-5' }">
      <div class="flex items-center gap-3">
        <div class="min-w-0">
          <h2 class="text-[15px] font-semibold text-highlighted">{{ t('websocket.laravel.title') }}</h2>
          <p class="mt-1 text-[11px] text-muted">{{ t('websocket.laravel.subtitle') }}</p>
        </div>
        <UButton color="neutral" variant="outline" icon="i-lucide-copy" :label="t('common.copy')" size="sm" class="ml-auto" @click="copyEnv" />
      </div>
      <pre class="mt-3.5 max-h-[420px] overflow-auto rounded-lg bg-sage-950 p-4 font-mono text-xs leading-[19px] text-sage-200">{{ envFile(mask) }}</pre>
    </UCard>

    <UCard :ui="{ body: 'p-5 sm:p-5' }">
      <div class="flex items-center gap-3">
        <h2 class="text-base font-semibold text-highlighted">{{ t('websocket.defaultApp.title') }}</h2>
        <UButton
          color="neutral" variant="ghost" size="sm" class="ml-auto" :icon="reveal ? 'i-lucide-eye-off' : 'i-lucide-eye'"
          :label="reveal ? t('common.hide') : t('common.show')" :aria-pressed="reveal" @click="reveal = !reveal"
        />
      </div>
      <p class="mt-1.5 text-[11px] text-muted">{{ t('websocket.defaultApp.subtitle') }}</p>
      <dl class="mt-5 flex flex-col gap-3 text-xs">
        <div v-for="cr in credentials" :key="cr.key">
          <dt class="text-muted">{{ cr.label }}</dt>
          <dd class="mt-0.5 truncate font-mono text-highlighted">{{ cr.secret ? mask(cr.value) : cr.value }}</dd>
        </div>
      </dl>
      <UButton color="neutral" variant="outline" :label="t('websocket.defaultApp.edit')" to="/configuration" block class="mt-5" />
      <p class="mt-3 text-[10px] leading-[15px] text-muted">{{ t('websocket.defaultApp.note') }}</p>
    </UCard>
  </TwoColumns>

  <InfoBanner v-if="!enabled">{{ t('websocket.banner.disabled', { domain: 'ws.frankenmanager.test' }) }}</InfoBanner>
  <InfoBanner v-else>{{ t('websocket.banner.enabled', { address: `127.0.0.1:${c.SOKETI_PORT}` }) }}</InfoBanner>
</template>
