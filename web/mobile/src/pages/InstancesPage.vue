<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import type { DropdownMenuItem } from '@nuxt/ui'
import { client, logout, type Status } from '@app/lib/api'
import { current, removeInstance, setCurrent, store, type Instance } from '../lib/instances'
import { openPairing, scanPairing } from '../lib/pairing'

const router = useRouter()
const version = __APP_VERSION__
const build = __APP_BUILD__

/** State of each instance at the last contact, for the list. */
interface Probe {
  state: 'loading' | 'online' | 'offline' | 'expired'
  hostname?: string
  running?: boolean
  hosts?: number
  error?: string
}
const probes = reactive<Record<string, Probe>>({})

async function probe(inst: Instance) {
  if (inst.expired) {
    probes[inst.id] = { state: 'expired' }
    return
  }
  probes[inst.id] = { ...probes[inst.id], state: 'loading' }
  try {
    const api = client(inst)
    const [host, status] = await Promise.all([api.get<{ hostname: string }>('/api/v1/host'), api.get<Status>('/api/v1/status')])
    probes[inst.id] = { state: 'online', hostname: host.hostname, running: status.running, hosts: status.hosts.length }
  } catch (e) {
    probes[inst.id] = { state: inst.expired ? 'expired' : 'offline', error: e instanceof Error ? e.message : String(e) }
  }
}
const probeAll = () => store.list.forEach(probe)
onMounted(probeAll)

/** Opens the dashboard on this instance, or its sign-in again if the session expired. */
async function open(inst: Instance) {
  if (inst.expired) return router.push(`/instances/${inst.id}/connexion`)
  await setCurrent(inst.id)
  router.push('/')
}

/** Pairing with the QR code of the dashboard. */
const scanError = ref('')
async function scan() {
  scanError.value = ''
  try {
    const link = await scanPairing()
    if (link) await openPairing(router, link)
  } catch (e) {
    scanError.value = e instanceof Error ? e.message : String(e)
  }
}

const menu = (inst: Instance): DropdownMenuItem[] => [
  { label: 'Se reconnecter', icon: 'i-lucide-key-round', onSelect: () => router.push(`/instances/${inst.id}/connexion`) },
  {
    label: 'Retirer de l’app',
    icon: 'i-lucide-trash-2',
    color: 'error',
    onSelect: async () => {
      await logout(inst)
      await removeInstance(inst.id)
      if (current.id === inst.id) await setCurrent(null)
    },
  },
]

const dot: Record<Probe['state'], string> = {
  loading: 'bg-dimmed animate-pulse',
  online: 'bg-primary',
  offline: 'bg-error',
  expired: 'bg-warning',
}
</script>

<template>
  <div class="h-dvh overflow-y-auto pt-safe pb-safe">
    <header class="px-safe flex items-center justify-between gap-3 pt-4 pb-2">
      <div class="flex items-center gap-2">
        <div class="size-9 rounded-md bg-primary grid place-items-center text-white">
          <UIcon name="i-lucide-server-cog" class="size-5" />
        </div>
        <div>
          <h1 class="text-lg font-semibold text-highlighted leading-tight">FrankenManager</h1>
          <p class="text-xs text-muted">Instances</p>
        </div>
      </div>
      <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" size="lg" aria-label="Actualiser" @click="probeAll" />
    </header>

    <main class="px-safe space-y-3 py-3">
      <UAlert v-if="scanError" color="error" variant="subtle" icon="i-lucide-circle-alert" :description="scanError" />

      <div v-if="!store.list.length" class="rounded-lg border border-dashed border-accented bg-default p-6 text-center space-y-3">
        <UIcon name="i-lucide-smartphone" class="size-10 text-primary mx-auto" />
        <h2 class="font-semibold text-highlighted">Aucune instance</h2>
        <p class="text-sm text-muted">
          Sur la machine qui fait tourner FrankenManager, ouvrez l’accès distant (<code>DASHBOARD_ACCESS=lan</code>), puis scannez le QR
          code d’appairage de son dashboard (Accès distant), ou ajoutez son adresse avec le mot de passe du dashboard.
        </p>
        <UButton icon="i-lucide-scan-qr-code" label="Appairer avec un QR code" size="lg" block @click="scan" />
        <UButton to="/instances/ajouter" icon="i-lucide-plus" label="Ajouter avec le mot de passe" color="neutral" variant="outline" size="lg" block />
      </div>

      <div
        v-for="inst in store.list"
        :key="inst.id"
        class="rounded-lg border border-default bg-default flex items-stretch overflow-hidden active:bg-elevated"
      >
        <button type="button" class="flex-1 min-w-0 text-left p-4 flex gap-3 items-start" @click="open(inst)">
          <span class="mt-1.5 size-2.5 shrink-0 rounded-full" :class="dot[probes[inst.id]?.state ?? 'loading']" />
          <span class="min-w-0 flex-1 space-y-1">
            <span class="block font-semibold text-highlighted truncate">{{ inst.name }}</span>
            <span class="block text-xs text-muted truncate">{{ inst.url.replace('https://', '') }}<template v-if="inst.version"> · v{{ inst.version }}</template></span>
            <span class="flex flex-wrap gap-1.5 pt-1">
              <template v-if="probes[inst.id]?.state === 'online'">
                <UBadge :color="probes[inst.id]?.running ? 'success' : 'neutral'" variant="subtle" :label="probes[inst.id]?.running ? 'Stack en ligne' : 'Stack arrêtée'" />
                <UBadge color="neutral" variant="outline" :label="`${probes[inst.id]?.hosts} domaines`" />
              </template>
              <UBadge v-else-if="probes[inst.id]?.state === 'expired'" color="warning" variant="subtle" label="Session expirée : se reconnecter" />
              <UBadge v-else-if="probes[inst.id]?.state === 'offline'" color="error" variant="subtle" label="Injoignable" />
            </span>
          </span>
          <UIcon name="i-lucide-chevron-right" class="size-5 text-dimmed mt-1" />
        </button>
        <UDropdownMenu :items="menu(inst)" :content="{ align: 'end' }">
          <UButton icon="i-lucide-ellipsis-vertical" color="neutral" variant="ghost" class="rounded-none px-3" aria-label="Actions" />
        </UDropdownMenu>
      </div>

      <template v-if="store.list.length">
        <UButton icon="i-lucide-scan-qr-code" label="Appairer avec un QR code" size="lg" block @click="scan" />
        <UButton to="/instances/ajouter" icon="i-lucide-plus" label="Ajouter avec le mot de passe" color="neutral" variant="outline" size="lg" block />
      </template>

      <p class="pt-4 text-center text-xs text-dimmed">FrankenManager mobile {{ version }} ({{ build }})</p>
    </main>
  </div>
</template>
