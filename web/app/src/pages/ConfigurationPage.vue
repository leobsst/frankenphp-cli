<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import InfoBanner from '../components/InfoBanner.vue'
import { useI18n } from 'vue-i18n'
import {
  CONFIG_DEFS, CONFIG_SECTIONS, GENERATED_CONFIG, SERVICES, actions, isServiceBusy, state, type ConfigDef, type ConfigSection, type ServiceName,
} from '../composables/useFm'

const { t } = useI18n()

/**
 * Equivalent of `frankenmanager configuration set|reset <KEY> [value]`:
 * each row is edited as a draft, then saved or reset to its default value.
 */
const q = ref('')
const onlyChanged = ref(false)
const draft = reactive<Record<string, string>>({ ...state.config })
const errors = reactive<Record<string, string | undefined>>({})
const revealed = reactive<Record<string, boolean>>({})

/** A value generated at startup (password, Soketi or S3 credential) is not a customization. */
const isGenerated = (d: ConfigDef) => !!d.generated && !!state.config[d.key] && state.config[d.key] === GENERATED_CONFIG[d.key]
const isChanged = (d: ConfigDef) => state.config[d.key] !== d.default && !isGenerated(d)
const isDirty = (d: ConfigDef) => draft[d.key] !== state.config[d.key]

// Sections (submenu): the open one is in the address (?section=), so that a link opens it (Containers › Optional
// services). A search, or "only changed", looks through every section.
const route = useRoute()
const router = useRouter()
const sections = computed(() => CONFIG_SECTIONS.filter((s) => CONFIG_DEFS.some((d) => d.section === s.id)))
const section = computed<ConfigSection>(() => {
  const id = route.query.section
  return sections.value.find((s) => s.id === id)?.id ?? sections.value[0]?.id ?? 'general'
})
function open(id: ConfigSection) {
  q.value = ''
  onlyChanged.value = false
  void router.replace({ query: { ...route.query, section: id } })
}

const matches = (d: ConfigDef) => {
  const s = q.value.trim().toLowerCase()
  return (!s || d.key.toLowerCase().includes(s) || d.help.toLowerCase().includes(s)) && (!onlyChanged.value || isChanged(d))
}
const searching = computed(() => !!q.value.trim() || onlyChanged.value)
/** Settings of a section matching the search (all of them without a search). */
const rowsOf = (id: ConfigSection) => CONFIG_DEFS.filter((d) => d.section === id && matches(d))
const groups = computed(() => (searching.value ? sections.value.map((s) => s.id) : [section.value])
  .map((id) => ({ id, name: t(`labels.configGroups.${id}`), rows: rowsOf(id) }))
  .filter((g) => g.rows.length))
/** Badge of a section in the submenu: its matches while searching, its customised settings otherwise. */
const badge = (id: ConfigSection) => (searching.value ? rowsOf(id).length : CONFIG_DEFS.filter((d) => d.section === id && isChanged(d)).length)
const sectionItems = computed(() => sections.value.map((s) => ({ ...s, label: t(`labels.configGroups.${s.id}`), badge: badge(s.id) })))

/** Switch of an optional service: turned on and off through its service (the container starts or stops). */
const SERVICE_OF: Record<string, ServiceName> = Object.fromEntries((Object.keys(SERVICES) as ServiceName[]).map((n) => [SERVICES[n].key, n]))

const stats = computed(() => [
  { label: t('configuration.stats.settings'), value: CONFIG_DEFS.length, sub: t('configuration.stats.settingsSub') },
  { label: t('configuration.stats.custom'), value: CONFIG_DEFS.filter(isChanged).length, sub: t('configuration.stats.customSub') },
  { label: t('configuration.stats.pending'), value: state.configPending.length, sub: state.configPending.length ? t('configuration.restartRequired') : t('configuration.stats.allApplied') },
])

async function save(d: ConfigDef) {
  const service = SERVICE_OF[d.key]
  if (service) {
    const err = await actions.setService(service, draft[d.key] === 'true')
    errors[d.key] = err ?? undefined
    // The settings are read again after the operation: the switch keeps its new position meanwhile.
    if (!err) state.config[d.key] = draft[d.key]
    return
  }
  const err = await actions.setConfig(d.key, draft[d.key])
  errors[d.key] = err ?? undefined
  if (!err) draft[d.key] = state.config[d.key]
}

