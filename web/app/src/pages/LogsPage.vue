<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import PageTabs from '../components/PageTabs.vue'
import TwoColumns from '../components/TwoColumns.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
import TablePagination from '../components/TablePagination.vue'
import { usePagination } from '../composables/usePagination'
import { LIVE_NOISE, state, toast, type LogLevel } from '../composables/useFm'
import { formatNumber } from '../i18n'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const LEVELS: LogLevel[] = ['INFO', 'WARN', 'ERROR']
const tab = ref('live')
const tabs = computed(() => [{ label: t('logs.tabs.live'), value: 'live' }, { label: t('logs.tabs.history'), value: 'history' }])
const levels = computed(() => [{ label: t('logs.levels.info'), value: 'INFO' }, { label: t('logs.levels.warn'), value: 'WARN' }, { label: t('logs.levels.error'), value: 'ERROR' }])
const periods = computed(() => [{ label: t('logs.periods.last30'), value: '30' }, { label: t('logs.periods.lastHour'), value: '60' }, { label: t('logs.periods.last24'), value: '1440' }])

const draft = reactive({ service: 'all', level: 'INFO' as LogLevel, period: '30', q: '', autoscroll: true })
const applied = reactive({ ...draft })
const pausedAt = ref<number | null>(null) // last visible log when paused

const sources = computed(() => [{ label: t('logs.allServices'), value: 'all' }, ...[...new Set(state.logs.map((l) => l.source))].sort().map((s) => ({ label: s, value: s }))])
const warnings = computed(() => state.logs.filter((l) => l.level === 'WARN'))
const errors = computed(() => state.logs.filter((l) => l.level === 'ERROR'))
const live = computed(() => tab.value === 'live' && applied.autoscroll && state.prefs.autoRefresh)

const filtered = computed(() => state.logs.filter((l) =>
  (applied.service === 'all' || l.source === applied.service) &&
  LEVELS.indexOf(l.level) >= LEVELS.indexOf(applied.level) &&
  (!applied.q || `${l.source} ${l.msg}`.toLowerCase().includes(applied.q.toLowerCase())) &&
  (live.value || pausedAt.value === null || l.id <= pausedAt.value) &&
  (tab.value === 'live' || !LIVE_NOISE.test(l.msg)),
))

/*
 * Pagination of the in-memory buffer (newest first, so page 1 is the live head of the stream).
 * Leaving page 1 anchors the list on the newest event seen: incoming events no longer shift the page being read,
 * they are counted instead and shown again when going back to page 1.
 */
const anchor = ref<number | null>(null)
const shown = computed(() => (anchor.value === null ? filtered.value : filtered.value.filter((l) => l.id <= anchor.value!)))
const fresh = computed(() => filtered.value.length - shown.value.length)
const pager = usePagination(shown, {
  key: 'logs',
  resetOn: [tab, () => applied.service, () => applied.level, () => applied.q],
})
const { page, pageSize, total, from, to, rows } = pager
watch(page, (p) => {
  if (p === 1) anchor.value = null
  else if (anchor.value === null) anchor.value = state.logs[0]?.id ?? null
}, { flush: 'sync' })

const stats = computed(() => [
  { label: t('logs.stats.events'), value: formatNumber(state.logs.length), sub: t('logs.stats.eventsSub') },
  { label: t('logs.stats.warnings'), value: formatNumber(warnings.value.length), sub: warnings.value[0]?.msg ?? t('logs.stats.noWarnings') },
  { label: t('logs.stats.errors'), value: formatNumber(errors.value.length), sub: errors.value[0]?.msg ?? t('logs.stats.noErrors') },
])

function apply() {
  Object.assign(applied, draft)
  pausedAt.value = draft.autoscroll ? null : state.logs[0]?.id ?? null
}

function togglePause() {
  draft.autoscroll = !applied.autoscroll
  apply()
}

function exportTxt() {
  const txt = filtered.value.map((l) => `${l.time} ${l.level.padEnd(5)} [${l.source}] ${l.msg}`).join('\n')
  const a = document.createElement('a')
  a.href = URL.createObjectURL(new Blob([txt], { type: 'text/plain' }))
  a.download = 'frankenmanager.log'
  a.click()
  toast('success', t('logs.exported'), t('logs.exportedLines', { n: formatNumber(filtered.value.length) }, filtered.value.length))
}
</script>

