<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import TwoColumns from '../components/TwoColumns.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
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

const shown = computed(() => state.logs.filter((l) =>
  (applied.service === 'all' || l.source === applied.service) &&
  LEVELS.indexOf(l.level) >= LEVELS.indexOf(applied.level) &&
  (!applied.q || `${l.source} ${l.msg}`.toLowerCase().includes(applied.q.toLowerCase())) &&
  (live.value || pausedAt.value === null || l.id <= pausedAt.value) &&
  (tab.value === 'live' || !LIVE_NOISE.test(l.msg)),
))

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
  const txt = shown.value.map((l) => `${l.time} ${l.level.padEnd(5)} [${l.source}] ${l.msg}`).join('\n')
  const a = document.createElement('a')
  a.href = URL.createObjectURL(new Blob([txt], { type: 'text/plain' }))
  a.download = 'frankenmanager.log'
  a.click()
  toast('success', 'Export terminé', `${shown.value.length} lignes exportées.`)
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
          v-if="tab === 'live'" size="xs" variant="ghost" :color="live ? 'primary' : 'neutral'" class="ml-auto text-[10px] font-semibold"
          :aria-label="live ? 'Mettre en pause' : 'Reprendre'" @click="togglePause"
        >
          <span class="size-1.5 rounded-full bg-current" :class="{ 'animate-pulse': live }" />{{ live ? 'LIVE' : 'PAUSE' }}
        </UButton>
      </div>
      <p class="mt-3 text-xs text-muted">{{ applied.service === 'all' ? 'Tous les services' : applied.service }} · {{ live ? 'Actualisation automatique' : 'Flux en pause' }}</p>
      <div class="mt-4 flex max-h-[410px] flex-col gap-2.5 overflow-auto pr-1">
        <UEmpty v-if="!shown.length" icon="i-lucide-search-x" description="Aucun événement pour ces filtres." variant="naked" />
        <div v-for="(l, i) in shown.slice(0, 120)" :key="l.id" class="grid grid-cols-[65px_52px_minmax(0,1fr)] text-xs" :class="{ 'fm-fade-in': i === 0 && live }">
          <time class="pt-0.5 font-mono text-[10px] text-muted">{{ l.time }}</time>
          <span class="pt-0.5 text-[10px] font-semibold" :class="l.level === 'WARN' ? 'text-warning' : l.level === 'ERROR' ? 'text-error' : 'text-primary'">{{ l.level }}</span>
          <span class="font-semibold text-highlighted">{{ l.source }}</span>
          <span class="col-start-3 text-muted">{{ l.msg }}</span>
        </div>
      </div>
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
