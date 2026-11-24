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
import ConfirmModal from '../components/ConfirmModal.vue'
import CaddyfileEditor from '../components/CaddyfileEditor.vue'
import TablePagination from '../components/TablePagination.vue'
import { usePagination } from '../composables/usePagination'
import { focusField } from '../composables/focus'
import { simpleDomain } from '../composables/caddyfile'
import { actions, active, aliasTarget, caddyfilePathOf, containerFor, domainStatus, hasSiteCaddyfile, isCaddyfileCustom, isProxied, servedPath, state, type Domain, type FieldErrors } from '../composables/useFm'

const tab = ref('routes')
const tabs = [{ label: 'Routes', value: 'routes' }, { label: 'Configuration Caddy', value: 'caddy' }]
const form = reactive({ source: 'preview.test', target: 'http://127.0.0.1:5173', type: 'address', protocol: 'https', websocket: true })
const errors = ref<FieldErrors>({})
const archive = ref<string | null>(null)
const archivedKind = computed(() => state.domains.find((d) => d.name === archive.value)?.kind)

const caddyUp = computed(() => state.containers.find((c) => c.kind === 'caddy')?.status === 'running')
const stats = computed(() => [
  { label: 'Routes', value: active.value.length, sub: caddyUp.value ? 'Toutes actives' : 'Caddy est arrêté' },
  { label: 'Ports publics', value: '80 / 443', sub: 'HTTP → HTTPS' },
  { label: 'Réponse', value: caddyUp.value ? `${16 + (state.cpuHistory[state.cpuHistory.length - 1] % 5)} ms` : '—', sub: 'Latence moyenne' },
])
// Alias targets: PHP projects and proxy hosts.
const aliasTargets = computed(() => active.value
  .filter((d) => d.kind === 'php' || d.kind === 'proxy')
  .map((d) => ({ label: d.kind === 'proxy' ? `${d.name} · proxy` : `${d.name} · PHP ${d.php}`, value: d.name })))

const dest = (d: Domain) => (d.kind === 'proxy' ? d.target : d.kind === 'alias' ? `→ ${d.aliasOf}` : d.kind === 'static' ? 'Fichiers' : `127.0.0.1:${containerFor(d)?.port ?? '—'}`)
// A proxy route relays WebSocket by default (Caddy); when refused, an upgrade gets a 403 (v0.11.0).
const wsAllowed = (d: Domain) => d.websocket !== false
const kind = (d: Domain) => (d.kind === 'proxy' ? `Proxy direct · HTTPS · ${wsAllowed(d) ? 'WebSocket' : 'WebSocket refusé'}` : d.kind === 'alias' ? (isProxied(d) ? 'Alias de proxy · HTTPS' : 'Alias · HTTPS') : d.kind === 'static' ? `Site statique${d.spa ? ' (SPA)' : ''} · HTTPS` : 'Site PHP · HTTPS')

