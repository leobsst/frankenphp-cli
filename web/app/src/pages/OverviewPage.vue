<script setup lang="ts">
import { computed, ref } from 'vue'
import type { TableColumn } from '@nuxt/ui'
import PageHeader from '../components/PageHeader.vue'
import StatusDot from '../components/StatusDot.vue'
import DomainActions from '../components/DomainActions.vue'
import DeviceModal from '../components/DeviceModal.vue'
import StopStackModal from '../components/StopStackModal.vue'
import DockerAlert from '../components/DockerAlert.vue'
import { NAV } from '../nav'
import {
  DEPRECATED_PHP, LIVE_NOISE, S3_DOMAIN, actions, active, domainIcon, domainStatus, envLabel, phpVersions, running, servedPath, serviceEnabled, state,
  type ContainerKind, type Domain,
} from '../composables/useFm'
import { formatNumber, formatPercent } from '../i18n'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const q = ref('')
const type = ref<'all' | Domain['kind']>('all')
const stopOpen = ref(false)
const deviceOpen = ref(false)

const stopped = computed(() => state.stack === 'stopped')
const busy = computed(() => state.stack === 'starting' || state.stack === 'stopping')
const total = computed(() => state.containers.length)
const allUp = computed(() => running.value.length === total.value)
const count = (k: Domain['kind']) => active.value.filter((d) => d.kind === k).length
const cpu = computed(() => (stopped.value ? 0 : state.cpuHistory[state.cpuHistory.length - 1]))
const maxCpu = computed(() => Math.max(...state.cpuHistory, 30))
const rows = computed(() => active.value.filter((d) => d.name.includes(q.value.toLowerCase()) && (type.value === 'all' || d.kind === type.value)))
/** The overview only previews the first domains of the filtered list; the full list lives on the Projects page. */
const PREVIEW = 5
const preview = computed(() => rows.value.slice(0, PREVIEW))
const projectsPath = NAV.find((n) => n.name === 'projects')!.path
const activity = computed(() => state.logs.filter((l) => !LIVE_NOISE.test(l.msg)).slice(0, 3))
const deprecated = computed(() => phpVersions.value.filter((v) => DEPRECATED_PHP.includes(v)))
const up = (kind: ContainerKind) => { const list = state.containers.filter((c) => c.kind === kind); return list.length > 0 && list.every((c) => c.status === 'running') }
/** Database engines present in the stack (franken_mariadb, franken_mysql, franken_postgresql). */
const dbs = computed(() => state.containers.filter((c) => c.kind === 'mariadb' || c.kind === 'postgres'))

const typeItems = computed(() => [
  { label: t('overview.types.all'), value: 'all' },
  { label: t('overview.types.php'), value: 'php' },
  { label: t('overview.types.static'), value: 'static' },
  { label: t('overview.types.proxy'), value: 'proxy' },
  { label: t('overview.types.alias'), value: 'alias' },
])

