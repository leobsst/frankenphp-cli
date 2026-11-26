<script setup lang="ts">
import { computed, ref } from 'vue'
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
import { actions, active, state, type Domain } from '../composables/useFm'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const tab = ref('certs')
const tabs = computed(() => [{ label: t('ssl.tabs.certs'), value: 'certs' }, { label: t('ssl.tabs.ca'), value: 'ca' }])
// Domain to regenerate: none until one is chosen.
const domain = ref('')
const method = ref('force')
const port = ref(String(state.ssl.caPort))

const regenerating = computed(() => state.ssl.regenerating)
const domains = computed(() => active.value.map((d) => d.name))
const stats = computed(() => [
  { label: t('ssl.stats.trusted'), value: `${active.value.length} / ${active.value.length}`, sub: t('ssl.stats.authority') },
  { label: t('ssl.stats.expiry'), value: t('ssl.stats.overOneYear'), sub: t('ssl.stats.noRenewal') },
  {
    label: t('ssl.stats.caSharing'), value: state.ssl.caShared ? t('ssl.stats.active') : t('ssl.stats.inactive'),
    sub: state.ssl.caShared ? state.ssl.caUrl || t('ssl.stats.port', { port: state.ssl.caPort }) : t('ssl.stats.offByDefault'),
  },
])
const share = (on: boolean) => actions.setCaShared(on, Number(port.value) || 9080)

const columns = computed<TableColumn<Domain>[]>(() => [
  { accessorKey: 'name', header: t('ssl.columns.domain') },
  { id: 'validity', header: t('ssl.columns.validity'), meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: t('common.status'), meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])
</script>

<template>
  <PageHeader :title="t('nav.ssl')" :subtitle="t('ssl.subtitle')">
    <UButton icon="i-lucide-rotate-cw" :label="t('ssl.regenerateAll')" size="lg" class="md:w-[205px]" :loading="regenerating === 'all'" :disabled="!!regenerating" @click="actions.regenerateCert('all')" />
  </PageHeader>
  <MiniStats :items="stats" />
  <PageTabs v-model="tab" :items="tabs" />

  <TwoColumns>
    <ResourceTable v-if="tab === 'certs'" storage-key="ssl-certs" :title="t('ssl.table.title')" :data="active" :columns="columns" search-key="name">
      <template #name-cell="{ row }"><NameCell icon="i-lucide-globe" :name="row.original.name" :sub="t('ssl.table.issuer')" /></template>
      <template #validity-cell>{{ t('ssl.table.validity') }}</template>
      <template #state-cell="{ row }">
        <StateBadge :value="regenerating === row.original.name || regenerating === 'all' ? 'restarting' : 'online'" :ok="t('ssl.table.trusted')" />
      </template>
      <template #actions-cell="{ row }">
        <UDropdownMenu :items="[{ label: t('ssl.table.regenerate'), icon: 'i-lucide-rotate-cw', disabled: !!regenerating, onSelect: () => actions.regenerateCert(row.original.name) }]" :content="{ align: 'end' }">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <UCard v-else :ui="{ body: 'p-5 sm:p-5 flex flex-col gap-3 text-xs text-muted' }">
      <h2 class="text-[15px] font-semibold text-highlighted">{{ t('ssl.tabs.ca') }}</h2>
      <p>{{ t('ssl.ca.issuer', { name: 'mkcert development CA' }) }}</p>
      <p>{{ t('ssl.ca.installed') }}</p>
      <i18n-t keypath="ssl.ca.fingerprint" tag="p" scope="global">
        <template #value><span class="font-mono">3F:9A:…:C2:71</span></template>
      </i18n-t>
      <USwitch :model-value="state.ssl.caShared" :label="t('ssl.share')" @update:model-value="share" />
    </UCard>

    <SideForm
      :title="t('ssl.form.title')"
      :description="t('ssl.form.description')"
      :hint="t('ssl.form.hint')"
      @submit="actions.regenerateCert(domain)"
    >
      <UFormField :label="t('ssl.form.domain')"><USelect v-model="domain" :items="domains" :placeholder="t('ssl.form.chooseDomain')" size="lg" class="w-full" /></UFormField>
      <UFormField :label="t('ssl.form.authority')"><USelect model-value="mkcert" :items="[{ label: t('ssl.form.mkcert'), value: 'mkcert' }]" size="lg" class="w-full" disabled /></UFormField>
      <UFormField :label="t('ssl.form.method')">
        <USelect v-model="method" size="lg" class="w-full" :items="[{ label: t('ssl.form.force'), value: 'force' }, { label: t('ssl.form.expired'), value: 'expired' }]" />
      </UFormField>
      <UFormField :label="t('ssl.form.port')"><UInput v-model="port" inputmode="numeric" size="lg" class="w-full" /></UFormField>
      <USwitch :model-value="state.ssl.caShared" :label="t('ssl.share')" @update:model-value="share" />
      <UButton type="submit" :label="t('ssl.form.submit')" size="lg" block :loading="!!regenerating && regenerating !== 'all'" :disabled="!!regenerating || !domain" />
    </SideForm>
  </TwoColumns>

  <InfoBanner v-if="state.ssl.caShared" tone="warning">
    {{ state.ssl.caUrl ? t('ssl.banner.sharedUrl', { url: state.ssl.caUrl }) : t('ssl.banner.sharedPort', { port: state.ssl.caPort }) }}
  </InfoBanner>
  <InfoBanner v-else>{{ t('ssl.banner.notShared') }}</InfoBanner>
</template>
