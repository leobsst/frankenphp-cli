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
import { formatMegabytes, formatNumber, formatPercent } from '../i18n'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

// Redis cache: `/api/v1/redis` (INFO), `/redis/keys` (SCAN MATCH then TYPE and TTL), `/redis/keys/delete` (DEL),
// `/redis/flush` (FLUSHALL). The daemon speaks the Redis protocol directly on REDIS_PORT, without redis-cli.
const r = state.redis
const tab = ref('keys')
const tabs = computed(() => [{ label: t('redis.tabs.keys'), value: 'keys' }, { label: t('redis.tabs.server'), value: 'stats' }])
const flushOpen = ref(false)
const drop = ref<string | null>(null)
const copy = (key: string) => { navigator.clipboard?.writeText(key); toast('info', t('redis.copied'), key) }

const pattern = ref('*')
const applied = ref('*')
const count = ref(100)
const shown = computed(() => r.sample.filter((k) => redisMatch(applied.value, k.key)))
// Server-side SCAN MATCH: the pattern and key count are sent to the daemon.
const scan = async () => { applied.value = pattern.value.trim() || '*'; await actions.scanRedis(applied.value, count.value) }

const enabled = computed(() => serviceEnabled('redis'))
const up = computed(() => state.containers.find((c) => c.kind === 'redis')?.status === 'running')
const hits = computed(() => r.hits)
const misses = computed(() => r.misses)
const stats = computed(() => [
  { label: t('redis.stats.memory'), value: formatMegabytes(r.memMB), sub: 'used_memory' },
  {
    label: t('redis.stats.hitRate'), value: hits.value + misses.value ? formatPercent(Math.round((hits.value / (hits.value + misses.value)) * 1000) / 10) : '—',
    sub: t('redis.stats.hitsMisses', { hits: formatNumber(hits.value), misses: formatNumber(misses.value) }),
  },
  { label: t('redis.stats.keys'), value: formatNumber(r.keys), sub: t('redis.stats.allDatabases') },
])

/** Readable TTL: -1 means no expiration. */
function ttl(s: number) {
  if (s < 0) return t('redis.ttl.none')
  if (s < 60) return t('redis.ttl.seconds', { n: formatNumber(s) })
  if (s < 3600) return t('redis.ttl.minutes', { n: formatNumber(Math.round(s / 60)) })
  return t('units.hours', { n: formatNumber(Math.round(s / 360) / 10) })
}

const columns = computed<TableColumn<RedisKey>[]>(() => [
  { accessorKey: 'key', header: t('redis.columns.key') },
  { id: 'ttl', header: 'TTL', meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])
/** Uptime shown on the server card. */
const uptime = computed(() => `${t('units.hours', { n: formatNumber(3) })} ${t('redis.ttl.minutes', { n: formatNumber(12) })}`)
</script>

<template>
  <PageHeader :title="t('nav.redis')" :subtitle="t('redis.subtitle')">
    <UButton v-if="!enabled" icon="i-lucide-power" :label="t('redis.enable')" size="lg" class="md:w-[205px]" :loading="isServiceBusy('redis')" @click="actions.setService('redis', true)" />
    <UButton v-else icon="i-lucide-eraser" :label="t('redis.flush.button')" size="lg" class="md:w-[205px]" :disabled="!up || r.keys === 0" @click="flushOpen = true" />
  </PageHeader>
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-16 pb-2.5' }" />

  <TwoColumns>
    <ResourceTable v-if="tab === 'keys'" storage-key="redis-keys" :title="applied === '*' ? t('redis.tabs.keys') : t('redis.keysMatching', { pattern: applied })" :data="shown" :columns="columns" search-key="key" :empty="t('redis.empty')">
      <template #key-cell="{ row }"><NameCell icon="i-lucide-key-round" :name="row.original.key" :sub="`${row.original.type} · ${row.original.size}`" /></template>
      <template #ttl-cell="{ row }"><span :class="row.original.ttl >= 0 && row.original.ttl < 60 ? 'text-warning' : ''">{{ ttl(row.original.ttl) }}</span></template>
      <template #actions-cell="{ row }">
        <UDropdownMenu
          :content="{ align: 'end' }"
          :items="[
            [{ label: t('redis.menu.copy'), icon: 'i-lucide-copy', onSelect: () => copy(row.original.key) }],
            [{ label: t('redis.menu.delete'), icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { drop = row.original.key } }],
          ]"
        >
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <UCard v-else :ui="{ body: 'p-5 sm:p-5 flex flex-col gap-3 text-xs text-muted' }">
      <h2 class="text-[15px] font-semibold text-highlighted">{{ t('redis.server.title') }}</h2>
      <p>{{ t('redis.server.version', { version: 'Redis 7.4.1', uptime }) }}</p>
      <p>{{ t('redis.server.clients', { clients: formatNumber(6), memory: formatMegabytes(r.memMB) }) }}</p>
      <p>{{ t('redis.server.hits', { hits: formatNumber(hits), misses: formatNumber(misses) }) }}</p>
      <i18n-t keypath="redis.server.listen" tag="p" scope="global">
        <template #address><span class="font-mono">127.0.0.1:{{ state.config.REDIS_PORT }}</span></template>
        <template #variable>REDIS_PORT</template>
        <template #container>franken_redis</template>
      </i18n-t>
    </UCard>

    <SideForm
      :title="t('redis.form.title')"
      :description="t('redis.form.description')"
      :hint="t('redis.form.hint', { any: '*', one: '?' })"
      @submit="scan"
    >
      <UFormField :label="t('redis.form.pattern')">
        <UInput v-model="pattern" placeholder="laravel_*" size="lg" class="w-full font-mono" />
      </UFormField>
      <UFormField :label="t('redis.form.count')">
        <USelect v-model="count" size="lg" class="w-full" :items="[100, 500, 1000]" />
      </UFormField>
      <UButton type="submit" :label="t('common.search')" icon="i-lucide-search" size="lg" block :disabled="!up" />
      <USwitch
        :model-value="enabled" :disabled="isServiceBusy('redis')" :label="t('redis.form.enabled')"
        :description="t('redis.form.enabledHelp', { command: 'service enable|disable redis' })"
        @update:model-value="(v: boolean) => actions.setService('redis', v)"
      />
    </SideForm>
  </TwoColumns>

  <InfoBanner v-if="!enabled" tone="warning">{{ t('redis.banner.disabled', { container: 'franken_redis' }) }}</InfoBanner>
  <InfoBanner v-else>{{ t('redis.banner.confirm') }}</InfoBanner>
  <ConfirmModal
    v-model:open="flushOpen"
    :eyebrow="t('redis.destructive')"
    :title="t('redis.flush.title')"
    :description="t('redis.flush.description', { n: formatNumber(r.keys) }, r.keys)"
    :type-to-confirm="t('redis.flush.word')"
    :hint="t('redis.flush.hint')"
    :confirm="t('redis.flush.button')"
    @confirm="actions.flushRedis()"
  />
  <ConfirmModal
    :open="!!drop"
    :eyebrow="t('redis.destructive')"
    :title="t('redis.drop.title', { key: drop })"
    :description="t('redis.drop.description')"
    :confirm="t('redis.menu.delete')"
    @update:open="(v: boolean) => { if (!v) drop = null }"
    @confirm="actions.deleteRedisKeys([drop!])"
  />
</template>
