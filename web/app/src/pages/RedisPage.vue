<script setup lang="ts">
import { computed, ref } from 'vue'
import type { TableColumn } from '@nuxt/ui'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import TwoColumns from '../components/TwoColumns.vue'
import ResourceTable from '../components/ResourceTable.vue'
import NameCell from '../components/NameCell.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
import ConfirmModal from '../components/ConfirmModal.vue'
import { actions, isServiceBusy, redisMatch, serviceEnabled, state, toast, type RedisKey } from '../composables/useFm'

// Cache Redis : `/api/v1/redis` (INFO), `/redis/keys` (SCAN MATCH puis TYPE et TTL), `/redis/keys/delete` (DEL),
// `/redis/flush` (FLUSHALL). Le daemon parle le protocole Redis directement sur REDIS_PORT, sans redis-cli.
const r = state.redis
const tab = ref('keys')
const tabs = [{ label: 'Clés du cache', value: 'keys' }, { label: 'Serveur', value: 'stats' }]
const flushOpen = ref(false)
const drop = ref<string | null>(null)
const copy = (key: string) => { navigator.clipboard?.writeText(key); toast('info', 'Copié', key) }

const pattern = ref('*')
const applied = ref('*')
const count = ref(100)
const shown = computed(() => r.sample.filter((k) => redisMatch(applied.value, k.key)))
// SCAN MATCH côté serveur : le motif et le nombre de clés sont envoyés au daemon.
const scan = async () => { applied.value = pattern.value.trim() || '*'; await actions.scanRedis(applied.value, count.value) }

const enabled = computed(() => serviceEnabled('redis'))
const up = computed(() => state.containers.find((c) => c.kind === 'redis')?.status === 'running')
const hits = computed(() => r.hits)
const misses = computed(() => r.misses)
const stats = computed(() => [
  { label: 'Mémoire', value: `${r.memMB} Mo`, sub: 'used_memory' },
  { label: 'Taux de hits', value: hits.value + misses.value ? `${(Math.round((hits.value / (hits.value + misses.value)) * 1000) / 10).toLocaleString('fr-FR')} %` : '—', sub: `${hits.value.toLocaleString('fr-FR')} hits · ${misses.value.toLocaleString('fr-FR')} misses` },
  { label: 'Clés', value: r.keys.toLocaleString('fr-FR'), sub: 'Toutes les bases Redis' },
])

/** TTL lisible : -1 sans expiration. */
function ttl(s: number) {
  if (s < 0) return 'Sans expiration'
  if (s < 60) return `${s} s`
  if (s < 3600) return `${Math.round(s / 60)} min`
  return `${(Math.round(s / 360) / 10).toLocaleString('fr-FR')} h`
}

const columns: TableColumn<RedisKey>[] = [
  { accessorKey: 'key', header: 'Clé / type' },
  { id: 'ttl', header: 'TTL', meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]
</script>

<template>
  <PageHeader title="Cache Redis" subtitle="Parcourez les clés du cache partagé et surveillez son utilisation.">
    <UButton v-if="!enabled" icon="i-lucide-power" label="Activer Redis" size="lg" class="md:w-[205px]" :loading="isServiceBusy('redis')" @click="actions.setService('redis', true)" />
    <UButton v-else icon="i-lucide-eraser" label="Vider le cache" size="lg" class="md:w-[205px]" :disabled="!up || r.keys === 0" @click="flushOpen = true" />
  </PageHeader>
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-16 pb-2.5' }" />

  <TwoColumns>
    <ResourceTable v-if="tab === 'keys'" :title="applied === '*' ? 'Clés du cache' : `Clés ${applied}`" :data="shown" :columns="columns" search-key="key" empty="Aucune clé ne correspond.">
      <template #key-cell="{ row }"><NameCell icon="i-lucide-key-round" :name="row.original.key" :sub="`${row.original.type} · ${row.original.size}`" /></template>
      <template #ttl-cell="{ row }"><span :class="row.original.ttl >= 0 && row.original.ttl < 60 ? 'text-warning' : ''">{{ ttl(row.original.ttl) }}</span></template>
      <template #actions-cell="{ row }">
        <UDropdownMenu
          :content="{ align: 'end' }"
          :items="[
            [{ label: 'Copier la clé', icon: 'i-lucide-copy', onSelect: () => copy(row.original.key) }],
            [{ label: 'Supprimer la clé', icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { drop = row.original.key } }],
          ]"
        >
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <UCard v-else :ui="{ body: 'p-5 sm:p-5 flex flex-col gap-3 text-xs text-muted' }">
      <h2 class="text-[15px] font-semibold text-highlighted">Serveur Redis</h2>
      <p>Version : Redis 7.4.1 · uptime 3 h 12 min</p>
      <p>Clients connectés : 6 · Mémoire utilisée : {{ r.memMB }} Mo</p>
      <p>Hits : {{ hits.toLocaleString('fr-FR') }} · Misses : {{ misses.toLocaleString('fr-FR') }}</p>
      <p>Écoute sur <span class="font-mono">127.0.0.1:{{ state.config.REDIS_PORT }}</span> (REDIS_PORT) · conteneur franken_redis</p>
    </UCard>

    <SideForm
      title="Parcourir les clés"
      description="Le daemon interroge Redis avec SCAN, sans bloquer le serveur."
      hint="* remplace n’importe quelle suite de caractères, ? un seul caractère."
      @submit="scan"
    >
      <UFormField label="Motif (SCAN MATCH)">
        <UInput v-model="pattern" placeholder="laravel_*" size="lg" class="w-full font-mono" />
      </UFormField>
      <UFormField label="Clés par page (COUNT)">
        <USelect v-model="count" size="lg" class="w-full" :items="[100, 500, 1000]" />
      </UFormField>
      <UButton type="submit" label="Rechercher" icon="i-lucide-search" size="lg" block :disabled="!up" />
      <USwitch
        :model-value="enabled" :disabled="isServiceBusy('redis')" label="Service Redis activé"
        description="Démarré ou arrêté immédiatement (service enable|disable redis)."
        @update:model-value="(v: boolean) => actions.setService('redis', v)"
      />
    </SideForm>
  </TwoColumns>

  <InfoBanner v-if="!enabled" tone="warning">Redis est désactivé : le conteneur franken_redis n’existe pas. Activez-le pour utiliser le cache.</InfoBanner>
  <InfoBanner v-else>Supprimer une clé ou vider le cache demande une confirmation, et le mot de passe du dashboard depuis l’accès distant.</InfoBanner>
  <ConfirmModal
    v-model:open="flushOpen"
    eyebrow="Action destructive"
    title="Vider le cache Redis ?"
    :description="`Les ${r.keys.toLocaleString('fr-FR')} clés de toutes les bases Redis seront supprimées (FLUSHALL). Cette action est irréversible.`"
    type-to-confirm="VIDER"
    hint="Les bases SQL ne sont pas concernées."
    confirm="Vider le cache"
    @confirm="actions.flushRedis()"
  />
  <ConfirmModal
    :open="!!drop"
    eyebrow="Action destructive"
    :title="`Supprimer ${drop} ?`"
    description="La clé et sa valeur seront supprimées du cache (DEL)."
    confirm="Supprimer la clé"
    @update:open="(v: boolean) => { if (!v) drop = null }"
    @confirm="actions.deleteRedisKeys([drop!])"
  />
</template>
