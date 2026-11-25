<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import { clock, loadSystem, state, toast } from '../composables/useFm'
import { formatDateTime, formatNumber, formatPercent } from '../i18n'

// The machine of the instance (`GET /api/v1/host/system`): system, hardware, storage, Docker and network. Read when
// the page opens (loadPage) and on demand; nothing here changes by itself fast enough to need a stream.
const { t } = useI18n()
const sys = computed(() => state.system)

const loading = ref(false)
async function reload() {
  loading.value = true
  try {
    await loadSystem()
  } catch (e) {
    toast('error', t('store.loadFailed'), e instanceof Error ? e.message : String(e))
  } finally {
    loading.value = false
  }
}

/** Memory in binary GB (what the systems show for RAM); disks in decimal GB (what Finder and the disk vendors show). */
const ramGb = (n: number) => t('units.gb', { n: formatNumber(n / 1024 ** 3, { maximumFractionDigits: 1 }) })
const diskGb = (n: number) => (n >= 1e12
  ? t('units.tb', { n: formatNumber(n / 1e12, { maximumFractionDigits: 2 }) })
  : t('units.gb', { n: formatNumber(n / 1e9, { maximumFractionDigits: 1 }) }))

/** Duration since a number of seconds: « 3 j 4 h », « 2 h 15 min », « 12 min ». */
function duration(seconds: number) {
  const d = Math.floor(seconds / 86_400)
  const h = Math.floor((seconds % 86_400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d) return t('machine.duration.days', { d: formatNumber(d), h: formatNumber(h) })
  if (h) return t('machine.duration.hours', { h: formatNumber(h), m: formatNumber(m) })
  return t('machine.duration.minutes', { m: formatNumber(m) })
}

// The uptimes move with the shared clock, from the time of the reading.
const readAt = ref(Date.now())
watch(sys, () => { readAt.value = Date.now() })
const uptime = computed(() => (sys.value?.uptime ? sys.value.uptime + Math.max(0, (clock.value - readAt.value) / 1000) : 0))
const daemonUp = computed(() => (sys.value ? Math.max(0, (clock.value - Date.parse(sys.value.daemon.started_at)) / 1000) : 0))
const bootedAt = computed(() => (sys.value?.uptime ? new Date(clock.value - uptime.value * 1000) : null))

const OS_ICON: Record<string, string> = { darwin: 'i-lucide-laptop', linux: 'i-lucide-server', windows: 'i-lucide-monitor' }

const stats = computed(() => {
  const s = sys.value
  if (!s) return []
  return [
    { label: t('machine.stats.system'), value: s.os_name, sub: [s.arch, s.kernel].filter(Boolean).join(' · ') },
    { label: t('machine.stats.cpu'), value: t('machine.cores', { n: formatNumber(s.cpu.cores) }, s.cpu.cores), sub: s.cpu.model || t('machine.unknown') },
    { label: t('machine.stats.memory'), value: s.memory.total ? ramGb(s.memory.total) : '—', sub: t('machine.stats.physical') },
    { label: t('machine.stats.uptime'), value: uptime.value ? duration(uptime.value) : '—', sub: bootedAt.value ? t('machine.since', { date: formatDateTime(bootedAt.value, { dateStyle: 'short', timeStyle: 'short' }) }) : t('machine.unknown') },
  ]
})

const disks = computed(() => (sys.value?.disks ?? []).map((d) => {
  const used = Math.max(0, d.total - d.free)
  const pct = d.total ? (used / d.total) * 100 : 0
  return { ...d, used, pct, color: pct >= 90 ? 'error' as const : pct >= 75 ? 'warning' as const : 'primary' as const }
}))

const accessMode = computed(() => t(`machine.access.${state.config.DASHBOARD_ACCESS || 'local'}`))
</script>

<template>
  <PageHeader :title="t('nav.machine')" :subtitle="sys ? t('machine.subtitle', { host: sys.hostname, os: sys.os_name }) : t('machine.subtitleLoading')">
    <UButton icon="i-lucide-refresh-cw" :label="t('machine.refresh')" size="lg" color="neutral" variant="outline" class="md:w-[160px] justify-center" :loading="loading" @click="reload" />
  </PageHeader>

  <div v-if="!sys" class="mt-6 flex flex-col gap-4">
    <div class="grid grid-cols-2 gap-2.5 md:grid-cols-4 md:gap-3"><USkeleton v-for="i in 4" :key="i" class="h-20" /></div>
    <USkeleton class="h-64" />
  </div>

  <template v-else>
    <MiniStats :items="stats" :cols="4" />

    <div class="mt-6 grid items-start gap-6 xl:grid-cols-2 *:min-w-0">
      <!-- System and hardware -->
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <div class="flex items-center gap-2.5">
          <UIcon :name="OS_ICON[sys.os] ?? 'i-lucide-monitor'" class="size-5 text-primary" />
          <h2 class="text-[15px] font-semibold text-highlighted">{{ t('machine.system.title') }}</h2>
        </div>
        <dl class="mt-4 divide-y divide-default text-xs">
          <div v-for="row in [
            { k: t('machine.system.hostname'), v: sys.hostname, mono: true },
            { k: t('machine.system.os'), v: sys.os_name },
            { k: t('machine.system.kernel'), v: sys.kernel, mono: true },
            { k: t('machine.system.arch'), v: sys.arch, mono: true },
            { k: t('machine.system.cpu'), v: sys.cpu.model },
            { k: t('machine.system.cores'), v: formatNumber(sys.cpu.cores) },
            { k: t('machine.system.memory'), v: sys.memory.total ? ramGb(sys.memory.total) : '' },
            { k: t('machine.system.booted'), v: bootedAt ? formatDateTime(bootedAt, { dateStyle: 'medium', timeStyle: 'short' }) : '' },
          ].filter((r) => r.v)" :key="row.k" class="flex items-baseline justify-between gap-4 py-2.5">
            <dt class="shrink-0 text-muted">{{ row.k }}</dt>
            <dd class="min-w-0 text-right break-words text-highlighted" :class="row.mono && 'font-mono'">{{ row.v }}</dd>
          </div>
        </dl>
      </UCard>

      <!-- Storage -->
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <div class="flex items-center gap-2.5">
          <UIcon name="i-lucide-hard-drive" class="size-5 text-primary" />
          <h2 class="text-[15px] font-semibold text-highlighted">{{ t('machine.storage.title') }}</h2>
        </div>
        <p v-if="!disks.length" class="mt-4 text-xs text-muted">{{ t('machine.storage.none') }}</p>
        <div v-for="d in disks" :key="d.label" class="mt-5">
          <div class="flex items-baseline justify-between gap-3">
            <p class="text-xs font-medium text-highlighted">{{ t(`machine.storage.${d.label}`) }}</p>
            <p class="shrink-0 text-[11px] text-muted">{{ t('machine.storage.free', { free: diskGb(d.free), total: diskGb(d.total) }) }}</p>
          </div>
          <UProgress :model-value="d.pct" :color="d.color" size="sm" class="mt-2" />
          <div class="mt-1.5 flex items-baseline justify-between gap-3 text-[11px] text-muted">
            <span class="min-w-0 truncate font-mono" :title="d.path">{{ d.path }}</span>
            <span class="shrink-0">{{ t('machine.storage.used', { pct: formatPercent(d.pct, { maximumFractionDigits: 0 }) }) }}</span>
          </div>
        </div>
      </UCard>

      <!-- FrankenManager daemon -->
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <div class="flex items-center gap-2.5">
          <UIcon name="i-lucide-square-terminal" class="size-5 text-primary" />
          <h2 class="text-[15px] font-semibold text-highlighted">{{ t('machine.daemon.title') }}</h2>
        </div>
        <dl class="mt-4 divide-y divide-default text-xs">
          <div v-for="row in [
            { k: t('machine.daemon.version'), v: sys.daemon.version, mono: true },
            { k: t('machine.daemon.go'), v: sys.daemon.go, mono: true },
            { k: t('machine.daemon.pid'), v: String(sys.daemon.pid), mono: true },
            { k: t('machine.daemon.uptime'), v: duration(daemonUp) },
            { k: t('machine.daemon.access'), v: accessMode },
          ]" :key="row.k" class="flex items-baseline justify-between gap-4 py-2.5">
            <dt class="shrink-0 text-muted">{{ row.k }}</dt>
            <dd class="min-w-0 text-right break-words text-highlighted" :class="row.mono && 'font-mono'">{{ row.v }}</dd>
          </div>
        </dl>
      </UCard>

      <!-- Docker engine -->
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <div class="flex items-center gap-2.5">
          <UIcon name="i-lucide-container" class="size-5 text-primary" />
          <h2 class="text-[15px] font-semibold text-highlighted">{{ t('machine.docker.title') }}</h2>
          <UBadge :label="sys.docker ? t('machine.docker.up') : t('machine.docker.down')" :color="sys.docker ? 'success' : 'error'" variant="soft" size="sm" class="ml-auto" />
        </div>
        <p v-if="!sys.docker" class="mt-4 text-xs text-muted">{{ t('machine.docker.downHelp') }}</p>
        <dl v-else class="mt-4 divide-y divide-default text-xs">
          <div v-for="row in [
            { k: t('machine.docker.version'), v: sys.docker.version, mono: true },
            { k: t('machine.docker.os'), v: sys.docker.os },
            { k: t('machine.docker.cpus'), v: sys.docker.cpus ? formatNumber(sys.docker.cpus) : '' },
            { k: t('machine.docker.memory'), v: sys.docker.memory ? ramGb(sys.docker.memory) : '' },
          ].filter((r) => r.v)" :key="row.k" class="flex items-baseline justify-between gap-4 py-2.5">
            <dt class="shrink-0 text-muted">{{ row.k }}</dt>
            <dd class="min-w-0 text-right break-words text-highlighted" :class="row.mono && 'font-mono'">{{ row.v }}</dd>
          </div>
        </dl>
      </UCard>
    </div>

    <!-- Network -->
    <UCard class="mt-6" :ui="{ body: 'p-5 sm:p-5' }">
      <div class="flex flex-wrap items-center gap-2.5">
        <UIcon name="i-lucide-network" class="size-5 text-primary" />
        <h2 class="text-[15px] font-semibold text-highlighted">{{ t('machine.network.title') }}</h2>
        <span v-if="sys.network.lan_ip" class="ml-auto text-xs text-muted">
          {{ t('machine.network.lan') }} <span class="font-mono text-highlighted">{{ sys.network.lan_ip }}</span>
        </span>
      </div>
      <p v-if="!sys.network.interfaces.length" class="mt-4 text-xs text-muted">{{ t('machine.network.none') }}</p>
      <div class="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        <div v-for="i in sys.network.interfaces" :key="i.name" class="min-w-0 rounded-lg border border-default p-3">
          <div class="flex items-center gap-2">
            <span class="font-mono text-xs font-semibold text-highlighted">{{ i.name }}</span>
            <UBadge :label="i.up ? t('machine.network.up') : t('machine.network.down')" :color="i.up ? 'success' : 'neutral'" variant="soft" size="sm" class="ml-auto" />
          </div>
          <p v-if="i.mac" class="mt-1 font-mono text-[11px] text-muted">{{ i.mac }}</p>
          <ul class="mt-2 flex flex-col gap-0.5">
            <li v-for="a in i.addresses" :key="a" class="truncate font-mono text-[11px] text-toned" :title="a">{{ a }}</li>
          </ul>
        </div>
      </div>
    </UCard>
  </template>
</template>
