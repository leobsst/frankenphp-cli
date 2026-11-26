<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import type { TableColumn } from '@nuxt/ui'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import PageTabs from '../components/PageTabs.vue'
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
import { formatNumber } from '../i18n'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const tab = ref('records')
const tabs = computed(() => [{ label: t('dns.tabs.records'), value: 'records' }, { label: t('dns.tabs.clients'), value: 'clients' }, { label: t('dns.tabs.lan'), value: 'lan' }])
const form = reactive({ ip: state.dns.ip, listen: state.dns.listen, upstream: state.dns.upstream, port: String(state.dns.port) })
const error = ref<string>()
const showIdle = ref(false)

const busy = computed(() => state.dns.status === 'restarting' || isServiceBusy('dns'))
const up = computed(() => state.dns.enabled && state.dns.status === 'running')
const stats = computed(() => [
  { label: t('dns.stats.lan'), value: state.dns.ip, sub: state.config.DNS_IP ? t('dns.stats.fixed', { variable: 'DNS_IP' }) : t('dns.stats.followsIp') },
  { label: t('dns.stats.domains'), value: active.value.length, sub: t('dns.stats.records') },
  { label: t('dns.stats.devices'), value: up.value ? dnsClientsConnected.value.length : 0, sub: t('dns.stats.lastMinutes') },
])

function submit() {
  const port = Number(form.port)
  if (!Number.isInteger(port) || port < 1 || port > 65535) { error.value = t('dns.form.invalidPort', { min: 1, max: 65535 }); return }
  actions.saveDns({ ...form, port })
}

function copy(d: Domain) {
  const rec = `${d.name}. IN A ${state.dns.ip}`
  navigator.clipboard?.writeText(rec)
  toast('info', t('dns.copied'), rec)
}

// ---------- Devices (`frankenmanager dns-clients [--all]`) ----------
const isConnected = (c: DnsClient) => up.value && clock.value - c.lastSeen < DNS_CLIENT_WINDOW_MS
const clients = computed(() => [...(showIdle.value ? state.dnsClients : state.dnsClients.filter(isConnected))].sort((a, b) => b.lastSeen - a.lastSeen))
const clientIcon = (c: DnsClient) => (/iphone|pixel|galaxy|android/i.test(c.name ?? '') ? 'i-lucide-smartphone' : /ipad|tab/i.test(c.name ?? '') ? 'i-lucide-tablet' : 'i-lucide-laptop')

function ago(ts: number) {
  const s = Math.max(0, Math.round((clock.value - ts) / 1000))
  if (s < 60) return t('dns.ago.now')
  if (s < 3600) return t('dns.ago.minutes', { n: formatNumber(Math.round(s / 60)) })
  if (s < 86_400) return t('dns.ago.hours', { n: formatNumber(Math.round(s / 3600)) })
  return t('dns.ago.days', { n: formatNumber(Math.round(s / 86_400)) })
}

