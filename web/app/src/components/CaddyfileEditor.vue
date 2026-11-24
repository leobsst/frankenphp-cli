<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { actions, caddyfilePathOf, containerFor, defaultCaddyfile, isCaddyfileCustom, loadCaddyfile, siteCaddyfile, state, toast } from '../composables/useFm'
import { hasWebsocket, toggleWebsocket } from '../composables/caddyfile'

// Editor for a site's Caddyfile: caddy/sites/php-<version>/<domain>_Caddyfile for a PHP project,
// caddy/sites/static/<domain>_Caddyfile for a static host (v0.9.0).
const props = defineProps<{ domain: string }>()
const open = defineModel<boolean>('open', { default: false })

const text = ref('')
const error = ref<string | null>(null)
const saving = ref(false)
const checking = ref(false)
/** Result of the last check by Caddy: null until the text has been checked. */
const checked = ref<boolean | null>(null)
const confirmReset = ref(false)
const editor = ref<HTMLTextAreaElement>()

const site = computed(() => state.domains.find((d) => d.name === props.domain))
const isStatic = computed(() => site.value?.kind === 'static')
const path = computed(() => caddyfilePathOf(props.domain))
const server = computed(() => (site.value ? containerFor(site.value)?.id : undefined) ?? '—')
const saved = computed(() => siteCaddyfile(props.domain))
const dirty = computed(() => text.value !== saved.value)
const isTemplate = computed(() => text.value === defaultCaddyfile(props.domain))
const websocket = computed(() => hasWebsocket(text.value))
const lines = computed(() => text.value.split('\n').length)

watch(open, async (o) => {
  if (!o) return
  // The file, its template and its path come from the daemon (`GET /api/v1/hosts/{name}/caddyfile`).
  text.value = ''
  try { await loadCaddyfile(props.domain) } catch (e) { toast('error', 'Lecture impossible', e instanceof Error ? e.message : String(e)) }
  text.value = saved.value
  error.value = null
  checked.value = null
  confirmReset.value = false
})

function onInput() {
  error.value = null
  checked.value = null
  confirmReset.value = false
}

/** `POST /api/v1/hosts/{name}/caddyfile/validate`: checked by the container's Caddy, nothing is saved. */
async function check() {
  checking.value = true
  const invalid = await actions.checkCaddyfile(props.domain, text.value)
  checking.value = false
  checked.value = !invalid
  error.value = invalid?.message ?? null
  if (invalid) focusLine(invalid.line)
}

/** Tab inserts a tab character (the CLI's Caddyfile indentation) instead of leaving the field. */
function onTab(e: KeyboardEvent) {
  const el = e.target as HTMLTextAreaElement
  const { selectionStart: a, selectionEnd: b } = el
  text.value = text.value.slice(0, a) + '\t' + text.value.slice(b)
  requestAnimationFrame(() => el.setSelectionRange(a + 1, a + 1))
}

function setWebsocket(on: boolean) {
  const next = toggleWebsocket(text.value, on)
  if (next === null) {
    error.value = 'Les blocs WebSocket du modèle ont été modifiés : éditez le fichier à la main (encode, @notWebsocket, @websocket).'
    return
  }
  text.value = next
  error.value = null
}

/** Moves the cursor to the faulty line so it can be found in a long file. */
function focusLine(line: number) {
  const el = editor.value
  if (!el) return
  const start = text.value.split('\n').slice(0, line - 1).reduce((n, l) => n + l.length + 1, 0)
  el.focus()
  el.setSelectionRange(start, start + (text.value.split('\n')[line - 1]?.length ?? 0))
}

async function save() {
  // The daemon has the container's Caddy check the file before saving it.
  saving.value = true
  error.value = await actions.saveCaddyfile(props.domain, text.value)
  saving.value = false
  if (!error.value) open.value = false
}

async function reset() {
  if (!confirmReset.value) { confirmReset.value = true; return }
  open.value = false
  await actions.resetCaddyfile(props.domain)
}
</script>

