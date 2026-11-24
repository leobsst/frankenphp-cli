<script setup lang="ts">
import { computed, ref } from 'vue'
import type { TableColumn } from '@nuxt/ui'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import TwoColumns from '../components/TwoColumns.vue'
import ResourceTable from '../components/ResourceTable.vue'
import NameCell from '../components/NameCell.vue'
import StateBadge from '../components/StateBadge.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
import { actions, active, state, type Domain } from '../composables/useFm'

const tab = ref('certs')
const tabs = [{ label: 'Certificats', value: 'certs' }, { label: 'Autorité locale', value: 'ca' }]
const domain = ref(active.value[0]?.name ?? '')
const method = ref('force')
const port = ref(String(state.ssl.caPort))

const regenerating = computed(() => state.ssl.regenerating)
const domains = computed(() => active.value.map((d) => d.name))
const stats = computed(() => [
  { label: 'Approuvés', value: `${active.value.length} / ${active.value.length}`, sub: 'Autorité locale mkcert' },
  { label: 'Expiration', value: '> 1 an', sub: 'Aucun renouvellement requis' },
  { label: 'Partage CA', value: state.ssl.caShared ? 'Actif' : 'Inactif', sub: state.ssl.caShared ? state.ssl.caUrl || `port ${state.ssl.caPort}` : 'Désactivé par défaut' },
])
const share = (on: boolean) => actions.setCaShared(on, Number(port.value) || 9080)

const columns: TableColumn<Domain>[] = [
  { accessorKey: 'name', header: 'Domaine / émetteur' },
  { id: 'validity', header: 'Validité', meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: 'État', meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]
</script>

<template>
  <PageHeader title="Certificats SSL" subtitle="Gérez les certificats locaux et la confiance HTTPS de vos appareils.">
    <UButton icon="i-lucide-rotate-cw" label="Régénérer les certificats" size="lg" class="md:w-[205px]" :loading="regenerating === 'all'" :disabled="!!regenerating" @click="actions.regenerateCert('all')" />
  </PageHeader>
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-16 pb-2.5' }" />

  <TwoColumns>
    <ResourceTable v-if="tab === 'certs'" title="Certificats des domaines" :data="active" :columns="columns" search-key="name">
      <template #name-cell="{ row }"><NameCell icon="i-lucide-globe" :name="row.original.name" sub="Autorité locale · mkcert" /></template>
      <template #validity-cell>Valide · &gt; 1 an</template>
      <template #state-cell="{ row }">
        <StateBadge :value="regenerating === row.original.name || regenerating === 'all' ? 'restarting' : 'online'" ok="Approuvé" />
      </template>
      <template #actions-cell="{ row }">
        <UDropdownMenu :items="[{ label: 'Régénérer ce certificat', icon: 'i-lucide-rotate-cw', disabled: !!regenerating, onSelect: () => actions.regenerateCert(row.original.name) }]" :content="{ align: 'end' }">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <UCard v-else :ui="{ body: 'p-5 sm:p-5 flex flex-col gap-3 text-xs text-muted' }">
      <h2 class="text-[15px] font-semibold text-highlighted">Autorité locale</h2>
      <p>Émetteur : mkcert development CA</p>
      <p>Installée dans le trousseau système · approuvée</p>
      <p>Empreinte SHA-256 : <span class="font-mono">3F:9A:…:C2:71</span></p>
      <USwitch :model-value="state.ssl.caShared" label="Partager la CA sur le réseau local" @update:model-value="share" />
    </UCard>

    <SideForm
      title="Régénérer un certificat"
      description="Le certificat existant sera remplacé pour ce domaine."
      hint="La clé privée de la CA n’est jamais partagée. Désactivez le partage après l’installation."
      @submit="actions.regenerateCert(domain)"
    >
      <UFormField label="Domaine concerné"><USelect v-model="domain" :items="domains" size="lg" class="w-full" /></UFormField>
      <UFormField label="Autorité de certification"><USelect model-value="mkcert" :items="[{ label: 'mkcert · Autorité locale', value: 'mkcert' }]" size="lg" class="w-full" disabled /></UFormField>
      <UFormField label="Méthode">
        <USelect v-model="method" size="lg" class="w-full" :items="[{ label: 'Forcer la régénération', value: 'force' }, { label: 'Seulement si expiré', value: 'expired' }]" />
      </UFormField>
      <UFormField label="Port de partage de la CA"><UInput v-model="port" inputmode="numeric" size="lg" class="w-full" /></UFormField>
      <USwitch :model-value="state.ssl.caShared" label="Partager la CA sur le réseau local" @update:model-value="share" />
      <UButton type="submit" label="Régénérer le certificat" size="lg" block :loading="!!regenerating && regenerating !== 'all'" :disabled="!!regenerating || !domain" />
    </SideForm>
  </TwoColumns>

  <InfoBanner v-if="state.ssl.caShared" tone="warning">
    La CA est partagée sur {{ state.ssl.caUrl || `le port ${state.ssl.caPort}` }}. Désactivez le partage une fois vos appareils configurés.
  </InfoBanner>
  <InfoBanner v-else>Sur un autre appareil, installez puis approuvez la CA locale pour éviter les avertissements HTTPS.</InfoBanner>
</template>