<template>
  <PageHeader :title="t('nav.logs')" :subtitle="t('logs.subtitle')">
    <UButton icon="i-lucide-download" :label="t('logs.export')" size="lg" class="md:w-[205px]" @click="exportTxt" />
  </PageHeader>
  <MiniStats :items="stats" />
  <PageTabs v-model="tab" :items="tabs" />

  <TwoColumns>
    <UCard :ui="{ body: 'px-[18px] py-4 sm:px-[18px] sm:py-4' }">
      <div class="flex items-center gap-2">
        <h2 class="text-sm font-semibold text-highlighted">{{ tab === 'live' ? t('logs.tabs.live') : t('logs.tabs.history') }}</h2>
        <UButton
          v-if="fresh" size="xs" color="primary" variant="soft" icon="i-lucide-arrow-up" class="ml-auto text-[10px]"
          :label="t('logs.fresh', { n: formatNumber(fresh) }, fresh)" :aria-label="t('logs.backToLatest')" @click="page = 1"
        />
        <UButton
          v-if="tab === 'live'" size="xs" variant="ghost" :color="live ? 'primary' : 'neutral'" class="text-[10px] font-semibold" :class="{ 'ml-auto': !fresh }"
          :aria-label="live ? t('logs.pause') : t('logs.resume')" @click="togglePause"
        >
          <span class="size-1.5 rounded-full bg-current" :class="{ 'animate-pulse': live }" />{{ live ? 'LIVE' : 'PAUSE' }}
        </UButton>
      </div>
      <p class="mt-3 text-xs text-muted">{{ applied.service === 'all' ? t('logs.allServices') : applied.service }} · {{ live ? t('logs.autoRefresh') : t('logs.paused') }}</p>
      <div class="mt-4 flex max-h-[410px] flex-col gap-2.5 overflow-auto pr-1">
        <!-- Waiting for the first lines of the stream: skeleton rather than "no logs". -->
        <template v-if="!state.logsReady && !state.logs.length">
          <div v-for="i in 5" :key="i" class="grid grid-cols-[65px_52px_minmax(0,1fr)] items-center gap-y-1.5 text-xs" :aria-busy="true">
            <USkeleton class="h-2.5 w-12" /><USkeleton class="h-2.5 w-8" /><USkeleton class="h-3 w-24" />
            <USkeleton class="col-start-3 h-2.5 w-3/4" />
          </div>
        </template>
        <UEmpty v-else-if="!shown.length" icon="i-lucide-search-x" :description="t('logs.empty')" variant="naked" />
        <div v-for="(l, i) in rows" :key="l.id" class="grid grid-cols-[65px_52px_minmax(0,1fr)] text-xs" :class="{ 'fm-fade-in': i === 0 && live && page === 1 }">
          <time class="pt-0.5 font-mono text-[10px] text-muted">{{ l.time }}</time>
          <span class="pt-0.5 text-[10px] font-semibold" :class="l.level === 'WARN' ? 'text-warning' : l.level === 'ERROR' ? 'text-error' : 'text-primary'">{{ l.level }}</span>
          <span class="font-semibold text-highlighted">{{ l.source }}</span>
          <span class="col-start-3 text-muted">{{ l.msg }}</span>
        </div>
      </div>
      <TablePagination
        v-model:page="page" :total="total" :from="from" :to="to" :page-size="pageSize"
        class="mt-3 border-t border-default" @update:page-size="pager.setPageSize"
      />
    </UCard>

    <SideForm
      :title="t('logs.filters.title')"
      :description="t('logs.filters.description')"
      :hint="t('logs.filters.hint')"
      @submit="apply"
    >
      <UFormField :label="t('logs.filters.service')"><USelect v-model="draft.service" :items="sources" size="lg" class="w-full" /></UFormField>
      <UFormField :label="t('logs.filters.level')">
        <USelect v-model="draft.level" size="lg" class="w-full" :items="levels" />
      </UFormField>
      <UFormField :label="t('logs.filters.period')">
        <USelect v-model="draft.period" size="lg" class="w-full" :items="periods" />
      </UFormField>
      <UFormField :label="t('logs.filters.search')"><UInput v-model="draft.q" :placeholder="t('logs.filters.searchPlaceholder')" size="lg" class="w-full" /></UFormField>
      <USwitch v-model="draft.autoscroll" :label="t('logs.filters.autoscroll')" />
      <UButton type="submit" :label="t('logs.filters.apply')" size="lg" block />
    </SideForm>
  </TwoColumns>

  <InfoBanner>{{ t('logs.banner') }}</InfoBanner>
</template>
