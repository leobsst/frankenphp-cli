<script setup lang="ts">
import { computed } from 'vue'
import { checkUpdate, state, toast, updateAvailable, updates } from '../composables/useFm'
import { releaseBlocks } from '../lib/markdown'

// `frankenmanager update --check`, then the update itself in a terminal of the host: the daemon runs as the user and
// cannot replace a binary installed in a system directory (administrator rights).
const open = defineModel<boolean>('open', { default: false })

const info = computed(() => updates.info)
const version = computed(() => info.value?.version || state.app.version)
const releases = computed(() => (info.value?.releases ?? []).map((r) => ({ ...r, blocks: releaseBlocks(r.notes) })))
const command = computed(() => info.value?.command ?? 'sudo frankenmanager update')
const host = computed(() => state.connection.host || 'la machine hôte')

const date = (iso: string) => (iso ? new Date(iso).toLocaleDateString('fr-FR', { day: 'numeric', month: 'long', year: 'numeric' }) : '')
const checkedAt = computed(() => (info.value?.checked_at ? new Date(info.value.checked_at).toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit' }) : ''))

function copy() {
  navigator.clipboard?.writeText(command.value)
  toast('info', 'Commande copiée', command.value)
}
</script>

<template>
  <UModal v-model:open="open" title="Mise à jour de FrankenManager" description="Dernière version publiée sur GitHub." :ui="{ content: 'sm:max-w-[520px]', footer: 'gap-2' }">
    <template #body>
      <div class="flex items-center gap-3 rounded-lg border border-default bg-muted p-3 text-xs">
        <div>
          <p class="text-muted">Version installée</p>
          <p class="font-mono text-sm font-semibold text-highlighted">v{{ version || '…' }}</p>
        </div>
        <UIcon name="i-lucide-arrow-right" class="size-4 text-muted" />
        <div>
          <p class="text-muted">Dernière version</p>
          <p class="font-mono text-sm font-semibold text-highlighted">{{ info?.latest ? `v${info.latest}` : '?' }}</p>
        </div>
        <UBadge v-if="!info" label="Inconnue" color="neutral" variant="soft" class="ml-auto" />
        <UBadge v-else-if="updateAvailable" label="Disponible" color="warning" variant="soft" class="ml-auto" />
        <UBadge v-else label="À jour" variant="soft" class="ml-auto" />
      </div>
      <p v-if="checkedAt" class="mt-1.5 text-[11px] text-dimmed">Vérifié à {{ checkedAt }}</p>

      <UAlert
        v-if="updates.error || info?.error"
        class="mt-4" color="warning" variant="soft" icon="i-lucide-cloud-off"
        title="Vérification impossible"
        :description="info?.error ? `${info.error} Résultat de la vérification précédente.` : updates.error"
        :ui="{ description: 'text-xs' }"
      />

      <template v-if="updateAvailable">
        <div class="mt-2 max-h-[45vh] overflow-y-auto pr-1 scrollbar-thin">
          <section v-for="r in releases" :key="r.version">
            <p class="mt-4 text-sm font-medium text-highlighted">
              Nouveautés de la v{{ r.version }}
              <span v-if="r.published_at" class="text-xs font-normal text-muted">· {{ date(r.published_at) }}</span>
              <ULink v-if="r.url" :to="r.url" target="_blank" class="ml-1 text-xs font-normal">GitHub</ULink>
            </p>
            <template v-for="(b, i) in r.blocks" :key="i">
              <p v-if="b.kind === 'heading'" class="mt-3 text-xs font-semibold text-highlighted">
                <template v-for="(s, j) in b.content" :key="j">{{ s.text }}</template>
              </p>
              <p v-else class="mt-1.5 flex gap-2 text-xs text-muted">
                <span v-if="b.kind === 'item'" aria-hidden="true">•</span>
                <span class="min-w-0 break-words">
                  <template v-for="(s, j) in b.content" :key="j">
                    <code v-if="s.kind === 'code'" class="rounded bg-elevated px-1 font-mono text-[11px] text-highlighted">{{ s.text }}</code>
                    <strong v-else-if="s.kind === 'bold'" class="font-semibold text-highlighted">{{ s.text }}</strong>
                    <ULink v-else-if="s.kind === 'link'" :to="s.href" target="_blank" class="font-mono">{{ s.text }}</ULink>
                    <template v-else>{{ s.text }}</template>
                  </template>
                </span>
              </p>
            </template>
          </section>
        </div>
        <UAlert
          class="mt-5" color="neutral" variant="soft" icon="i-lucide-info"
          :description="`L’installation se fait dans un terminal de ${host} : elle demande les droits administrateur. ${state.stack === 'running'
            ? 'Le serveur est démarré : il sera arrêté avant l’installation puis relancé sur la nouvelle version.'
            : 'Le serveur est arrêté : il le restera après l’installation.'}`"
          :ui="{ description: 'text-xs' }"
        />
        <div class="mt-3 flex items-center gap-2 rounded-lg border border-default bg-muted py-1 pr-1 pl-3">
          <code class="min-w-0 flex-1 truncate font-mono text-xs text-highlighted">$ {{ command }}</code>
          <UButton icon="i-lucide-copy" color="neutral" variant="ghost" size="sm" aria-label="Copier la commande" @click="copy" />
        </div>
      </template>
    </template>
    <template #footer="{ close }">
      <UButton color="neutral" variant="outline" icon="i-lucide-refresh-cw" label="Vérifier" :loading="updates.checking" @click="checkUpdate()" />
      <UButton v-if="updateAvailable" icon="i-lucide-copy" label="Copier la commande" class="ml-auto" @click="copy" />
      <UButton v-else label="Fermer" class="ml-auto" @click="close" />
    </template>
  </UModal>
</template>
