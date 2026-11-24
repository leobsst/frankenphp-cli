<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import TwoColumns from '../components/TwoColumns.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
import TablePagination from '../components/TablePagination.vue'
import { usePagination } from '../composables/usePagination'
import { LIVE_NOISE, state, toast, type LogLevel } from '../composables/useFm'

const LEVELS: LogLevel[] = ['INFO', 'WARN', 'ERROR']
const tab = ref('live')
const tabs = [{ label: 'Flux en direct', value: 'live' }, { label: 'Historique', value: 'history' }]

const draft = reactive({ service: 'all', level: 'INFO' as LogLevel, period: '30', q: '', autoscroll: true })
const applied = reactive({ ...draft })
const pausedAt = ref<number | null>(null) // last visible log when paused

const sources = computed(() => [{ label: 'Tous les services', value: 'all' }, ...[...new Set(state.logs.map((l) => l.source))].sort().map((s) => ({ label: s, value: s }))])
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
  { label: 'Événements', value: state.logs.length.toLocaleString('fr-FR'), sub: 'Depuis l’ouverture' },
  { label: 'Avertissements', value: warnings.value.length, sub: warnings.value[0]?.msg ?? 'Aucun avertissement' },
  { label: 'Erreurs', value: errors.value.length, sub: errors.value[0]?.msg ?? 'Aucune erreur récente' },
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
  toast('success', 'Export terminé', `${filtered.value.length} lignes exportées.`)
}
</script>

<template>
  <PageHeader title="Journaux" subtitle="Suivez en direct les événements de vos services.">
    <UButton icon="i-lucide-download" label="Exporter les journaux" size="lg" class="md:w-[205px]" @click="exportTxt" />
  </PageHeader>
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-16 pb-2.5' }" />

  <TwoColumns>
    <UCard :ui="{ body: 'px-[18px] py-4 sm:px-[18px] sm:py-4' }">
      <div class="flex items-center gap-2">
        <h2 class="text-sm font-semibold text-highlighted">{{ tab === 'live' ? 'Flux en direct' : 'Historique' }}</h2>
        <UButton
          v-if="fresh" size="xs" color="primary" variant="soft" icon="i-lucide-arrow-up" class="ml-auto text-[10px]"
          :label="`${fresh} nouveau${fresh > 1 ? 'x' : ''}`" aria-label="Revenir aux événements les plus récents" @click="page = 1"
        />
        <UButton
          v-if="tab === 'live'" size="xs" variant="ghost" :color="live ? 'primary' : 'neutral'" class="text-[10px] font-semibold" :class="{ 'ml-auto': !fresh }"
          :aria-label="live ? 'Mettre en pause' : 'Reprendre'" @click="togglePause"
        >
          <span class="size-1.5 rounded-full bg-current" :class="{ 'animate-pulse': live }" />{{ live ? 'LIVE' : 'PAUSE' }}
        </UButton>
      </div>
      <p class="mt-3 text-xs text-muted">{{ applied.service === 'all' ? 'Tous les services' : applied.service }} · {{ live ? 'Actualisation automatique' : 'Flux en pause' }}</p>
      <div class="mt-4 flex max-h-[410px] flex-col gap-2.5 overflow-auto pr-1">
        <UEmpty v-if="!shown.length" icon="i-lucide-search-x" description="Aucun événement pour ces filtres." variant="naked" />
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
      title="Filtres du journal"
      description="Affinez le flux sans interrompre vos services."
      hint="Le flux se met à jour automatiquement. Les secrets et mots de passe sont masqués."
      @submit="apply"
    >
      <UFormField label="Service"><USelect v-model="draft.service" :items="sources" size="lg" class="w-full" /></UFormField>
      <UFormField label="Niveau minimum">
        <USelect v-model="draft.level" size="lg" class="w-full" :items="[{ label: 'Information', value: 'INFO' }, { label: 'Avertissement', value: 'WARN' }, { label: 'Erreur', value: 'ERROR' }]" />
      </UFormField>
      <UFormField label="Période">
        <USelect v-model="draft.period" size="lg" class="w-full" :items="[{ label: 'Les 30 dernières minutes', value: '30' }, { label: 'La dernière heure', value: '60' }, { label: 'Les dernières 24 heures', value: '1440' }]" />
      </UFormField>
      <UFormField label="Recherche dans le message"><UInput v-model="draft.q" placeholder="Rechercher un événement…" size="lg" class="w-full" /></UFormField>
      <USwitch v-model="draft.autoscroll" label="Faire défiler automatiquement" />
      <UButton type="submit" label="Appliquer les filtres" size="lg" block />
    </SideForm>
  </TwoColumns>

  <InfoBanner>Le flux affiche les événements de la machine hôte. Export disponible au format texte.</InfoBanner>
</template>