<template>
  <USlideover
    v-model:open="open" side="right" :title="`Caddyfile · ${domain}`" :description="path"
    :ui="{ content: 'w-full sm:max-w-[760px]', body: 'flex flex-col gap-4 sm:p-5', description: 'font-mono text-[11px] truncate', footer: 'flex-wrap gap-2' }"
  >
    <template #body>
      <div class="flex flex-wrap items-center gap-2">
        <UBadge v-if="dirty" label="Non enregistré" color="warning" variant="soft" />
        <UBadge v-else-if="isCaddyfileCustom(domain)" label="Personnalisé" variant="soft" />
        <UBadge v-else label="Généré par le modèle" color="neutral" variant="soft" />
        <UBadge :label="isStatic ? `Statique${site?.spa ? ' · SPA' : ''}` : `PHP ${site?.php}`" color="neutral" variant="outline" />
        <USwitch
          v-if="!isStatic" class="ml-auto" :model-value="websocket" label="WebSocket (/app*)"
          description="Proxy vers 127.0.0.1:7006, compression désactivée sur /app*"
          @update:model-value="setWebsocket"
        />
      </div>

      <UAlert v-if="error" color="error" variant="soft" icon="i-lucide-circle-alert" :description="error" :ui="{ description: 'text-xs' }" />
      <UAlert
        v-else-if="checked" color="success" variant="soft" icon="i-lucide-circle-check"
        :description="`Caddy accepte ce fichier (${isStatic ? 'caddy adapt dans le reverse proxy' : `frankenphp adapt dans ${server}`}).`"
        :ui="{ description: 'text-xs' }"
      />

      <div class="relative min-h-[420px] flex-1 overflow-hidden rounded-lg bg-sage-950 ring-1 ring-default">
        <textarea
          ref="editor" v-model="text" spellcheck="false" autocapitalize="off" autocomplete="off" wrap="off"
          aria-label="Contenu du Caddyfile"
          class="absolute inset-0 size-full resize-none bg-transparent p-4 font-mono text-xs leading-[19px] text-sage-200 outline-none [tab-size:4]"
          @input="onInput" @keydown.tab.exact.prevent="onTab" @keydown.meta.s.prevent="save" @keydown.ctrl.s.prevent="save"
        />
      </div>
      <p class="text-[11px] text-muted">
        {{ lines }} lignes · Tab indente · ⌘S enregistre. Le fichier est vérifié par le Caddy du conteneur avant d’être enregistré, puis s’applique au rechargement du conteneur
        <span class="font-mono">{{ server }}</span> ;
        <template v-if="isStatic">servi en HTTPS par le reverse proxy, ce fichier garde sa ligne <span class="font-mono">tls</span>.</template>
        <template v-else>les autres domaines de cette version sont brièvement indisponibles.</template>
      </p>
    </template>

    <template #footer="{ close }">
      <UButton
        :color="confirmReset ? 'error' : 'neutral'" variant="ghost" icon="i-lucide-rotate-ccw"
        :label="confirmReset ? 'Confirmer : écraser avec le modèle' : 'Rétablir le modèle'"
        :disabled="isTemplate && !isCaddyfileCustom(domain)" @click="reset"
      />
      <code class="hidden truncate font-mono text-[10px] text-dimmed lg:block">$ frankenmanager restart --caddy</code>
      <!-- On phones, the buttons wrap and "Enregistrer" takes the full width. -->
      <div class="ml-auto flex flex-wrap justify-end gap-2 max-sm:w-full">
        <UButton color="neutral" variant="outline" label="Annuler" @click="close" />
        <UButton color="neutral" variant="outline" icon="i-lucide-shield-check" label="Vérifier" :loading="checking" @click="check" />
        <UButton icon="i-lucide-save" label="Enregistrer et recharger" class="max-sm:w-full max-sm:justify-center" :loading="saving" :disabled="!dirty" @click="save" />
      </div>
    </template>
  </USlideover>
</template>
