<script setup lang="ts">
import { NAV, pathAvailable } from '../nav'
import { computed, ref, watch } from 'vue'
import type { DropdownMenuItem, TableColumn } from '@nuxt/ui'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import PageTabs from '../components/PageTabs.vue'
import TwoColumns from '../components/TwoColumns.vue'
import ResourceTable from '../components/ResourceTable.vue'
import NameCell from '../components/NameCell.vue'
import StateBadge from '../components/StateBadge.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
import StopStackModal from '../components/StopStackModal.vue'
import DockerAlert from '../components/DockerAlert.vue'
import { DEPRECATED_PHP, SERVICES, actions, active, isServiceBusy, running, serviceEnabled, state, toast, type Container, type ServiceName } from '../composables/useFm'
import { formatMegabytes, formatNumber, formatPercent } from '../i18n'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
/** Optional services are turned on and off in Configuration. */
const CONFIGURATION = NAV.find((n) => n.name === 'configuration')!.path
/** Plural count message, the number formatted in the active language. */
const plural = (key: string, n: number) => t(key, { n: formatNumber(n) }, n)

const tab = ref('all')
const tabs = computed(() => [
  { label: t('containers.tabs.all'), value: 'all' }, { label: t('containers.tabs.services'), value: 'services' }, { label: t('containers.tabs.images'), value: 'images' },
])
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
const fmtCpu = (c: Container) => (c.cpu < 1 ? t('containers.cpuUnderOne', { value: formatPercent(1) }) : formatPercent(Math.round(c.cpu)))
const targetItems = computed(() => state.containers.map((c) => ({ label: c.status === 'stopped' ? t('containers.form.stoppedTarget', { name: kindLabel(c) }) : kindLabel(c), value: c.id })))
const actionItems = computed(() => (selected.value.status === 'stopped'
  ? [{ label: t('containers.actions.start'), value: 'start' }]
  : [{ label: t('containers.actions.restart'), value: 'restart' }, { label: t('containers.actions.stop'), value: 'stop' }]))
const rebuildItems = computed(() => [{ label: t('containers.form.rebuildNo'), value: false }, { label: t('containers.form.rebuildYes', { flag: '--build' }), value: true }])

const totalCpu = computed(() => Math.round(state.containers.reduce((a, c) => a + (c.status === 'running' ? c.cpu : 0), 0)))
const totalMem = computed(() => state.containers.reduce((a, c) => a + (c.status === 'running' ? c.mem : 0), 0))
const stats = computed(() => [
  {
    label: t('containers.stats.online'), value: `${running.value.length} / ${state.containers.length}`,
    sub: running.value.length === state.containers.length ? t('containers.stats.healthy') : plural('containers.stats.stopped', state.containers.length - running.value.length),
  },
  { label: t('containers.stats.cpu'), value: formatPercent(totalCpu.value), sub: t('containers.stats.onMachine') },
  {
    label: t('containers.stats.memory'), value: t('units.gb', { n: formatNumber(totalMem.value / 1000, { maximumFractionDigits: 1 }) }),
    sub: state.memTotalGo ? t('containers.stats.outOf', { total: t('units.gb', { n: formatNumber(state.memTotalGo) }) }) : t('containers.stats.onMachine'),
  },
])
const phpImages = computed(() => state.containers.filter((c) => c.kind === 'php'))

function run() {
  // The rebuild only applies to a PHP container: the choice stays set when the target changes.
  const build = rebuild.value && action.value !== 'stop' && selected.value.kind === 'php' && state.stack === 'running'
  actions.restartContainer(target.value, { rebuild: build, action: action.value })
  if (details.value) toast('info', t('containers.toast.title'), t('containers.toast.sent', { name: target.value }))
}

