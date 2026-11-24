<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import PageHeader from '../components/PageHeader.vue'
import ColorSwatches from '../components/ColorSwatches.vue'
import { DEFAULT_PREFS, actions, state, toast, type Prefs } from '../composables/useFm'
import { draftPrefs as draft } from '../composables/usePrefs'

const PRIMARIES = ['#2563EB', '#197453', '#7C3AED', '#E07A32', '#DB2777', '#334155']
const ACCENTS = ['#8B5CF6', '#3B82F6', '#0D9488', '#F59E0B', '#EC4899', '#197453']
const BARS = [13, 18, 11, 22, 27, 18, 30, 24, 32, 21, 26, 18]
const HEX = /^#[0-9a-fA-F]{6}$/

const THEMES: { id: Prefs['theme']; label: string; nav: string; bg: string; title: string; card: string }[] = [
  { id: 'light', label: 'Clair', nav: '#ffffff', bg: 'var(--color-sage-50)', title: 'var(--color-sage-900)', card: '#ffffff' },
  { id: 'dark', label: 'Sombre', nav: 'var(--fm-dark-bg)', bg: 'var(--color-sage-950)', title: 'var(--fm-dark-text)', card: 'var(--fm-dark-elevated)' },
  { id: 'system', label: 'Système', nav: 'linear-gradient(90deg,#fff 50%,var(--fm-dark-bg) 50%)', bg: 'linear-gradient(90deg,var(--color-sage-50) 50%,var(--color-sage-950) 50%)', title: 'var(--color-sage-400)', card: 'linear-gradient(90deg,#fff 50%,var(--fm-dark-elevated) 50%)' },
]

// Free hexadecimal input: applied only when valid.
const hexPrimary = ref(draft.value.primary)
const hexAccent = ref(draft.value.accent)
watch(() => draft.value.primary, (v) => { hexPrimary.value = v })
watch(() => draft.value.accent, (v) => { hexAccent.value = v })
watch(hexPrimary, (v) => { if (HEX.test(v)) draft.value.primary = v })
watch(hexAccent, (v) => { if (HEX.test(v)) draft.value.accent = v })

const dirty = computed(() => JSON.stringify(state.prefs) !== JSON.stringify(draft.value))
const now = new Date()

function save() {
  actions.savePrefs(draft.value)
  toast('info', 'Configuration enregistrée', 'Les préférences du dashboard sont appliquées.')
}
const revert = () => { draft.value = { ...state.prefs } }
const reset = () => { draft.value = { ...DEFAULT_PREFS } }

const toggles = [
  { key: 'autoRefresh', label: 'Actualisation automatique', help: 'Mettre à jour toutes les 5 secondes.' },
  { key: 'notifications', label: 'Notifications', help: 'Afficher les résultats des opérations.' },
  { key: 'reduceMotion', label: 'Réduire les animations', help: 'Privilégier les transitions discrètes.' },
] as const
</script>