async function reset(d: ConfigDef) {
  await actions.resetConfig(d.key)
  draft[d.key] = state.config[d.key]
  errors[d.key] = undefined
}

function cancel(d: ConfigDef) {
  draft[d.key] = state.config[d.key]
  errors[d.key] = undefined
}

const quote = (v: string) => (!v || /\s/.test(v) ? `"${v}"` : v)
const command = (d: ConfigDef) => isDirty(d)
  ? `frankenmanager configuration set ${d.key} ${d.type === 'secret' ? '••••••••' : quote(draft[d.key].trim())}`
  : `frankenmanager configuration reset ${d.key}`

const bool = (key: string) => computed({
  get: () => draft[key] === 'true',
  set: (v: boolean) => { draft[key] = String(v) },
})
const bools = Object.fromEntries(CONFIG_DEFS.filter((d) => d.type === 'bool').map((d) => [d.key, bool(d.key)]))
</script>

<template>
  <PageHeader :title="t('nav.configuration')" :subtitle="t('configuration.subtitle')">
    <UButton
      icon="i-lucide-rotate-cw" :label="t('configuration.restartStack')" size="lg" class="md:w-[205px]"
      :disabled="!state.configPending.length || state.stack !== 'running' || !state.docker"
      @click="actions.restartStack()"
    />
  </PageHeader>
  <MiniStats :items="stats" />

  <UAlert
    v-if="state.configPending.length"
    class="mt-6" color="warning" variant="soft" icon="i-lucide-triangle-alert"
    :title="t('configuration.pendingAlert.title')"
    :description="t('configuration.pendingAlert.description', { keys: state.configPending.join(', ') }, state.configPending.length)"
    :ui="{ root: 'px-5 py-4', title: 'text-xs', description: 'text-xs' }"
  />

  <!-- Submenu of the sections (side column from the tablet up, scrolling row on a phone), then the open section. -->
  <div class="mt-6 grid grid-cols-[minmax(0,1fr)] gap-4 md:grid-cols-[220px_minmax(0,1fr)] md:gap-6">
    <nav class="min-w-0 md:sticky md:top-0 md:self-start" :aria-label="t('configuration.sections')">
      <ul class="-mx-1 flex gap-1 overflow-x-auto px-1 pb-1 md:mx-0 md:flex-col md:overflow-visible md:p-0">
        <li v-for="s in sectionItems" :key="s.id" class="shrink-0">
          <button
            type="button"
            class="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-[13px] whitespace-nowrap transition-colors"
            :class="!searching && section === s.id ? 'bg-primary/10 font-semibold text-primary' : 'text-muted hover:bg-elevated hover:text-highlighted'"
            :aria-current="!searching && section === s.id ? 'page' : undefined"
            @click="open(s.id)"
          >
            <UIcon :name="s.icon" class="size-4 shrink-0" />
            <span class="min-w-0 flex-1 truncate">{{ s.label }}</span>
            <UBadge v-if="s.badge" :label="String(s.badge)" :color="searching ? 'neutral' : 'primary'" variant="soft" size="sm" />
          </button>
        </li>
      </ul>
    </nav>

    <div class="flex min-w-0 flex-col gap-4">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center">
        <UInput v-model="q" icon="i-lucide-search" :placeholder="t('configuration.filter')" size="lg" class="sm:w-80" />
        <USwitch v-model="onlyChanged" :label="t('configuration.onlyChanged')" class="sm:ml-auto" />
      </div>

      <UCard v-for="g in groups" :key="g.id" :ui="{ header: 'px-5 py-3.5 sm:px-5', body: 'p-0 sm:p-0' }">
        <template #header>
          <h2 class="text-[15px] font-semibold text-highlighted">{{ g.name }}</h2>
          <p v-if="g.id === 'services'" class="mt-1 text-[11px] text-muted">{{ t('configuration.servicesHelp') }}</p>
        </template>
        <ul class="divide-y divide-default">
          <li
            v-for="d in g.rows" :key="d.key"
            class="grid gap-3 px-5 py-4 md:grid-cols-[minmax(0,1fr)_minmax(0,300px)] md:gap-6"
          >
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <code class="font-mono text-xs font-semibold text-highlighted">{{ d.key }}</code>
                <UBadge v-if="isChanged(d)" :label="t('configuration.badges.custom')" variant="soft" size="sm" />
                <UBadge v-else-if="isGenerated(d)" :label="t('configuration.badges.generated')" color="neutral" variant="soft" size="sm" />
                <UBadge v-if="d.type === 'secret'" :label="t('configuration.badges.secret')" color="neutral" variant="outline" size="sm" icon="i-lucide-lock" />
                <UBadge v-if="state.configPending.includes(d.key)" :label="t('configuration.restartRequired')" color="warning" variant="soft" size="sm" />
              </div>
              <p class="mt-1.5 text-xs text-muted">{{ d.help }}</p>
              <i18n-t keypath="configuration.default" tag="p" scope="global" class="mt-1 text-[11px] text-dimmed">
                <template #value><span class="font-mono">{{ d.generated ? t('configuration.generatedAtStartup') : d.default || t('configuration.emptyValue') }}</span></template>
              </i18n-t>
            </div>

            <div class="flex min-w-0 flex-col gap-2">
              <UFormField :error="errors[d.key]" :ui="{ error: 'text-[11px]' }">
                <USwitch
                  v-if="d.type === 'bool'" v-model="bools[d.key].value" :disabled="!!SERVICE_OF[d.key] && isServiceBusy(SERVICE_OF[d.key])"
                  :label="draft[d.key] === 'true' ? t('configuration.enabled') : t('configuration.disabled')"
                />
                <USelect v-else-if="d.type === 'select'" v-model="draft[d.key]" :items="d.options" class="w-full" :aria-label="d.key" />
                <UInput
                  v-else-if="d.type === 'secret'" v-model="draft[d.key]" class="w-full font-mono" :placeholder="d.placeholder" :aria-label="d.key"
                  :type="revealed[d.key] ? 'text' : 'password'" autocomplete="off" :ui="{ trailing: 'pe-1' }"
                  @update:model-value="errors[d.key] = undefined" @keydown.enter="save(d)" @keydown.esc="cancel(d)"
                >
                  <template #trailing>
                    <UButton
                      color="neutral" variant="link" size="sm" :icon="revealed[d.key] ? 'i-lucide-eye-off' : 'i-lucide-eye'"
                      :aria-label="revealed[d.key] ? t('configuration.hideKey', { key: d.key }) : t('configuration.showKey', { key: d.key })" :aria-pressed="!!revealed[d.key]"
                      @click="revealed[d.key] = !revealed[d.key]"
                    />
                  </template>
                </UInput>
                <UInput
                  v-else v-model="draft[d.key]" class="w-full font-mono" :placeholder="d.placeholder" :aria-label="d.key"
                  :inputmode="d.type === 'port' ? 'numeric' : undefined"
                  @update:model-value="errors[d.key] = undefined" @keydown.enter="save(d)" @keydown.esc="cancel(d)"
                />
              </UFormField>
              <div v-if="isDirty(d) || isChanged(d) || isGenerated(d)" class="flex flex-wrap items-center gap-2">
                <template v-if="isDirty(d)">
                  <UButton :label="t('common.save')" size="sm" @click="save(d)" />
                  <UButton :label="t('common.cancel')" size="sm" color="neutral" variant="ghost" @click="cancel(d)" />
                </template>
                <UButton
                  v-else icon="i-lucide-rotate-ccw" :label="t('configuration.reset')" size="sm" color="neutral" variant="outline"
                  :aria-label="t('configuration.resetKey', { key: d.key })" @click="reset(d)"
                />
              </div>
              <code v-if="isDirty(d) || isChanged(d) || isGenerated(d)" class="block truncate font-mono text-[10px] text-dimmed" :title="command(d)">$ {{ command(d) }}</code>
            </div>
          </li>
        </ul>
      </UCard>
      <p v-if="!groups.length" class="py-10 text-center text-xs text-muted">{{ t('configuration.empty') }}</p>
    </div>
  </div>

  <InfoBanner>
    <i18n-t keypath="configuration.banner" scope="global">
      <template #cmd><code class="font-mono">configuration list</code></template>
    </i18n-t>
  </InfoBanner>
</template>