const recordColumns = computed<TableColumn<Domain>[]>(() => [
  { accessorKey: 'name', header: t('dns.columns.domain') },
  { id: 'ip', header: t('dns.columns.target'), meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: t('common.status'), meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])
const clientColumns = computed<TableColumn<DnsClient>[]>(() => [
  { accessorKey: 'ip', header: t('dns.columns.device') },
  { id: 'mac', header: t('dns.columns.mac'), meta: { class: { th: 'w-40 max-lg:hidden', td: 'max-lg:hidden font-mono' } } },
  { id: 'seen', header: t('dns.columns.lastQuery'), meta: { class: { th: 'w-32 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: t('common.status'), meta: { class: { th: 'w-28' } } },
])
</script>

<template>
  <PageHeader :title="t('nav.dns')" :subtitle="t('dns.subtitle', { tld: '.test' })">
    <UButton icon="i-lucide-rotate-cw" :label="t('dns.restart')" size="lg" class="md:w-[205px]" :loading="busy" :disabled="state.stack !== 'running'" @click="actions.restartDns()" />
  </PageHeader>
  <MiniStats :items="stats" />
  <PageTabs v-model="tab" :items="tabs" />

  <TwoColumns>
    <ResourceTable v-if="tab === 'records'" storage-key="dns-records" :title="t('dns.records.title', { tld: '.test' })" :data="active" :columns="recordColumns" search-key="name">
      <template #name-cell="{ row }"><NameCell icon="i-lucide-globe" :name="row.original.name" :sub="t('dns.records.sub')" /></template>
      <template #ip-cell>{{ state.dns.ip }}</template>
      <template #state-cell><StateBadge :value="up ? 'online' : busy ? 'restarting' : 'offline'" :ok="t('dns.records.active')" /></template>
      <template #actions-cell="{ row }">
        <UDropdownMenu :items="[{ label: t('dns.records.copy'), icon: 'i-lucide-copy', onSelect: () => copy(row.original) }]" :content="{ align: 'end' }">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
        </UDropdownMenu>
      </template>
    </ResourceTable>

    <ResourceTable
        v-else-if="tab === 'clients'" storage-key="dns-clients" :title="showIdle ? t('dns.clients.all', { days: 30 }) : t('dns.stats.devices')" :data="clients" :columns="clientColumns" search-key="ip"
        :empty="up ? t('dns.clients.empty') : t('dns.clients.stopped')"
      >
        <template #ip-cell="{ row }">
          <NameCell :icon="clientIcon(row.original)" :name="row.original.name ?? t('dns.clients.unknown')" :sub="`${row.original.ip} · ${t('dns.clients.queries', { n: formatNumber(row.original.queries) }, row.original.queries)}`" />
        </template>
        <template #mac-cell="{ row }">
          <span v-if="row.original.mac" class="inline-flex items-center gap-1.5">
            {{ row.original.mac }}
            <UTooltip v-if="row.original.privateMac" :text="t('dns.clients.privateMacHelp')">
              <UBadge :label="t('dns.clients.privateMac')" color="neutral" variant="soft" size="sm" />
            </UTooltip>
          </span>
          <span v-else class="text-dimmed">—</span>
        </template>
        <template #seen-cell="{ row }">{{ ago(row.original.lastSeen) }}</template>
        <template #state-cell="{ row }">
          <UBadge v-if="isConnected(row.original)" :label="t('dns.clients.connected')" variant="soft" class="min-w-[57px] justify-center" />
          <UBadge v-else :label="t('dns.clients.idle')" color="neutral" variant="soft" class="min-w-[57px] justify-center" />
        </template>
      </ResourceTable>

    <UCard v-else :ui="{ body: 'p-5 sm:p-5 flex flex-col gap-3 text-xs text-muted' }">
      <h2 class="text-[15px] font-semibold text-highlighted">{{ t('dns.tabs.lan') }}</h2>
      <p>{{ t('dns.lan.interface', { name: 'en0 · Wi-Fi' }) }}</p>
      <i18n-t keypath="dns.lan.ip" tag="p" scope="global">
        <template #ip><b class="text-highlighted">{{ state.dns.ip }}</b></template>
        <template #mask>255.255.255.0</template>
      </i18n-t>
      <p>{{ t('dns.lan.gateway', { ip: '192.168.1.1' }) }}</p>
      <p>{{ t('dns.lan.upstream', { list: state.dns.upstream }) }}</p>
      <p class="text-[11px]">{{ t('dns.lan.ipChange') }}</p>
    </UCard>

    <div class="flex flex-col gap-6">
      <UCard v-if="tab === 'clients'" :ui="{ body: 'p-5 sm:p-5 flex flex-col gap-3' }">
        <USwitch v-model="showIdle" :label="t('dns.clients.showIdle')" :description="t('dns.clients.showIdleHelp', { days: 30, command: 'dns-clients --all' })" />
        <code class="truncate font-mono text-[10px] text-dimmed">$ frankenmanager dns-clients{{ showIdle ? ' --all' : '' }}</code>
      </UCard>
      <SideForm
        :title="t('dns.form.title')"
        :description="t('dns.form.description')"
        :hint="t('dns.form.hint', { ip: form.ip })"
        @submit="submit"
      >
        <UFormField :label="t('dns.form.ip')">
          <USelect v-model="form.ip" size="lg" class="w-full" :items="[{ label: t('dns.form.ipAuto', { ip: '192.168.1.42' }), value: '192.168.1.42' }, { label: t('dns.form.ipLoopback', { ip: '127.0.0.1' }), value: '127.0.0.1' }]" />
        </UFormField>
        <UFormField :label="t('dns.form.listen')"><UInput v-model="form.listen" size="lg" class="w-full" /></UFormField>
        <UFormField :label="t('dns.form.port')" :error="error">
          <UInput v-model="form.port" inputmode="numeric" size="lg" class="w-full" @update:model-value="error = undefined" />
        </UFormField>
        <UFormField :label="t('dns.form.upstream')"><UInput v-model="form.upstream" size="lg" class="w-full" /></UFormField>
        <UButton type="submit" :label="t('common.save')" size="lg" block :loading="busy" />
      </SideForm>
    </div>
  </TwoColumns>

  <InfoBanner v-else>{{ t('dns.banner.process') }}</InfoBanner>
</template>
