<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import PageHeader from '../components/PageHeader.vue'
import ColorSwatches from '../components/ColorSwatches.vue'
import { DEFAULT_PREFS, actions, state, toast, type Prefs } from '../composables/useFm'
import { draftPrefs as draft } from '../composables/usePrefs'
import { formatDate, formatTime, LANGUAGES, systemLanguage, type LanguagePref } from '../i18n'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const PRIMARIES = ['#2563EB', '#197453', '#7C3AED', '#E07A32', '#DB2777', '#334155']
const ACCENTS = ['#8B5CF6', '#3B82F6', '#0D9488', '#F59E0B', '#EC4899', '#197453']
const BARS = [13, 18, 11, 22, 27, 18, 30, 24, 32, 21, 26, 18]
const HEX = /^#[0-9a-fA-F]{6}$/

const THEMES: { id: Prefs['theme']; nav: string; bg: string; title: string; card: string }[] = [
  { id: 'light', nav: '#ffffff', bg: 'var(--color-sage-50)', title: 'var(--color-sage-900)', card: '#ffffff' },
  { id: 'dark', nav: 'var(--fm-dark-bg)', bg: 'var(--color-sage-950)', title: 'var(--fm-dark-text)', card: 'var(--fm-dark-elevated)' },
  { id: 'system', nav: 'linear-gradient(90deg,#fff 50%,var(--fm-dark-bg) 50%)', bg: 'linear-gradient(90deg,var(--color-sage-50) 50%,var(--color-sage-950) 50%)', title: 'var(--color-sage-400)', card: 'linear-gradient(90deg,#fff 50%,var(--fm-dark-elevated) 50%)' },
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
  toast('info', t('settings.saved'), t('settings.savedHelp'))
}
const revert = () => { draft.value = { ...state.prefs } }
const reset = () => { draft.value = { ...DEFAULT_PREFS } }

const toggles = ['autoRefresh', 'notifications', 'reduceMotion'] as const

// Language names stay in their own language: someone lost in the other one still finds theirs.
const languages = computed<{ label: string; value: LanguagePref }[]>(() => [
  { label: t('settings.languageSystem', { language: t(`languages.${systemLanguage.value}`) }), value: 'system' },
  ...LANGUAGES.map((l) => ({ label: t(`languages.${l}`), value: l })),
])
</script>