// Without Docker, no container action can run.
function menuFor(c: Container): DropdownMenuItem[][] {
  const off = !state.docker
  return [
    [
      c.status === 'stopped'
        ? { label: t('containers.actions.start'), icon: 'i-lucide-play', disabled: off, onSelect: () => actions.restartContainer(c.id, { action: 'start' }) }
        : { label: t('containers.actions.restart'), icon: 'i-lucide-rotate-cw', disabled: off || c.status === 'restarting', onSelect: () => actions.restartContainer(c.id) },
      ...(c.kind === 'php' ? [{ label: t('containers.actions.rebuildBuild', { flag: '--build' }), icon: 'i-lucide-hammer', disabled: off || state.stack !== 'running' || c.status === 'restarting', onSelect: () => actions.restartContainer(c.id, { rebuild: true }) }] : []),
      { label: t('containers.actions.select'), icon: 'i-lucide-mouse-pointer-click', onSelect: () => { target.value = c.id } },
    ],
    [{ label: t('containers.actions.stop'), icon: 'i-lucide-square', color: 'error', disabled: off || c.status !== 'running', onSelect: () => actions.restartContainer(c.id, { action: 'stop' }) }],
  ]
}

const columns = computed<TableColumn<Container>[]>(() => [
  { accessorKey: 'id', header: t('containers.columns.container') },
  { id: 'res', header: t('containers.columns.resources'), meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: t('common.status'), meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])
const imageColumns = computed<TableColumn<Container>[]>(() => [
  { accessorKey: 'id', header: t('containers.columns.image') },
  { id: 'domains', header: t('containers.columns.domains'), meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: t('common.status'), meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])
// Optional services: `frankenmanager service enable|disable|status <dns|redis|s3|soketi>`.
interface ServiceRow { name: ServiceName; label: string; desc: string; icon: string; to: string }
const services = computed<ServiceRow[]>(() => (Object.keys(SERVICES) as ServiceName[]).map((name) => ({ name, ...SERVICES[name] })))
function serviceState(s: ServiceName): 'online' | 'offline' | 'restarting' | 'disabled' {
  if (isServiceBusy(s)) return 'restarting'
  if (!serviceEnabled(s)) return 'disabled'
  const up = s === 'dns' ? state.dns.status === 'running' : state.containers.some((c) => c.kind === s && c.status === 'running')
  return up ? 'online' : 'offline'
}
const serviceColumns = computed<TableColumn<ServiceRow>[]>(() => [
  { accessorKey: 'label', header: t('containers.columns.service') },
  { id: 'state', header: t('common.status'), meta: { class: { th: 'w-28' } } },
  { id: 'configure', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])

const domainsOn = (c: Container) => active.value.filter((d) => d.php === c.php && d.kind !== 'proxy').length
</script>

<template>
  <PageHeader :title="t('nav.containers')" :subtitle="t('containers.subtitle')">
    <UButton v-if="state.stack === 'stopped'" icon="i-lucide-play" :label="t('containers.startStack')" size="lg" class="md:w-[205px]" :disabled="!state.docker" @click="actions.startStack()" />
    <template v-else>
      <UButton color="neutral" variant="outline" icon="i-lucide-square" :label="t('containers.actions.stop')" size="lg" class="max-md:hidden" :disabled="state.stack !== 'running'" @click="stopOpen = true" />
      <UButton icon="i-lucide-rotate-cw" :label="t('containers.restartStack')" size="lg" class="md:w-[205px]" :loading="state.stack !== 'running'" @click="actions.restartStack()" />
    </template>
  </PageHeader>
  <DockerAlert />
  <MiniStats :items="stats" />
  <PageTabs v-model="tab" :items="tabs" />

  <TwoColumns>
    <ResourceTable v-if="tab === 'all'" storage-key="containers" :title="t('containers.table.docker')" :data="state.containers" :columns="columns" search-key="id">
      <template #id-cell="{ row }"><NameCell icon="i-lucide-box" :name="row.original.id" :sub="row.original.port ? `${row.original.label} · :${row.original.port}` : row.original.label" /></template>
      <template #res-cell="{ row }">{{ row.original.status === 'running' ? `${fmtCpu(row.original)} · ${formatMegabytes(row.original.mem)}` : '—' }}</template>
      <template #state-cell="{ row }">
        <StateBadge :value="row.original.status === 'running' ? 'online' : row.original.status === 'restarting' ? 'restarting' : 'offline'" />
      </template>
      <template #actions-cell="{ row }">
        <UDropdownMenu :items="menuFor(row.original)" :content="{ align: 'end' }">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <ResourceTable v-else-if="tab === 'services'" storage-key="containers-services" :title="t('containers.tabs.services')" :data="services" :columns="serviceColumns">
      <template #label-cell="{ row }">
        <!-- A service whose page is hidden while it is off (Redis) is not a link. -->
        <RouterLink v-if="pathAvailable(row.original.to)" :to="row.original.to" class="block min-w-0 hover:opacity-80">
          <NameCell :icon="row.original.icon" :name="row.original.label" :sub="row.original.desc" />
        </RouterLink>
        <NameCell v-else :icon="row.original.icon" :name="row.original.label" :sub="row.original.desc" />
      </template>
      <template #state-cell="{ row }"><StateBadge :value="serviceState(row.original.name)" /></template>
      <!-- Turned on and off in Configuration only (Optional services section). -->
      <template #configure-cell="{ row }">
        <UButton
          color="neutral" variant="ghost" icon="i-lucide-settings-2" :to="{ path: CONFIGURATION, query: { section: 'services' } }"
          :aria-label="t('containers.configureService', { name: row.original.label })"
        />
      </template>
    </ResourceTable>
    <ResourceTable v-else :title="t('containers.table.images')" storage-key="containers-images" :data="phpImages" :columns="imageColumns">
      <template #id-cell="{ row }">
        <NameCell icon="i-lucide-server" :name="`dunglas/frankenphp:php${row.original.php}`" :sub="DEPRECATED_PHP.includes(row.original.php!) ? t('containers.images.deprecated') : t('containers.images.supported')" />
      </template>
      <template #domains-cell="{ row }">{{ plural('containers.images.domains', domainsOn(row.original)) }}</template>
      <template #state-cell="{ row }">
        <StateBadge v-if="DEPRECATED_PHP.includes(row.original.php!)" value="warn" />
        <StateBadge v-else :value="row.original.status === 'running' ? 'online' : 'offline'" :ok="t('containers.images.upToDate')" />
      </template>
      <template #actions-cell="{ row }">
        <UDropdownMenu :items="[{ label: t('containers.actions.rebuild'), icon: 'i-lucide-hammer', disabled: !state.docker || state.stack !== 'running', onSelect: () => actions.restartContainer(row.original.id, { rebuild: true }) }]" :content="{ align: 'end' }">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
        </UDropdownMenu>
      </template>
    </ResourceTable>

    <SideForm
      :title="t('containers.form.title')"
      :description="t('containers.form.description')"
      :hint="t('containers.form.hint')"
      @submit="run"
    >
      <UFormField :label="t('containers.form.target')"><USelect v-model="target" :items="targetItems" size="lg" class="w-full" /></UFormField>
      <UFormField :label="t('containers.form.action')"><USelect v-model="action" :items="actionItems" size="lg" class="w-full" /></UFormField>
      <UFormField :label="t('containers.form.rebuild')">
        <USelect v-model="rebuild" :items="rebuildItems" size="lg" class="w-full" :disabled="selected.kind !== 'php' || action === 'stop' || state.stack !== 'running'" />
      </UFormField>
      <USwitch v-model="details" :label="t('containers.form.details')" />
      <UButton
        type="submit" size="lg" block :color="action === 'stop' ? 'error' : 'primary'"
        :label="action === 'stop' ? t('containers.form.submitStop') : action === 'start' ? t('containers.form.submitStart') : t('containers.form.submitRestart')"
        :disabled="selected.status === 'restarting' || !state.docker"
      />
    </SideForm>
  </TwoColumns>

  <InfoBanner v-if="!state.docker" tone="warning">{{ t('containers.banner.unreachable') }}</InfoBanner>
  <InfoBanner v-else-if="running.length === state.containers.length">{{ t('containers.banner.healthy') }}</InfoBanner>
  <InfoBanner v-else>{{ plural('containers.banner.unhealthy', state.containers.length - running.length) }}</InfoBanner>
  <StopStackModal v-model:open="stopOpen" />
</template>