const columns = computed<TableColumn<Domain>[]>(() => [
  { accessorKey: 'name', header: t('overview.columns.name'), meta: { class: { th: 'w-[34%]' } } },
  { id: 'env', header: t('overview.columns.env'), meta: { class: { th: 'max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'status', header: t('overview.columns.status') },
  { id: 'https', header: 'HTTPS', meta: { class: { th: 'max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'actions', header: t('common.actions'), meta: { class: { th: 'w-[110px] text-left max-md:w-12', td: 'py-1' } } },
])

/** Plural count message, the number formatted in the active language. */
const plural = (key: string, n: number) => t(key, { n: formatNumber(n) }, n)

const domainCount = computed(() => plural('overview.domainCount', active.value.length))
/** Breakdown of the local domains by kind; static hosts and aliases only when there are some. */
const domainBreakdown = computed(() => [
  plural('overview.stats.phpProjects', count('php')),
  ...(count('static') ? [plural('overview.stats.staticSites', count('static'))] : []),
  plural('overview.stats.proxies', count('proxy')),
  ...(count('alias') ? [plural('overview.stats.aliases', count('alias'))] : []),
].join(' · '))

// Optional services (DNS, Redis, S3, Soketi) only appear when enabled.
const infra = computed(() => [
  {
    name: 'FrankenPHP', icon: 'i-lucide-server', to: '/conteneurs', up: up('php'),
    desc: t('overview.infra.php.desc', { n: formatNumber(phpVersions.value.length), versions: phpVersions.value.join(' / ') }, phpVersions.value.length),
    meta: deprecated.value.length ? t('overview.infra.php.toMigrate', { versions: deprecated.value.join(', ') }) : t('overview.infra.php.upToDate'), warn: deprecated.value.length > 0,
  },
  {
    name: 'Caddy', icon: 'i-lucide-arrow-up-right', to: '/reverse-proxy', up: up('caddy'),
    desc: t('overview.infra.caddy.desc', { http: state.config.WEB_HTTP_PORT || 80, https: state.config.WEB_HTTPS_PORT || 443 }),
    meta: plural('overview.infra.caddy.routes', active.value.length),
  },
  ...(state.dns.enabled ? [{
    name: t('nav.dns'), icon: 'i-lucide-globe', to: '/dns', up: state.dns.status === 'running', desc: t('overview.infra.dns.desc', { address: `${state.dns.ip}:${state.dns.port}` }),
    meta: `${plural('overview.devicesConnected', state.dns.connected)} · ${domainCount.value}`,
  }] : []),
  ...(serviceEnabled('redis') ? [{ name: 'Redis', icon: 'i-lucide-zap', to: '/redis', up: up('redis'), desc: t('overview.infra.redis.desc', { port: state.config.REDIS_PORT }), meta: t('overview.infra.container', { name: 'franken_redis' }) }] : []),
  ...(serviceEnabled('soketi') ? [{ name: 'Soketi', icon: 'i-lucide-radio', to: '/websocket', up: up('soketi'), desc: 'WebSocket · wss://ws.frankenmanager.test', meta: t('overview.infra.soketi.meta', { address: `127.0.0.1:${state.config.SOKETI_PORT}` }) }] : []),
  ...(serviceEnabled('s3') ? [{ name: t('nav.s3'), icon: 'i-lucide-hard-drive', to: '/s3', up: up('s3'), desc: `Garage · https://${S3_DOMAIN}`, meta: t('overview.infra.s3.meta') }] : []),
  ...dbs.value.map((c) => {
    const n = state.databases.filter((d) => d.engine === c.label).length
    return { name: c.label, icon: 'i-lucide-database', to: '/bases-de-donnees', up: c.status === 'running', desc: t('overview.infra.db.desc', { port: c.port }), meta: `${plural('overview.infra.db.databases', n)} · ${c.id}` }
  }),
])

const statCard = { body: 'p-0 sm:p-0 h-[112px] px-[15px] py-[13px] sm:px-[15px] sm:py-[13px] flex flex-col relative' }
</script>

<template>
  <PageHeader large :title="t('overview.title')" :subtitle="t('overview.subtitle')">
    <UButton color="neutral" variant="outline" icon="i-lucide-rotate-cw" :label="t('overview.restart')" class="md:w-36" :disabled="stopped || busy || !state.docker" @click="actions.restartStack()" />
    <UButton v-if="stopped" icon="i-lucide-play" class="md:w-[206px]" :label="t('overview.startStack')" :disabled="!state.docker" @click="actions.startStack()" />
    <UButton
      v-else color="neutral" variant="outline" icon="i-lucide-square" class="md:w-[206px]" :loading="busy"
      :label="busy ? (state.stack === 'starting' ? t('overview.starting') : t('overview.stopping')) : t('overview.stopStack')"
      @click="stopOpen = true"
    />
  </PageHeader>
  <DockerAlert />

  <!-- Indicators -->
  <div class="mt-6 grid grid-cols-2 gap-2.5 md:gap-4 xl:mt-[30px] xl:grid-cols-4 *:min-w-0">
    <UCard :ui="statCard">
      <span class="text-xs font-medium text-muted">{{ t('overview.stats.containers') }}</span>
      <UIcon name="i-lucide-box" class="absolute top-3.5 right-5 size-[18px] text-muted" />
      <span class="mt-1.5 flex items-center gap-4 text-[28px] leading-[41px] font-semibold text-highlighted max-md:text-2xl">
        {{ running.length }} / {{ total }}
        <UBadge v-if="allUp" :label="t('overview.stats.online')" variant="soft" class="max-md:hidden" />
        <UBadge v-else-if="running.length === 0" :label="t('overview.stats.stopped')" color="error" variant="soft" class="max-md:hidden" />
        <UBadge v-else :label="t('overview.stats.degraded')" color="warning" variant="soft" class="max-md:hidden" />
      </span>
      <span class="mt-auto text-[11px] text-muted">{{ allUp ? t('overview.stats.allOperational') : plural('overview.stats.unavailable', total - running.length) }}</span>
    </UCard>
    <UCard :ui="statCard">
      <span class="text-xs font-medium text-muted">{{ t('overview.stats.domains') }}</span>
      <UIcon name="i-lucide-globe" class="absolute top-3.5 right-5 size-[18px] text-muted" />
      <span class="mt-1.5 text-[28px] leading-[41px] font-semibold text-highlighted max-md:text-2xl">{{ active.length }}</span>
      <span class="mt-auto truncate text-[11px] text-muted">{{ domainBreakdown }}</span>
    </UCard>
    <UCard :ui="statCard">
      <span class="text-xs font-medium text-muted">{{ t('overview.stats.certificates') }}</span>
      <UIcon name="i-lucide-lock" class="absolute top-3.5 right-5 size-[18px] text-muted" />
      <span class="mt-1.5 text-[28px] leading-[41px] font-semibold text-highlighted max-md:text-2xl">{{ active.length }} / {{ active.length }}</span>
      <span class="mt-auto text-[11px] text-muted">{{ t('overview.stats.certificatesTrusted') }}</span>
    </UCard>
    <UCard :ui="statCard">
      <span class="text-xs font-medium text-muted">{{ t('overview.stats.cpu') }}</span>
      <UIcon name="i-lucide-activity" class="absolute top-3.5 right-5 size-[18px] text-muted" />
      <span class="mt-1.5 flex items-center text-[28px] leading-[41px] font-semibold text-highlighted max-md:text-2xl">
        {{ formatPercent(cpu) }}
        <span class="mr-1 ml-auto flex h-[30px] items-end gap-1 max-md:hidden" aria-hidden="true">
          <span
            v-for="(v, i) in state.cpuHistory" :key="i"
            class="w-1 rounded-xs transition-[height] duration-400"
            :class="i >= state.cpuHistory.length - 3 ? 'bg-primary' : 'bg-primary/30'"
            :style="{ height: `${stopped ? 2 : Math.max(4, (v / maxCpu) * 30)}px` }"
          />
        </span>
      </span>
      <span class="mt-auto text-[11px] text-muted">{{ t('overview.stats.memory', { used: formatNumber(state.memGo), total: t('units.gb', { n: formatNumber(state.memTotalGo) }) }) }}</span>
    </UCard>
  </div>

  <!-- Projects & domains -->
  <UCard class="mt-6" :ui="{ body: 'p-0 sm:p-0 px-5 pt-3 pb-1.5 sm:px-5 sm:pt-3 sm:pb-1.5' }">
    <div class="flex flex-wrap items-center gap-x-6 gap-y-2">
      <h2 class="text-base font-semibold text-highlighted">{{ t('nav.projects') }}</h2>
      <span class="text-xs text-muted">{{ domainCount }}</span>
      <UButton icon="i-lucide-plus" :label="t('overview.addProject')" class="ml-auto md:w-[165px]" :to="projectsPath" />
    </div>
    <div class="mt-3.5 flex flex-wrap gap-3">
      <UInput v-model="q" icon="i-lucide-search" :placeholder="t('overview.filterPlaceholder')" class="w-full md:w-[300px]" :ui="{ base: 'bg-muted' }" />
      <USelect v-model="type" :items="typeItems" class="w-40" />
    </div>
    <UTable
      :data="preview" :columns="columns" class="mt-3" :empty="t('overview.empty')"
      :ui="{ th: 'px-0 pr-4 py-2 text-[10px] font-semibold uppercase text-muted', td: 'px-0 pr-4 py-2 text-xs', separator: 'bg-(--ui-border)' }"
    >
      <template #name-cell="{ row }">
        <div class="flex min-w-0 items-center gap-3 max-md:max-w-[44vw]">
          <UIcon :name="domainIcon(row.original)" class="size-[18px] shrink-0 text-muted" />
          <div class="min-w-0">
            <p class="truncate text-[13px] leading-[19px] font-medium text-highlighted">{{ row.original.name }}</p>
            <p class="truncate text-[10px] leading-[15px] text-muted">
              {{ row.original.kind === 'proxy' ? row.original.target : row.original.kind === 'alias' ? t('overview.aliasOf', { name: row.original.aliasOf }) : servedPath(row.original) }}
            </p>
          </div>
        </div>
      </template>
      <template #env-cell="{ row }">
        <UBadge v-if="!row.original.php" :label="envLabel(row.original)" variant="soft" :color="row.original.kind === 'static' ? 'neutral' : 'primary'" />
        <UBadge v-else :label="`PHP ${row.original.php}`" variant="soft" :color="DEPRECATED_PHP.includes(row.original.php!) ? 'warning' : 'primary'" />
      </template>
      <template #status-cell="{ row }"><StatusDot :value="domainStatus(row.original)" /></template>
      <template #https-cell>
        <span class="inline-flex items-center gap-2 text-muted"><UIcon name="i-lucide-lock" class="size-[15px] text-primary" />{{ t('overview.valid') }}</span>
      </template>
      <template #actions-cell="{ row }">
        <div class="flex items-center gap-5">
          <UButton color="neutral" variant="ghost" icon="i-lucide-arrow-up-right" class="max-md:hidden" :aria-label="t('overview.open', { name: row.original.name })" @click="actions.openSite(row.original.name)" />
          <DomainActions :domain="row.original" />
        </div>
      </template>
    </UTable>
    <div v-if="rows.length > PREVIEW" class="flex justify-center border-t border-default pt-2">
      <UButton
        color="neutral" variant="ghost" size="sm" trailing-icon="i-lucide-arrow-right" :to="projectsPath"
        :label="t('overview.seeAllDomains', { n: formatNumber(active.length) })"
      />
    </div>
  </UCard>

  <!-- Infrastructure -->
  <section class="mt-7">
    <div class="mb-3.5 flex flex-wrap items-baseline gap-x-5 gap-y-1">
      <h2 class="text-[17px] font-semibold text-highlighted">{{ t('overview.infra.title') }}</h2>
      <span class="text-xs text-muted">{{ plural('overview.infra.containers', total) }}</span>
      <ULink to="/conteneurs" class="ml-auto text-xs font-medium text-primary md:mr-6">{{ t('overview.infra.manage') }}</ULink>
    </div>
    <div class="grid gap-x-4 gap-y-3 md:grid-cols-2 xl:grid-cols-3 *:min-w-0">
      <RouterLink v-for="s in infra" :key="s.name" :to="s.to" class="group">
        <UCard class="h-[82px] transition-colors group-hover:border-accented" :ui="{ body: 'p-0 sm:p-0 relative flex gap-3 px-3.5 pt-3.5 sm:px-3.5 sm:pt-3.5' }">
          <span class="grid size-[34px] shrink-0 place-items-center rounded-lg bg-muted text-muted"><UIcon :name="s.icon" class="size-[18px]" /></span>
          <div class="min-w-0">
            <p class="-mt-0.5 text-[13px] leading-[19px] font-semibold text-highlighted">{{ s.name }}</p>
            <p class="mt-0.5 truncate text-[11px] text-muted">{{ s.desc }}</p>
            <p class="mt-1.5 truncate text-[11px] font-medium" :class="!s.up ? 'text-error' : s.warn ? 'text-warning' : 'text-primary'">{{ s.up ? s.meta : t('overview.infra.stopped') }}</p>
          </div>
          <span class="absolute top-5 right-5 size-1.5 rounded-full" :class="s.up ? 'bg-primary' : 'bg-error'" />
        </UCard>
      </RouterLink>
    </div>
  </section>

  <!-- Activity + local network -->
  <div class="mt-7 grid gap-5 xl:grid-cols-[712px_1fr] *:min-w-0">
    <UCard :ui="{ body: 'px-5 py-3.5 sm:px-5 sm:py-3.5' }">
      <div class="flex items-center justify-between md:pr-6">
        <h2 class="text-[15px] font-semibold text-highlighted">{{ t('overview.activity.title') }}</h2>
        <ULink to="/journaux" class="text-[11px] font-medium text-primary">{{ t('overview.activity.seeLogs') }}</ULink>
      </div>
      <ul class="mt-3.5 flex flex-col gap-4">
        <li v-for="l in activity" :key="l.id" class="grid grid-cols-[16px_minmax(0,1fr)] items-center gap-y-0.5 md:grid-cols-[70px_18px_minmax(0,1fr)_200px]">
          <time class="text-[11px] text-muted max-md:col-start-2 max-md:row-start-2">{{ l.time }}</time>
          <span class="size-1.5 rounded-full" :class="l.level === 'WARN' ? 'bg-warning' : l.level === 'ERROR' ? 'bg-error' : 'bg-primary'" />
          <span class="truncate text-xs font-medium text-highlighted">{{ l.msg }}</span>
          <span class="truncate text-[11px] text-muted max-md:hidden">{{ l.source }}</span>
        </li>
      </ul>
    </UCard>
    <UCard class="bg-primary/5" :ui="{ body: 'px-5 py-4 sm:px-5 sm:py-4' }">
      <h3 class="flex items-center gap-3 text-sm font-semibold text-highlighted"><UIcon name="i-lucide-globe" class="size-5 text-primary" />{{ t('overview.devices.title') }}</h3>
      <p class="mt-4 text-xs text-muted">
        {{ t('overview.devices.localDns', { ip: state.dns.ip }) }}
        <template v-if="state.dns.status === 'running'"> · <RouterLink to="/dns" class="font-medium text-primary">{{ plural('overview.devicesConnected', state.dns.connected) }}</RouterLink></template>
        <template v-else-if="!state.dns.enabled"> · {{ t('overview.devices.dnsDisabled') }}</template>
      </p>
      <p class="mt-1 text-xs text-muted">{{ t('overview.devices.installCa') }}</p>
      <div class="mt-4 flex flex-wrap items-center gap-3">
        <UButton color="neutral" variant="outline" icon="i-lucide-arrow-up-right" :label="t('overview.devices.configure')" class="bg-default max-md:w-full max-md:justify-center md:w-56" @click="deviceOpen = true" />
        <span class="text-[10px] text-muted">{{ state.ssl.caShared ? t('overview.devices.caShared', { port: state.ssl.caPort }) : t('overview.devices.caNotShared') }}</span>
      </div>
    </UCard>
  </div>

  <StopStackModal v-model:open="stopOpen" />
  <DeviceModal v-model:open="deviceOpen" />
</template>
