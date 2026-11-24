<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import type { TableColumn } from '@nuxt/ui'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import TwoColumns from '../components/TwoColumns.vue'
import ResourceTable from '../components/ResourceTable.vue'
import NameCell from '../components/NameCell.vue'
import StateBadge from '../components/StateBadge.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
import {
  DNS_CLIENT_WINDOW_MS, actions, active, clock, dnsClientsConnected, isServiceBusy, state, toast,
  type DnsClient, type Domain,
} from '../composables/useFm'

const tab = ref('records')
const tabs = [{ label: 'Enregistrements', value: 'records' }, { label: 'Appareils', value: 'clients' }, { label: 'Réseau local', value: 'lan' }]
const form = reactive({ ip: state.dns.ip, listen: state.dns.listen, upstream: state.dns.upstream, enabled: state.dns.enabled, port: String(state.dns.port) })
const error = ref<string>()
const showIdle = ref(false)

const busy = computed(() => state.dns.status === 'restarting' || isServiceBusy('dns'))
const up = computed(() => state.dns.enabled && state.dns.status === 'running')
const stats = computed(() => [
  { label: 'Adresse LAN', value: state.dns.ip, sub: state.config.DNS_IP ? 'Fixée par DNS_IP' : 'Suit les changements d’IP' },
  { label: 'Domaines', value: active.value.length, sub: 'Enregistrements A' },
  { label: 'Appareils connectés', value: up.value ? dnsClientsConnected.value.length : 0, sub: 'Dans les 5 dernières minutes' },
])

function submit() {
  const port = Number(form.port)
  if (!Number.isInteger(port) || port < 1 || port > 65535) { error.value = 'Port invalide (1-65535).'; return }
  actions.saveDns({ ...form, port })
}

function copy(d: Domain) {
  const rec = `${d.name}. IN A ${state.dns.ip}`
  navigator.clipboard?.writeText(rec)
  toast('info', 'Copié', rec)
}

// ---------- Devices (`frankenmanager dns-clients [--all]`) ----------
const isConnected = (c: DnsClient) => up.value && clock.value - c.lastSeen < DNS_CLIENT_WINDOW_MS
const clients = computed(() => [...(showIdle.value ? state.dnsClients : state.dnsClients.filter(isConnected))].sort((a, b) => b.lastSeen - a.lastSeen))
const clientIcon = (c: DnsClient) => (/iphone|pixel|galaxy|android/i.test(c.name ?? '') ? 'i-lucide-smartphone' : /ipad|tab/i.test(c.name ?? '') ? 'i-lucide-tablet' : 'i-lucide-laptop')

function ago(ts: number) {
  const s = Math.max(0, Math.round((clock.value - ts) / 1000))
  if (s < 60) return 'à l’instant'
  if (s < 3600) return `il y a ${Math.round(s / 60)} min`
  if (s < 86_400) return `il y a ${Math.round(s / 3600)} h`
  return `il y a ${Math.round(s / 86_400)} j`
}