/** Caddy block of a domain; a proxy host alias follows its upstream with the target as Host header. */
function caddyBlock(d: Domain) {
  // A static host is imported from its own file, served by the reverse proxy (projects folder mounted read-only).
  if (d.kind === 'static') return `# ${d.name} → ${servedPath(d)}\nimport /etc/caddy/sites/static/${simpleDomain(d.name)}_Caddyfile`
  const t = aliasTarget(d)
  const upstream = d.kind === 'proxy' ? d.target : t?.kind === 'proxy' ? `${t.target} {\n    header_up Host ${t.name}\n  }` : `frankenphp-${d.php}:80`
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

const columns: TableColumn<Domain>[] = [
  { accessorKey: 'name', header: 'Domaine source' },
  { id: 'dest', header: 'Destination', meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: 'État', meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]
</script>

<template>
  <PageHeader title="Reverse proxy" subtitle="Caddy gère le routage et la terminaison HTTPS de vos domaines.">
    <UButton icon="i-lucide-plus" label="Ajouter une route" size="lg" class="md:w-[205px]" @click="focusField('route-source')" />
  </PageHeader>
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-16 pb-2.5' }" />

  <TwoColumns>
    <ResourceTable v-if="tab === 'routes'" storage-key="proxy-routes" title="Règles de routage" :data="active" :columns="columns" search-key="name">
      <template #name-cell="{ row }"><NameCell icon="i-lucide-arrow-up-right" :name="row.original.name" :sub="kind(row.original)" /></template>
      <template #dest-cell="{ row }">{{ dest(row.original) }}</template>
      <template #state-cell="{ row }"><StateBadge :value="domainStatus(row.original)" ok="Active" /></template>
      <template #actions-cell="{ row }">
        <UDropdownMenu
          :content="{ align: 'end' }"
          :items="[
            [
              ...(hasSiteCaddyfile(row.original) ? [{ label: 'Modifier le Caddyfile', icon: 'i-lucide-file-code', onSelect: () => edit(row.original.name) }] : []),
              ...(row.original.kind === 'proxy' ? [{
                label: wsAllowed(row.original) ? 'Refuser le WebSocket' : 'Autoriser le WebSocket', icon: 'i-lucide-radio',
                onSelect: () => actions.setWebsocket(row.original.name, !wsAllowed(row.original)),
              }] : []),
              { label: 'Recharger Caddy', icon: 'i-lucide-rotate-cw', onSelect: () => actions.restartContainer('caddy') },
            ],
            [{ label: row.original.kind === 'proxy' ? 'Supprimer la route' : 'Retirer du proxy (archiver)', icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { archive = row.original.name } }],
          ]"
        >
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <div v-else class="flex flex-col gap-6">
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <h2 class="text-[15px] font-semibold text-highlighted">Caddyfiles des sites</h2>
        <p class="mt-1 text-[11px] text-muted">Un fichier par projet PHP (généré depuis caddy/Caddyfile.template) et par hôte statique, puis modifiable.</p>
        <ul class="mt-3 divide-y divide-default">
          <li v-for="d in siteRows" :key="d.name" class="flex items-center gap-3 py-3">
            <NameCell icon="i-lucide-file-code" :name="d.name" :sub="caddyfilePathOf(d.name)" class="min-w-0 flex-1" />
            <UBadge v-if="isCaddyfileCustom(d.name)" label="Personnalisé" variant="soft" class="max-sm:hidden" />
            <UButton color="neutral" variant="outline" size="sm" icon="i-lucide-pencil" label="Modifier" @click="edit(d.name)" />
          </li>
        </ul>
        <p v-if="!sites.length" class="py-6 text-center text-xs text-muted">Aucun projet PHP ni hôte statique.</p>
        <TablePagination
          v-model:page="sitePage" :total="siteTotal" :from="siteFrom" :to="siteTo" :page-size="siteSize"
          class="border-t border-default" @update:page-size="sitePager.setPageSize"
        />
      </UCard>
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <h2 class="text-[15px] font-semibold text-highlighted">Caddyfile du reverse proxy</h2>
        <p class="mt-1 text-[11px] text-muted">Lecture seule · régénéré à chaque ajout de domaine.</p>
        <pre class="mt-3.5 max-h-[420px] overflow-auto rounded-lg bg-sage-950 p-4 font-mono text-xs leading-[19px] text-sage-200">{{ caddyfile }}</pre>
      </UCard>
    </div>

    <SideForm
      title="Nouvelle route proxy"
      description="Exposez un service local sans créer de conteneur PHP."
      hint="Caddy sera rechargé. Aucun conteneur PHP supplémentaire ne sera créé."
      @submit="submit"
    >
      <UFormField label="Domaine source" :error="errors.source">
        <UInput id="route-source" v-model="form.source" placeholder="preview.test" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField :label="form.type === 'alias' ? 'Domaine cible' : 'Adresse de destination'" :error="errors.target">
        <USelect v-if="form.type === 'alias'" v-model="form.target" :items="aliasTargets" placeholder="Choisir un domaine…" size="lg" class="w-full" />
        <UInput v-else v-model="form.target" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField label="Type de route">
        <USelect
          v-model="form.type" size="lg" class="w-full"
          :items="[{ label: 'Proxy vers une adresse', value: 'address' }, { label: 'Alias d’un domaine existant', value: 'alias' }]"
          @update:model-value="onTypeChange"
        />
      </UFormField>
      <UFormField label="Protocole public">
        <USelect v-model="form.protocol" size="lg" class="w-full" :items="[{ label: 'HTTPS automatique', value: 'https' }, { label: 'HTTP uniquement', value: 'http' }]" />
      </UFormField>
      <USwitch v-model="form.websocket" label="Autoriser les connexions WebSocket" description="Refusées, une demande d’upgrade reçoit 403 (add-host … --no-websocket)." />
      <UButton type="submit" label="Ajouter la route" size="lg" block :disabled="!form.source || !form.target" />
    </SideForm>
  </TwoColumns>

  <InfoBanner>Les ports 80 et 443 sont exposés. Le trafic interne vers PHP utilise HTTP.</InfoBanner>
  <ConfirmModal
    :open="!!archive"
    eyebrow="Reverse proxy"
    :title="`Retirer ${archive} ?`"
    :description="archivedKind === 'proxy' ? 'La route sera supprimée de Caddy et du DNS, avec ses alias. Une route proxy n’a pas de Caddyfile à archiver.' : 'Le site sera retiré de Caddy et du DNS ; son Caddyfile est archivé et pourra être restauré.'"
    :confirm="archivedKind === 'proxy' ? 'Supprimer la route' : 'Retirer le site'"
    @update:open="(v: boolean) => { if (!v) archive = null }"
    @confirm="actions.archiveDomain(archive!)"
  />
  <CaddyfileEditor v-model:open="editorOpen" :domain="editing" />
</template>
