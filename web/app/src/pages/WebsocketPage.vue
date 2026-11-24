<script setup lang="ts">
import { computed, ref } from 'vue'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import TwoColumns from '../components/TwoColumns.vue'
import InfoBanner from '../components/InfoBanner.vue'
import { actions, isServiceBusy, serviceEnabled, state, toast } from '../composables/useFm'

// Soketi (Pusher protocol): `frankenmanager service enable|disable soketi`.
const reveal = ref(false)
const enabled = computed(() => serviceEnabled('soketi'))
const busy = computed(() => isServiceBusy('soketi'))
const container = computed(() => state.containers.find((c) => c.kind === 'soketi'))
const up = computed(() => container.value?.status === 'running')
const c = state.config

const stats = computed(() => [
  { label: 'État', value: up.value ? 'En ligne' : enabled.value ? 'Arrêté' : 'Désactivé', sub: up.value ? 'franken_soketi' : enabled.value ? 'Démarrera avec la stack' : 'service enable soketi' },
  { label: 'Navigateurs', value: 'ws.frankenmanager.test', sub: 'wss:// · port 443 via Caddy' },
  { label: 'Applications PHP', value: `127.0.0.1:${c.SOKETI_PORT}`, sub: 'API HTTP, sans TLS' },
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
  '# Côté serveur : PHP -> Soketi',
  'PUSHER_HOST=127.0.0.1',
  `PUSHER_PORT=${c.SOKETI_PORT}`,
  'PUSHER_SCHEME=http',
  'PUSHER_APP_CLUSTER=mt1',
  '',
  '# Côté navigateur : Laravel Echo -> reverse proxy',
  'VITE_PUSHER_APP_KEY="${PUSHER_APP_KEY}"',
  'VITE_PUSHER_HOST=ws.frankenmanager.test',
  'VITE_PUSHER_PORT=443',
  'VITE_PUSHER_SCHEME=https',
  'VITE_PUSHER_APP_CLUSTER="${PUSHER_APP_CLUSTER}"',
].join('\n')

function copyEnv() {
  navigator.clipboard?.writeText(envFile((v) => v))
  toast('info', 'Variables copiées', 'Collez-les dans le .env de votre projet Laravel.')
}
</script>

<template>
  <PageHeader title="WebSocket (Soketi)" subtitle="Serveur compatible Pusher pour Laravel Echo et le broadcasting.">
    <UButton
      v-if="enabled" color="neutral" variant="outline" icon="i-lucide-power-off" label="Désactiver Soketi" size="lg" class="md:w-[205px]"
      :loading="busy" @click="actions.setService('soketi', false)"
    />
    <UButton v-else icon="i-lucide-power" label="Activer Soketi" size="lg" class="md:w-[205px]" :loading="busy" @click="actions.setService('soketi', true)" />
  </PageHeader>
  <MiniStats :items="stats" />

  <TwoColumns class="mt-6">
    <UCard :ui="{ body: 'p-5 sm:p-5' }">
      <div class="flex items-center gap-3">
        <div class="min-w-0">
          <h2 class="text-[15px] font-semibold text-highlighted">Variables pour Laravel</h2>
          <p class="mt-1 text-[11px] text-muted">Toutes les applications partagent l’app par défaut.</p>
        </div>
        <UButton color="neutral" variant="outline" icon="i-lucide-copy" label="Copier" size="sm" class="ml-auto" @click="copyEnv" />
      </div>
      <pre class="mt-3.5 max-h-[420px] overflow-auto rounded-lg bg-sage-950 p-4 font-mono text-xs leading-[19px] text-sage-200">{{ envFile(mask) }}</pre>
    </UCard>

    <UCard :ui="{ body: 'p-5 sm:p-5' }">
      <div class="flex items-center gap-3">
        <h2 class="text-base font-semibold text-highlighted">App par défaut</h2>
        <UButton
          color="neutral" variant="ghost" size="sm" class="ml-auto" :icon="reveal ? 'i-lucide-eye-off' : 'i-lucide-eye'"
          :label="reveal ? 'Masquer' : 'Afficher'" :aria-pressed="reveal" @click="reveal = !reveal"
        />
      </div>
      <p class="mt-1.5 text-[11px] text-muted">Générés au démarrage s’ils sont vides.</p>
      <dl class="mt-5 flex flex-col gap-3 text-xs">
        <div v-for="cr in credentials" :key="cr.key">
          <dt class="text-muted">{{ cr.label }}</dt>
          <dd class="mt-0.5 truncate font-mono text-highlighted">{{ cr.secret ? mask(cr.value) : cr.value }}</dd>
        </div>
      </dl>
      <UButton color="neutral" variant="outline" label="Modifier dans Configuration" to="/configuration" block class="mt-5" />
      <p class="mt-3 text-[10px] leading-[15px] text-muted">Un identifiant modifié s’applique au prochain stop / start.</p>
    </UCard>
  </TwoColumns>

  <InfoBanner v-if="!enabled">Soketi est désactivé. L’activer crée le conteneur, son certificat, l’entrée hosts, la route du reverse proxy et l’enregistrement DNS de ws.frankenmanager.test.</InfoBanner>
  <InfoBanner v-else>Les navigateurs passent par Caddy en wss:// sur le port 443 ; vos applications PHP publient les événements sur 127.0.0.1:{{ c.SOKETI_PORT }}.</InfoBanner>
</template>
