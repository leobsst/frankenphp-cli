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
import ConfirmModal from '../components/ConfirmModal.vue'
import CaddyfileEditor from '../components/CaddyfileEditor.vue'
import TablePagination from '../components/TablePagination.vue'
import { usePagination } from '../composables/usePagination'
import { focusField } from '../composables/focus'
import { simpleDomain } from '../composables/caddyfile'
import { actions, active, aliasTarget, caddyfilePathOf, containerFor, domainStatus, hasSiteCaddyfile, isCaddyfileCustom, isProxied, servedPath, state, type Domain, type FieldErrors } from '../composables/useFm'
import { formatNumber } from '../i18n'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const tab = ref('routes')
const tabs = computed(() => [{ label: t('proxy.tabs.routes'), value: 'routes' }, { label: t('proxy.tabs.caddy'), value: 'caddy' }])
// The domain is not filled in in advance: an example as placeholder.
const form = reactive({ source: '', target: 'http://127.0.0.1:5173', type: 'address', protocol: 'https', websocket: true })
const errors = ref<FieldErrors>({})
const archive = ref<string | null>(null)
const archivedKind = computed(() => state.domains.find((d) => d.name === archive.value)?.kind)

const caddyUp = computed(() => state.containers.find((c) => c.kind === 'caddy')?.status === 'running')
const stats = computed(() => [
  { label: t('proxy.stats.routes'), value: active.value.length, sub: caddyUp.value ? t('proxy.stats.allActive') : t('proxy.stats.caddyStopped') },
  { label: t('proxy.stats.ports'), value: '80 / 443', sub: 'HTTP → HTTPS' },
  {
    label: t('proxy.stats.response'), value: caddyUp.value ? t('proxy.stats.ms', { n: formatNumber(16 + (state.cpuHistory[state.cpuHistory.length - 1] % 5)) }) : '—',
    sub: t('proxy.stats.latency'),
  },
])
// Alias targets: PHP projects and proxy hosts.
const aliasTargets = computed(() => active.value
  .filter((d) => d.kind === 'php' || d.kind === 'proxy')
  .map((d) => ({ label: d.kind === 'proxy' ? `${d.name} · proxy` : `${d.name} · PHP ${d.php}`, value: d.name })))

const dest = (d: Domain) => (d.kind === 'proxy' ? d.target : d.kind === 'alias' ? `→ ${d.aliasOf}` : d.kind === 'static' ? t('proxy.dest.files') : `127.0.0.1:${containerFor(d)?.port ?? '—'}`)
// A proxy route relays WebSocket by default (Caddy); when refused, an upgrade gets a 403 (v0.11.0).
const wsAllowed = (d: Domain) => d.websocket !== false
const kind = (d: Domain) => {
  if (d.kind === 'proxy') return wsAllowed(d) ? t('proxy.kind.proxy') : t('proxy.kind.proxyNoWs')
  if (d.kind === 'alias') return isProxied(d) ? t('proxy.kind.proxyAlias') : t('proxy.kind.alias')
  if (d.kind === 'static') return d.spa ? t('proxy.kind.spa') : t('proxy.kind.static')
  return t('proxy.kind.php')
}

/** Caddy block of a domain; a proxy host alias follows its upstream with the target as Host header. */
function caddyBlock(d: Domain) {
  // A static host is imported from its own file, served by the reverse proxy (projects folder mounted read-only).
  if (d.kind === 'static') return `# ${d.name} → ${servedPath(d)}\nimport /etc/caddy/sites/static/${simpleDomain(d.name)}_Caddyfile`
  const up = aliasTarget(d)
  const upstream = d.kind === 'proxy' ? d.target : up?.kind === 'proxy' ? `${up.target} {\n    header_up Host ${up.name}\n  }` : `frankenphp-${d.php}:80`
  const noWs = d.kind === 'proxy' && !wsAllowed(d)
    ? '  @websocket {\n    header Connection *Upgrade*\n    header Upgrade websocket\n  }\n  respond @websocket "WebSocket connections are disabled for this host" 403\n'
    : ''
  return `${d.name} {\n  tls internal\n${noWs}  reverse_proxy ${upstream}\n}`
}
const caddyfile = computed(() => active.value.map(caddyBlock).join('\n\n'))