<template>
  <PageHeader title="Préférences du dashboard" subtitle="Une interface à votre image, sur cette machine uniquement.">
    <UButton label="Enregistrer" size="lg" class="md:w-[142px]" :disabled="!dirty" @click="save" />
  </PageHeader>

  <div class="mt-7 grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_376px] *:min-w-0">
    <UCard :ui="{ body: 'p-5 sm:p-5' }">
      <h2 class="text-[17px] font-semibold text-highlighted">Apparence</h2>
      <p class="mt-1.5 text-xs text-muted">Choisissez votre thème et vos couleurs.</p>

      <h3 class="mt-6 text-[13px] font-semibold text-highlighted">Thème de l’interface</h3>
      <div class="mt-3.5 grid gap-3 sm:grid-cols-3">
        <button
          v-for="t in THEMES" :key="t.id" type="button"
          class="relative rounded-lg border bg-default p-2.5 text-left transition-colors"
          :class="draft.theme === t.id ? 'border-primary ring-1 ring-primary' : 'border-default hover:border-accented'"
          @click="draft.theme = t.id"
        >
          <div class="flex h-[61px] overflow-hidden rounded-md border border-default" :style="{ background: t.bg }">
            <i class="block w-[18%]" :style="{ background: t.nav }" />
            <div class="flex flex-1 flex-col gap-2 px-3.5 py-2.5">
              <b class="block h-[5px] w-3/5 rounded-full" :style="{ background: t.title }" />
              <em class="block flex-1 rounded" :style="{ background: t.card }" />
            </div>
          </div>
          <span class="mt-3 block text-xs font-medium text-highlighted">{{ t.label }}</span>
          <UIcon v-if="draft.theme === t.id" name="i-lucide-check" class="absolute right-3 bottom-3 size-4 text-primary" />
        </button>
      </div>

      <h3 class="mt-6 text-[13px] font-semibold text-highlighted">Couleur primaire</h3>
      <ColorSwatches v-model="draft.primary" :presets="PRIMARIES" label="Couleur primaire" class="mt-3" />
      <UInput v-model="hexPrimary" size="lg" class="mt-4 w-full font-mono" :color="HEX.test(hexPrimary) ? 'primary' : 'error'" :highlight="!HEX.test(hexPrimary)" aria-label="Couleur primaire">
        <template #leading><span class="size-[18px] rounded" :style="{ background: draft.primary }" /></template>
      </UInput>
      <p class="mt-2 text-[11px] text-muted">Boutons principaux et navigation sélectionnée.</p>

      <h3 class="mt-6 text-[13px] font-semibold text-highlighted">Couleur d’accentuation</h3>
      <ColorSwatches v-model="draft.accent" :presets="ACCENTS" label="Couleur d’accentuation" class="mt-3" />
      <UInput v-model="hexAccent" size="lg" class="mt-4 w-full font-mono" :color="HEX.test(hexAccent) ? 'primary' : 'error'" :highlight="!HEX.test(hexAccent)" aria-label="Couleur d’accentuation">
        <template #leading><span class="size-[18px] rounded" :style="{ background: draft.accent }" /></template>
      </UInput>
      <p class="mt-2 text-[11px] text-muted">Graphiques, repères et éléments mis en évidence.</p>
    </UCard>

    <div class="flex flex-col gap-6">
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <h2 class="text-[17px] font-semibold text-highlighted">Langue et formats</h2>
        <UFormField label="Langue de l’interface" class="mt-4">
          <USelect v-model="draft.lang" size="lg" class="w-full" :items="[{ label: 'Français', value: 'fr' }, { label: 'English', value: 'en' }]" />
        </UFormField>
        <p class="mt-3.5 flex gap-1.5 text-[11px] text-muted">
          <UIcon name="i-lucide-info" class="mt-px size-3.5 shrink-0" />
          <span>Simulé : le choix est enregistré, mais l’interface reste en français pour l’instant.</span>
        </p>
        <p class="mt-4 text-[11px] text-muted">
          Dates : {{ now.toLocaleDateString('fr-FR') }} · Heure : {{ now.toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit' }) }}
        </p>
      </UCard>
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <h2 class="text-[17px] font-semibold text-highlighted">Dashboard</h2>
        <div v-for="t in toggles" :key="t.key" class="mt-5 flex items-center gap-4">
          <div class="flex-1">
            <p class="text-xs font-medium text-highlighted">{{ t.label }}</p>
            <p class="mt-1 text-[11px] text-muted">{{ t.help }}</p>
          </div>
          <USwitch v-model="draft[t.key]" :aria-label="t.label" />
        </div>
        <p class="mt-8 text-[10px] text-muted">Réglages locaux · Aucun compte nécessaire</p>
      </UCard>
    </div>
  </div>

  <UCard class="mt-6" :ui="{ body: 'p-5 sm:p-5' }">
    <h2 class="text-[15px] font-semibold text-highlighted">Aperçu en direct</h2>
    <p class="mt-1.5 text-xs text-muted">Vos préférences ne modifient pas la configuration des services.</p>
    <div class="mt-7 flex flex-wrap items-center gap-5">
      <UButton label="Action principale" size="lg" class="w-[196px]" />
      <UBadge label="État sélectionné" variant="soft" size="lg" />
      <div class="flex h-[52px] min-w-[260px] flex-1 items-end justify-between rounded-lg border border-default px-3 pt-2 pb-2.5">
        <span class="self-center text-[11px] font-semibold text-muted">CPU</span>
        <span v-for="(h, i) in BARS" :key="i" class="w-2 rounded-xs bg-info transition-colors" :style="{ height: `${h}px` }" />
      </div>
    </div>
  </UCard>

  <UCard class="mt-6" :ui="{ body: 'flex flex-wrap items-center gap-3 px-5 py-3 sm:px-5 sm:py-3' }">
    <p class="mr-auto text-xs text-muted">
      {{ dirty ? 'Modifications non enregistrées · aperçu appliqué à ce dashboard.' : 'Les changements sont appliqués à ce dashboard.' }}
    </p>
    <UButton color="neutral" variant="outline" label="Rétablir" size="lg" :disabled="!dirty" @click="revert" />
    <UButton color="neutral" variant="outline" label="Par défaut" size="lg" @click="reset" />
    <UButton label="Enregistrer" size="lg" class="w-[142px] justify-center" :disabled="!dirty" @click="save" />
  </UCard>
</template>