const recordColumns: TableColumn<Domain>[] = [
  { accessorKey: 'name', header: 'Domaine / type' },
  { id: 'ip', header: 'Adresse cible', meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: 'État', meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]
const clientColumns: TableColumn<DnsClient>[] = [
  { accessorKey: 'ip', header: 'Appareil / IP' },
  { id: 'mac', header: 'Adresse MAC', meta: { class: { th: 'w-40 max-lg:hidden', td: 'max-lg:hidden font-mono' } } },
  { id: 'seen', header: 'Dernière requête', meta: { class: { th: 'w-32 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: 'État', meta: { class: { th: 'w-28' } } },
]
</script>

<template>
  <PageHeader title="Serveur DNS" subtitle="Résolvez vos domaines .test depuis tous les appareils du réseau.">
    <UButton v-if="!state.dns.enabled" icon="i-lucide-power" label="Activer le DNS" size="lg" class="md:w-[205px]" :loading="busy" @click="actions.setService('dns', true)" />
    <UButton v-else icon="i-lucide-rotate-cw" label="Redémarrer le DNS" size="lg" class="md:w-[205px]" :loading="busy" :disabled="state.stack !== 'running'" @click="actions.restartDns()" />
  </PageHeader>
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-4 md:me-16 pb-2.5' }" />

  <TwoColumns>
    <ResourceTable v-if="tab === 'records'" title="Zone locale .test" :data="active" :columns="recordColumns" search-key="name">
      <template #name-cell="{ row }"><NameCell icon="i-lucide-globe" :name="row.original.name" sub="Enregistrement A · LAN" /></template>
      <template #ip-cell>{{ state.dns.ip }}</template>
      <template #state-cell><StateBadge :value="up ? 'online' : busy ? 'restarting' : 'offline'" ok="Actif" /></template>
      <template #actions-cell="{ row }">
        <UDropdownMenu :items="[{ label: 'Copier l’enregistrement', icon: 'i-lucide-copy', onSelect: () => copy(row.original) }]" :content="{ align: 'end' }">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
        </UDropdownMenu>
      </template>
    </ResourceTable>

    <ResourceTable
        v-else-if="tab === 'clients'" :title="showIdle ? 'Tous les appareils (30 jours)' : 'Appareils connectés'" :data="clients" :columns="clientColumns" search-key="ip"
        :empty="up ? 'Aucun appareil n’a interrogé le serveur dans les 5 dernières minutes.' : 'Le serveur DNS est arrêté.'"
      >
        <template #ip-cell="{ row }">
          <NameCell :icon="clientIcon(row.original)" :name="row.original.name ?? 'Appareil inconnu'" :sub="`${row.original.ip} · ${row.original.queries.toLocaleString('fr-FR')} requêtes`" />
        </template>
        <template #mac-cell="{ row }">
          <span v-if="row.original.mac" class="inline-flex items-center gap-1.5">
            {{ row.original.mac }}
            <UTooltip v-if="row.original.privateMac" text="Adresse privée : l’appareil en change selon le réseau">
              <UBadge label="privée" color="neutral" variant="soft" size="sm" />
            </UTooltip>
          </span>
          <span v-else class="text-dimmed">—</span>
        </template>
        <template #seen-cell="{ row }">{{ ago(row.original.lastSeen) }}</template>
        <template #state-cell="{ row }">
          <UBadge v-if="isConnected(row.original)" label="Connecté" variant="soft" class="min-w-[57px] justify-center" />
          <UBadge v-else label="Inactif" color="neutral" variant="soft" class="min-w-[57px] justify-center" />
        </template>
      </ResourceTable>

    <UCard v-else :ui="{ body: 'p-5 sm:p-5 flex flex-col gap-3 text-xs text-muted' }">
      <h2 class="text-[15px] font-semibold text-highlighted">Réseau local</h2>
      <p>Interface : en0 · Wi-Fi</p>
      <p>Adresse IP : <b class="text-highlighted">{{ state.dns.ip }}</b> · Masque 255.255.255.0</p>
      <p>Passerelle : 192.168.1.1</p>
      <p>Résolveurs en amont : {{ state.dns.upstream }}</p>
      <p class="text-[11px]">Si l’adresse IP change (nouveau réseau), FrankenManager met la zone à jour automatiquement.</p>
    </UCard>

    <div class="flex flex-col gap-6">
      <UCard v-if="tab === 'clients'" :ui="{ body: 'p-5 sm:p-5 flex flex-col gap-3' }">
        <USwitch v-model="showIdle" label="Afficher les appareils inactifs" description="Conservés 30 jours (dns-clients --all)." />
        <code class="truncate font-mono text-[10px] text-dimmed">$ frankenmanager dns-clients{{ showIdle ? ' --all' : '' }}</code>
      </UCard>
      <SideForm
        title="Configuration DNS"
        description="Pris en compte en quelques secondes, sans redémarrage."
        :hint="`Configurez ${form.ip} comme DNS sur vos appareils. Réservez une IP fixe dans le routeur.`"
        @submit="submit"
      >
        <UFormField label="Adresse IP des domaines">
          <USelect v-model="form.ip" size="lg" class="w-full" :items="[{ label: 'Automatique · 192.168.1.42', value: '192.168.1.42' }, { label: 'Boucle locale · 127.0.0.1', value: '127.0.0.1' }]" />
        </UFormField>
        <UFormField label="Adresse d’écoute"><UInput v-model="form.listen" size="lg" class="w-full" /></UFormField>
        <UFormField label="Port d’écoute" :error="error">
          <UInput v-model="form.port" inputmode="numeric" size="lg" class="w-full" @update:model-value="error = undefined" />
        </UFormField>
        <UFormField label="Résolveurs en amont"><UInput v-model="form.upstream" size="lg" class="w-full" /></UFormField>
        <USwitch v-model="form.enabled" label="Activer le serveur DNS local" />
        <UButton type="submit" label="Enregistrer" size="lg" block :loading="busy" />
      </SideForm>
    </div>
  </TwoColumns>

  <InfoBanner v-if="!state.dns.enabled" tone="warning">Le serveur DNS est désactivé : vos appareils ne résolvent pas les domaines .test.</InfoBanner>
  <InfoBanner v-else>Le serveur DNS n’est pas un conteneur : il tourne dans un processus d’arrière-plan de FrankenManager, qui voit l’adresse réelle de chaque appareil.</InfoBanner>
</template>