// Site Caddyfiles: one per PHP project and per static host, editable (aliases and proxy hosts have none).
const sites = computed(() => active.value.filter(hasSiteCaddyfile))
const sitePager = usePagination(sites, { key: 'proxy-site-caddyfiles' })
const { page: sitePage, pageSize: siteSize, total: siteTotal, from: siteFrom, to: siteTo, rows: siteRows } = sitePager
// The domain stays set after closing so the title does not go blank during the animation.
const editing = ref('')
const editorOpen = ref(false)
const edit = (name: string) => { editing.value = name; editorOpen.value = true }

function onTypeChange(v: string) {
  form.target = v === 'alias' ? '' : 'http://127.0.0.1:5173'
  errors.value = {}
}

async function submit() {
  if (form.type === 'alias') {
    const err = await actions.addAlias(form.target, form.source.trim().toLowerCase())
    errors.value = err ? { source: err } : {}
    return
  }
  const err = await actions.addRoute({ source: form.source.trim().toLowerCase(), target: form.target.trim(), websocket: form.websocket })
  errors.value = err ?? {}
  if (!err) Object.assign(form, { source: '', target: 'http://127.0.0.1:' })
}

const columns = computed<TableColumn<Domain>[]>(() => [
  { accessorKey: 'name', header: t('proxy.columns.source') },
  { id: 'dest', header: t('proxy.columns.dest'), meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: t('common.status'), meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])
</script>

<template>
  <PageHeader :title="t('nav.proxy')" :subtitle="t('proxy.subtitle')">
    <UButton icon="i-lucide-plus" :label="t('proxy.addRoute')" size="lg" class="md:w-[205px]" @click="focusField('route-source')" />
  </PageHeader>
  <MiniStats :items="stats" />
  <PageTabs v-model="tab" :items="tabs" />

  <TwoColumns>
    <ResourceTable v-if="tab === 'routes'" storage-key="proxy-routes" :title="t('proxy.table.title')" :data="active" :columns="columns" search-key="name">
      <template #name-cell="{ row }"><NameCell icon="i-lucide-arrow-up-right" :name="row.original.name" :sub="kind(row.original)" /></template>
      <template #dest-cell="{ row }">{{ dest(row.original) }}</template>
      <template #state-cell="{ row }"><StateBadge :value="domainStatus(row.original)" :ok="t('proxy.table.active')" /></template>
      <template #actions-cell="{ row }">
        <UDropdownMenu
          :content="{ align: 'end' }"
          :items="[
            [
              ...(hasSiteCaddyfile(row.original) ? [{ label: t('proxy.menu.editCaddyfile'), icon: 'i-lucide-file-code', onSelect: () => edit(row.original.name) }] : []),
              ...(row.original.kind === 'proxy' ? [{
                label: wsAllowed(row.original) ? t('proxy.menu.refuseWs') : t('proxy.menu.allowWs'), icon: 'i-lucide-radio',
                onSelect: () => actions.setWebsocket(row.original.name, !wsAllowed(row.original)),
              }] : []),
              { label: t('proxy.menu.reload'), icon: 'i-lucide-rotate-cw', onSelect: () => actions.restartContainer('caddy') },
            ],
            [{ label: row.original.kind === 'proxy' ? t('proxy.menu.deleteRoute') : t('proxy.menu.archive'), icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { archive = row.original.name } }],
          ]"
        >
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <div v-else class="flex flex-col gap-6">
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <h2 class="text-[15px] font-semibold text-highlighted">{{ t('proxy.sites.title') }}</h2>
        <p class="mt-1 text-[11px] text-muted">{{ t('proxy.sites.description', { template: 'caddy/Caddyfile.template' }) }}</p>
        <ul class="mt-3 divide-y divide-default">
          <li v-for="d in siteRows" :key="d.name" class="flex items-center gap-3 py-3">
            <NameCell icon="i-lucide-file-code" :name="d.name" :sub="caddyfilePathOf(d.name)" class="min-w-0 flex-1" />
            <UBadge v-if="isCaddyfileCustom(d.name)" :label="t('proxy.sites.custom')" variant="soft" class="max-sm:hidden" />
            <UButton color="neutral" variant="outline" size="sm" icon="i-lucide-pencil" :label="t('common.edit')" @click="edit(d.name)" />
          </li>
        </ul>
        <p v-if="!sites.length" class="py-6 text-center text-xs text-muted">{{ t('proxy.sites.empty') }}</p>
        <TablePagination
          v-model:page="sitePage" :total="siteTotal" :from="siteFrom" :to="siteTo" :page-size="siteSize"
          class="border-t border-default" @update:page-size="sitePager.setPageSize"
        />
      </UCard>
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <h2 class="text-[15px] font-semibold text-highlighted">{{ t('proxy.main.title') }}</h2>
        <p class="mt-1 text-[11px] text-muted">{{ t('proxy.main.description') }}</p>
        <pre class="mt-3.5 max-h-[420px] overflow-auto rounded-lg bg-sage-950 p-4 font-mono text-xs leading-[19px] text-sage-200">{{ caddyfile }}</pre>
      </UCard>
    </div>

    <SideForm
      :title="t('proxy.form.title')"
      :description="t('proxy.form.description')"
      :hint="t('proxy.form.hint')"
      @submit="submit"
    >
      <UFormField :label="t('proxy.columns.source')" :error="errors.source">
        <UInput id="route-source" v-model="form.source" placeholder="preview.test" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField :label="form.type === 'alias' ? t('proxy.form.targetDomain') : t('proxy.form.targetAddress')" :error="errors.target">
        <USelect v-if="form.type === 'alias'" v-model="form.target" :items="aliasTargets" :placeholder="t('proxy.form.chooseDomain')" size="lg" class="w-full" />
        <UInput v-else v-model="form.target" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField :label="t('proxy.form.type')">
        <USelect
          v-model="form.type" size="lg" class="w-full"
          :items="[{ label: t('proxy.form.typeAddress'), value: 'address' }, { label: t('proxy.form.typeAlias'), value: 'alias' }]"
          @update:model-value="onTypeChange"
        />
      </UFormField>
      <UFormField :label="t('proxy.form.protocol')">
        <USelect v-model="form.protocol" size="lg" class="w-full" :items="[{ label: t('proxy.form.https'), value: 'https' }, { label: t('proxy.form.httpOnly'), value: 'http' }]" />
      </UFormField>
      <USwitch v-model="form.websocket" :label="t('proxy.form.websocket')" :description="t('proxy.form.websocketHelp', { command: 'add-host … --no-websocket' })" />
      <UButton type="submit" :label="t('proxy.form.submit')" size="lg" block :disabled="!form.source || !form.target" />
    </SideForm>
  </TwoColumns>

  <InfoBanner>{{ t('proxy.banner') }}</InfoBanner>
  <ConfirmModal
    :open="!!archive"
    :eyebrow="t('nav.proxy')"
    :title="t('proxy.confirm.title', { name: archive })"
    :description="archivedKind === 'proxy' ? t('proxy.confirm.descriptionRoute') : t('proxy.confirm.descriptionSite')"
    :confirm="archivedKind === 'proxy' ? t('proxy.menu.deleteRoute') : t('proxy.confirm.removeSite')"
    @update:open="(v: boolean) => { if (!v) archive = null }"
    @confirm="actions.archiveDomain(archive!)"
  />
  <CaddyfileEditor v-model:open="editorOpen" :domain="editing" />
</template>
