<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { DropdownMenuItem, TableColumn } from '@nuxt/ui'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import TwoColumns from '../components/TwoColumns.vue'
import ResourceTable from '../components/ResourceTable.vue'
import NameCell from '../components/NameCell.vue'
import StateBadge from '../components/StateBadge.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
import StopStackModal from '../components/StopStackModal.vue'
import DockerAlert from '../components/DockerAlert.vue'
import { DEPRECATED_PHP, SERVICES, actions, active, isServiceBusy, running, serviceEnabled, state, toast, type Container, type ServiceName } from '../composables/useFm'

const tab = ref('all')
const tabs = [{ label: 'Tous les services', value: 'all' }, { label: 'Services optionnels', value: 'services' }, { label: 'Images PHP', value: 'images' }]
const stopOpen = ref(false)

const target = ref(state.containers[0]?.id ?? '')
const action = ref<'restart' | 'stop' | 'start'>('restart')
const rebuild = ref(false)
const details = ref(true)
// Disabling a service removes its container: the form target then falls back to the first one.
const selected = computed(() => state.containers.find((c) => c.id === target.value) ?? state.containers[0] ?? { id: '', label: '', port: '', cpu: 0, mem: 0, status: 'stopped' as const, kind: 'admin' as const })
watch(selected, (c) => { target.value = c.id })
watch(() => selected.value.status, (s) => { action.value = s === 'stopped' ? 'start' : action.value === 'start' ? 'restart' : action.value }, { immediate: true })

const kindLabel = (c: Container) => (c.kind === 'php' ? `FrankenPHP ${c.php}` : c.label)
const fmtCpu = (c: Container) => (c.cpu < 1 ? '< 1 %' : `${Math.round(c.cpu)} %`)
const targetItems = computed(() => state.containers.map((c) => ({ label: `${kindLabel(c)}${c.status === 'stopped' ? ' (arrêté)' : ''}`, value: c.id })))
const actionItems = computed(() => (selected.value.status === 'stopped'
  ? [{ label: 'Démarrer', value: 'start' }]
  : [{ label: 'Redémarrer', value: 'restart' }, { label: 'Arrêter', value: 'stop' }]))
const rebuildItems = [{ label: 'Non · Utiliser l’image actuelle', value: false }, { label: 'Oui · Reconstruire les images PHP (--build)', value: true }]

const totalCpu = computed(() => Math.round(state.containers.reduce((a, c) => a + (c.status === 'running' ? c.cpu : 0), 0)))
const totalMem = computed(() => state.containers.reduce((a, c) => a + (c.status === 'running' ? c.mem : 0), 0))
const stats = computed(() => [
  { label: 'En ligne', value: `${running.value.length} / ${state.containers.length}`, sub: running.value.length === state.containers.length ? 'Services sains' : `${state.containers.length - running.value.length} arrêté(s)` },
  { label: 'CPU total', value: `${totalCpu.value} %`, sub: 'Sur la machine' },
  { label: 'Mémoire', value: `${(totalMem.value / 1000).toLocaleString('fr-FR', { maximumFractionDigits: 1 })} Go`, sub: state.memTotalGo ? `Sur ${state.memTotalGo.toLocaleString('fr-FR')} Go` : 'Sur la machine' },
])
const phpImages = computed(() => state.containers.filter((c) => c.kind === 'php'))

function run() {
  // The rebuild only applies to a PHP container: the choice stays set when the target changes.
  const build = rebuild.value && action.value !== 'stop' && selected.value.kind === 'php' && state.stack === 'running'
  actions.restartContainer(target.value, { rebuild: build, action: action.value })
  if (details.value) toast('info', 'Détails de l’opération', `Commande envoyée pour ${target.value}. Consultez les Journaux pour la sortie.`)
}

