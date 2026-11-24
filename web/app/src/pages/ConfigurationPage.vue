<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import InfoBanner from '../components/InfoBanner.vue'
import { CONFIG_DEFS, GENERATED_CONFIG, actions, state, type ConfigDef } from '../composables/useFm'

/**
 * Équivalent de `frankenmanager configuration set|reset <KEY> [value]` :
 * chaque ligne se modifie en brouillon, puis s'enregistre ou revient à sa valeur par défaut.
 */
const q = ref('')
const onlyChanged = ref(false)
const draft = reactive<Record<string, string>>({ ...state.config })
const errors = reactive<Record<string, string | undefined>>({})
const revealed = reactive<Record<string, boolean>>({})

/** Une valeur générée au démarrage (mot de passe, identifiant Soketi ou S3) n'est pas une personnalisation. */
const isGenerated = (d: ConfigDef) => !!d.generated && !!state.config[d.key] && state.config[d.key] === GENERATED_CONFIG[d.key]
const isChanged = (d: ConfigDef) => state.config[d.key] !== d.default && !isGenerated(d)
const isDirty = (d: ConfigDef) => draft[d.key] !== state.config[d.key]

const groups = computed(() => {
  const s = q.value.trim().toLowerCase()
  const rows = CONFIG_DEFS.filter((d) =>
    (!s || d.key.toLowerCase().includes(s) || d.help.toLowerCase().includes(s)) && (!onlyChanged.value || isChanged(d)))
  return [...new Set(rows.map((d) => d.group))].map((g) => ({ name: g, rows: rows.filter((d) => d.group === g) }))
})

const stats = computed(() => [
  { label: 'Paramètres', value: CONFIG_DEFS.length, sub: 'Fichier .env' },
  { label: 'Personnalisés', value: CONFIG_DEFS.filter(isChanged).length, sub: 'Différents du défaut' },
  { label: 'En attente', value: state.configPending.length, sub: state.configPending.length ? 'Redémarrage requis' : 'Tout est appliqué' },
])

async function save(d: ConfigDef) {
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
  <PageHeader title="Configuration" subtitle="Paramètres de frankenmanager, enregistrés dans le fichier .env du dossier de données.">
    <UButton
      icon="i-lucide-rotate-cw" label="Redémarrer la stack" size="lg" class="md:w-[205px]"
      :disabled="!state.configPending.length || state.stack !== 'running' || !state.docker"
      @click="actions.restartStack()"
    />
  </PageHeader>
  <MiniStats :items="stats" />

  <UAlert
    v-if="state.configPending.length"
    class="mt-6" color="warning" variant="soft" icon="i-lucide-triangle-alert"
    title="Redémarrage nécessaire"
    :description="`${state.configPending.join(', ')} ${state.configPending.length > 1 ? 'seront appliqués' : 'sera appliqué'} au prochain démarrage de la stack.`"
    :ui="{ root: 'px-5 py-4', title: 'text-xs', description: 'text-xs' }"
  />

  <div class="mt-6 flex flex-col gap-3 sm:flex-row sm:items-center">
    <UInput v-model="q" icon="i-lucide-search" placeholder="Filtrer par clé ou description…" size="lg" class="sm:w-80" />
    <USwitch v-model="onlyChanged" label="Personnalisés uniquement" class="sm:ml-auto" />
  </div>

  <div class="mt-4 flex flex-col gap-6">
    <UCard v-for="g in groups" :key="g.name" :ui="{ header: 'px-5 py-3.5 sm:px-5', body: 'p-0 sm:p-0' }">
      <template #header>
        <h2 class="text-[15px] font-semibold text-highlighted">{{ g.name }}</h2>
      </template>
      <ul class="divide-y divide-default">
        <li
          v-for="d in g.rows" :key="d.key"
          class="grid gap-3 px-5 py-4 md:grid-cols-[minmax(0,1fr)_minmax(0,300px)] md:gap-6"
        >
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <code class="font-mono text-xs font-semibold text-highlighted">{{ d.key }}</code>
              <UBadge v-if="isChanged(d)" label="Personnalisé" variant="soft" size="sm" />
              <UBadge v-else-if="isGenerated(d)" label="Généré" color="neutral" variant="soft" size="sm" />
              <UBadge v-if="d.type === 'secret'" label="Secret" color="neutral" variant="outline" size="sm" icon="i-lucide-lock" />
              <UBadge v-if="state.configPending.includes(d.key)" label="Redémarrage requis" color="warning" variant="soft" size="sm" />
            </div>
            <p class="mt-1.5 text-xs text-muted">{{ d.help }}</p>
            <p class="mt-1 text-[11px] text-dimmed">Défaut : <span class="font-mono">{{ d.generated ? 'généré au démarrage' : d.default || '(vide)' }}</span></p>
          </div>

          <div class="flex min-w-0 flex-col gap-2">
            <UFormField :error="errors[d.key]" :ui="{ error: 'text-[11px]' }">
              <USwitch v-if="d.type === 'bool'" v-model="bools[d.key].value" :label="draft[d.key] === 'true' ? 'Activé' : 'Désactivé'" />
              <USelect v-else-if="d.type === 'select'" v-model="draft[d.key]" :items="d.options" class="w-full" :aria-label="d.key" />
              <UInput
                v-else-if="d.type === 'secret'" v-model="draft[d.key]" class="w-full font-mono" :placeholder="d.placeholder" :aria-label="d.key"
                :type="revealed[d.key] ? 'text' : 'password'" autocomplete="off" :ui="{ trailing: 'pe-1' }"
                @update:model-value="errors[d.key] = undefined" @keydown.enter="save(d)" @keydown.esc="cancel(d)"
              >
                <template #trailing>
                  <UButton
                    color="neutral" variant="link" size="sm" :icon="revealed[d.key] ? 'i-lucide-eye-off' : 'i-lucide-eye'"
                    :aria-label="revealed[d.key] ? `Masquer ${d.key}` : `Afficher ${d.key}`" :aria-pressed="!!revealed[d.key]"
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
                <UButton label="Enregistrer" size="sm" @click="save(d)" />
                <UButton label="Annuler" size="sm" color="neutral" variant="ghost" @click="cancel(d)" />
              </template>
              <UButton
                v-else icon="i-lucide-rotate-ccw" label="Réinitialiser" size="sm" color="neutral" variant="outline"
                :aria-label="`Réinitialiser ${d.key}`" @click="reset(d)"
              />
            </div>
            <code v-if="isDirty(d) || isChanged(d) || isGenerated(d)" class="block truncate font-mono text-[10px] text-dimmed" :title="command(d)">$ {{ command(d) }}</code>
          </div>
        </li>
      </ul>
    </UCard>
    <p v-if="!groups.length" class="py-10 text-center text-xs text-muted">Aucun paramètre ne correspond.</p>
  </div>

  <InfoBanner>Les secrets (mots de passe, app secret Soketi, secrets S3) sont masqués comme dans <code class="font-mono">configuration list</code>. Réinitialiser une valeur générée en génère une nouvelle au prochain démarrage.</InfoBanner>
</template>