<template>
  <PageHeader :title="t('settings.title')" :subtitle="t('settings.subtitle')">
    <UButton :label="t('common.save')" size="lg" class="md:w-[142px]" :disabled="!dirty" @click="save" />
  </PageHeader>

  <div class="mt-7 grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_376px] *:min-w-0">
    <UCard :ui="{ body: 'p-5 sm:p-5' }">
      <h2 class="text-[17px] font-semibold text-highlighted">{{ t('settings.appearance') }}</h2>
      <p class="mt-1.5 text-xs text-muted">{{ t('settings.appearanceHelp') }}</p>

      <h3 class="mt-6 text-[13px] font-semibold text-highlighted">{{ t('settings.theme') }}</h3>
      <div class="mt-3.5 grid gap-3 sm:grid-cols-3">
        <button
          v-for="theme in THEMES" :key="theme.id" type="button"
          class="relative rounded-lg border bg-default p-2.5 text-left transition-colors"
          :class="draft.theme === theme.id ? 'border-primary ring-1 ring-primary' : 'border-default hover:border-accented'"
          @click="draft.theme = theme.id"
        >
          <div class="flex h-[61px] overflow-hidden rounded-md border border-default" :style="{ background: theme.bg }">
            <i class="block w-[18%]" :style="{ background: theme.nav }" />
            <div class="flex flex-1 flex-col gap-2 px-3.5 py-2.5">
              <b class="block h-[5px] w-3/5 rounded-full" :style="{ background: theme.title }" />
              <em class="block flex-1 rounded" :style="{ background: theme.card }" />
            </div>
          </div>
          <span class="mt-3 block text-xs font-medium text-highlighted">{{ t(`settings.themes.${theme.id}`) }}</span>
          <UIcon v-if="draft.theme === theme.id" name="i-lucide-check" class="absolute right-3 bottom-3 size-4 text-primary" />
        </button>
      </div>

      <h3 class="mt-6 text-[13px] font-semibold text-highlighted">{{ t('settings.primary') }}</h3>
      <ColorSwatches v-model="draft.primary" :presets="PRIMARIES" :label="t('settings.primary')" class="mt-3" />
      <UInput v-model="hexPrimary" size="lg" class="mt-4 w-full font-mono" :color="HEX.test(hexPrimary) ? 'primary' : 'error'" :highlight="!HEX.test(hexPrimary)" :aria-label="t('settings.primary')">
        <template #leading><span class="size-[18px] rounded" :style="{ background: draft.primary }" /></template>
      </UInput>
      <p class="mt-2 text-[11px] text-muted">{{ t('settings.primaryHelp') }}</p>

      <h3 class="mt-6 text-[13px] font-semibold text-highlighted">{{ t('settings.accent') }}</h3>
      <ColorSwatches v-model="draft.accent" :presets="ACCENTS" :label="t('settings.accent')" class="mt-3" />
      <UInput v-model="hexAccent" size="lg" class="mt-4 w-full font-mono" :color="HEX.test(hexAccent) ? 'primary' : 'error'" :highlight="!HEX.test(hexAccent)" :aria-label="t('settings.accent')">
        <template #leading><span class="size-[18px] rounded" :style="{ background: draft.accent }" /></template>
      </UInput>
      <p class="mt-2 text-[11px] text-muted">{{ t('settings.accentHelp') }}</p>
    </UCard>

    <div class="flex flex-col gap-6">
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <h2 class="text-[17px] font-semibold text-highlighted">{{ t('settings.languageTitle') }}</h2>
        <UFormField :label="t('settings.language')" class="mt-4">
          <USelect v-model="draft.language" size="lg" class="w-full" :items="languages" />
        </UFormField>
        <p class="mt-3.5 flex gap-1.5 text-[11px] text-muted">
          <UIcon name="i-lucide-info" class="mt-px size-3.5 shrink-0" />
          <span>{{ t('settings.languageHelp') }}</span>
        </p>
        <p class="mt-4 text-[11px] text-muted">
          {{ t('settings.formats', { date: formatDate(now), time: formatTime(now, { hour: '2-digit', minute: '2-digit' }) }) }}
        </p>
      </UCard>
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <h2 class="text-[17px] font-semibold text-highlighted">{{ t('settings.dashboard') }}</h2>
        <div v-for="key in toggles" :key="key" class="mt-5 flex items-center gap-4">
          <div class="flex-1">
            <p class="text-xs font-medium text-highlighted">{{ t(`settings.toggles.${key}`) }}</p>
            <p class="mt-1 text-[11px] text-muted">{{ t(`settings.toggles.${key}Help`) }}</p>
          </div>
          <USwitch v-model="draft[key]" :aria-label="t(`settings.toggles.${key}`)" />
        </div>
        <p class="mt-8 text-[10px] text-muted">{{ t('settings.localOnly') }}</p>
      </UCard>
    </div>
  </div>

  <UCard class="mt-6" :ui="{ body: 'p-5 sm:p-5' }">
    <h2 class="text-[15px] font-semibold text-highlighted">{{ t('settings.preview') }}</h2>
    <p class="mt-1.5 text-xs text-muted">{{ t('settings.previewHelp') }}</p>
    <div class="mt-7 flex flex-wrap items-center gap-5">
      <UButton :label="t('settings.previewButton')" size="lg" class="w-[196px]" />
      <UBadge :label="t('settings.previewBadge')" variant="soft" size="lg" />
      <div class="flex h-[52px] min-w-[260px] flex-1 items-end justify-between rounded-lg border border-default px-3 pt-2 pb-2.5">
        <span class="self-center text-[11px] font-semibold text-muted">{{ t('settings.previewCpu') }}</span>
        <span v-for="(h, i) in BARS" :key="i" class="w-2 rounded-xs bg-info transition-colors" :style="{ height: `${h}px` }" />
      </div>
    </div>
  </UCard>

  <UCard class="mt-6" :ui="{ body: 'flex flex-wrap items-center gap-3 px-5 py-3 sm:px-5 sm:py-3' }">
    <p class="mr-auto text-xs text-muted">
      {{ dirty ? t('settings.unsaved') : t('settings.applied') }}
    </p>
    <UButton color="neutral" variant="outline" :label="t('settings.revert')" size="lg" :disabled="!dirty" @click="revert" />
    <UButton color="neutral" variant="outline" :label="t('settings.reset')" size="lg" @click="reset" />
    <UButton :label="t('common.save')" size="lg" class="w-[142px] justify-center" :disabled="!dirty" @click="save" />
  </UCard>
</template>