// Without Docker, no container action can run.
function menuFor(c: Container): DropdownMenuItem[][] {
  const off = !state.docker
  return [
    [
      c.status === 'stopped'
        ? { label: 'Démarrer', icon: 'i-lucide-play', disabled: off, onSelect: () => actions.restartContainer(c.id, { action: 'start' }) }
        : { label: 'Redémarrer', icon: 'i-lucide-rotate-cw', disabled: off || c.status === 'restarting', onSelect: () => actions.restartContainer(c.id) },
      ...(c.kind === 'php' ? [{ label: 'Reconstruire les images PHP (--build)', icon: 'i-lucide-hammer', disabled: off || state.stack !== 'running' || c.status === 'restarting', onSelect: () => actions.restartContainer(c.id, { rebuild: true }) }] : []),
      { label: 'Sélectionner dans le formulaire', icon: 'i-lucide-mouse-pointer-click', onSelect: () => { target.value = c.id } },
    ],
    [{ label: 'Arrêter', icon: 'i-lucide-square', color: 'error', disabled: off || c.status !== 'running', onSelect: () => actions.restartContainer(c.id, { action: 'stop' }) }],
  ]
}

const columns: TableColumn<Container>[] = [
  { accessorKey: 'id', header: 'Conteneur / image' },
  { id: 'res', header: 'Ressources', meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: 'État', meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]
const imageColumns: TableColumn<Container>[] = [
  { accessorKey: 'id', header: 'Image' },
  { id: 'domains', header: 'Domaines', meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: 'État', meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]
// Optional services: `frankenmanager service enable|disable|status <dns|redis|s3|soketi>`.
interface ServiceRow { name: ServiceName; label: string; desc: string; icon: string; to: string }
const services = computed<ServiceRow[]>(() => (Object.keys(SERVICES) as ServiceName[]).map((name) => ({ name, ...SERVICES[name] })))
function serviceState(s: ServiceName): 'online' | 'offline' | 'restarting' | 'disabled' {
  if (isServiceBusy(s)) return 'restarting'
  if (!serviceEnabled(s)) return 'disabled'
  const up = s === 'dns' ? state.dns.status === 'running' : state.containers.some((c) => c.kind === s && c.status === 'running')
  return up ? 'online' : 'offline'
}
const serviceColumns: TableColumn<ServiceRow>[] = [
  { accessorKey: 'label', header: 'Service' },
  { id: 'state', header: 'État', meta: { class: { th: 'w-28' } } },
  { id: 'toggle', header: 'Activé', meta: { class: { th: 'w-20', td: 'pr-0' } } },
]

const domainsOn = (c: Container) => active.value.filter((d) => d.php === c.php && d.kind !== 'proxy').length
</script>

<template>
  <PageHeader title="Conteneurs" subtitle="Surveillez et pilotez les services Docker de la machine hôte.">
    <UButton v-if="state.stack === 'stopped'" icon="i-lucide-play" label="Démarrer la stack" size="lg" class="md:w-[205px]" :disabled="!state.docker" @click="actions.startStack()" />
    <template v-else>
      <UButton color="neutral" variant="outline" icon="i-lucide-square" label="Arrêter" size="lg" class="max-md:hidden" :disabled="state.stack !== 'running'" @click="stopOpen = true" />
      <UButton icon="i-lucide-rotate-cw" label="Redémarrer la stack" size="lg" class="md:w-[205px]" :loading="state.stack !== 'running'" @click="actions.restartStack()" />
    </template>
  </PageHeader>
  <DockerAlert />
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-4 md:me-16 pb-2.5' }" />

  <TwoColumns>
    <ResourceTable v-if="tab === 'all'" title="Services Docker" :data="state.containers" :columns="columns" search-key="id">
      <template #id-cell="{ row }"><NameCell icon="i-lucide-box" :name="row.original.id" :sub="row.original.port ? `${row.original.label} · :${row.original.port}` : row.original.label" /></template>
      <template #res-cell="{ row }">{{ row.original.status === 'running' ? `${fmtCpu(row.original)} · ${row.original.mem} Mo` : '—' }}</template>
      <template #state-cell="{ row }">
        <StateBadge :value="row.original.status === 'running' ? 'online' : row.original.status === 'restarting' ? 'restarting' : 'offline'" />
      </template>
      <template #actions-cell="{ row }">
        <UDropdownMenu :items="menuFor(row.original)" :content="{ align: 'end' }">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <ResourceTable v-else-if="tab === 'services'" title="Services optionnels" :data="services" :columns="serviceColumns">
      <template #label-cell="{ row }">
        <RouterLink :to="row.original.to" class="block min-w-0 hover:opacity-80">
          <NameCell :icon="row.original.icon" :name="row.original.label" :sub="row.original.desc" />
        </RouterLink>
      </template>
      <template #state-cell="{ row }"><StateBadge :value="serviceState(row.original.name)" /></template>
      <template #toggle-cell="{ row }">
        <USwitch
          :model-value="serviceEnabled(row.original.name)" :disabled="isServiceBusy(row.original.name) || !state.docker"
          :aria-label="`Activer ${row.original.label}`"
          @update:model-value="(v: boolean) => actions.setService(row.original.name, v)"
        />
      </template>
    </ResourceTable>
    <ResourceTable v-else title="Images FrankenPHP" :data="phpImages" :columns="imageColumns">
      <template #id-cell="{ row }">
        <NameCell icon="i-lucide-server" :name="`dunglas/frankenphp:php${row.original.php}`" :sub="DEPRECATED_PHP.includes(row.original.php!) ? 'Version dépréciée' : 'Version supportée'" />
      </template>
      <template #domains-cell="{ row }">{{ domainsOn(row.original) }} domaine{{ domainsOn(row.original) > 1 ? 's' : '' }}</template>
      <template #state-cell="{ row }">
        <StateBadge v-if="DEPRECATED_PHP.includes(row.original.php!)" value="warn" />
        <StateBadge v-else :value="row.original.status === 'running' ? 'online' : 'offline'" ok="À jour" />
      </template>
      <template #actions-cell="{ row }">
        <UDropdownMenu :items="[{ label: 'Reconstruire les images PHP', icon: 'i-lucide-hammer', disabled: !state.docker || state.stack !== 'running', onSelect: () => actions.restartContainer(row.original.id, { rebuild: true }) }]" :content="{ align: 'end' }">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
        </UDropdownMenu>
      </template>
    </ResourceTable>

    <SideForm
      title="Redémarrer des services"
      description="Choisissez la cible sans interrompre les autres services."
      hint="Les domaines de ce conteneur seront brièvement indisponibles. Les données sont conservées."
      @submit="run"
    >
      <UFormField label="Service ciblé"><USelect v-model="target" :items="targetItems" size="lg" class="w-full" /></UFormField>
      <UFormField label="Action"><USelect v-model="action" :items="actionItems" size="lg" class="w-full" /></UFormField>
      <UFormField label="Reconstruction de l’image">
        <USelect v-model="rebuild" :items="rebuildItems" size="lg" class="w-full" :disabled="selected.kind !== 'php' || action === 'stop' || state.stack !== 'running'" />
      </UFormField>
      <USwitch v-model="details" label="Afficher les détails de l’opération" />
      <UButton
        type="submit" size="lg" block :color="action === 'stop' ? 'error' : 'primary'"
        :label="action === 'stop' ? 'Arrêter le service' : action === 'start' ? 'Démarrer le service' : 'Redémarrer le service'"
        :disabled="selected.status === 'restarting' || !state.docker"
      />
    </SideForm>
  </TwoColumns>

  <InfoBanner v-if="!state.docker" tone="warning">Docker est injoignable. Démarrez Docker Desktop pour piloter les conteneurs.</InfoBanner>
  <InfoBanner v-else-if="running.length === state.containers.length">Docker est connecté. Tous les contrôles de santé sont réussis.</InfoBanner>
  <InfoBanner v-else>Docker est connecté. {{ state.containers.length - running.length }} service(s) ne répondent pas.</InfoBanner>
  <StopStackModal v-model:open="stopOpen" />
</template>
