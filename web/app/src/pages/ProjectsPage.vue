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
import DomainActions from '../components/DomainActions.vue'
import { focusField } from '../composables/focus'
import { defaultRoot } from '../composables/caddyfile'
import type { DropdownMenuItem } from '@nuxt/ui'
import { DEPRECATED_PHP, PHP_VERSIONS, PROJECTS_ROOT, actions, active, archived, domainIcon, domainStatus, envLabel, isProxied, phpVersions, restoreCommand, servedPath, type Domain, type FieldErrors, type NewProject, type Php } from '../composables/useFm'

const tab = ref('active')
const tabs = [{ label: 'Domaines actifs', value: 'active' }, { label: 'Archives', value: 'archived' }]

const form = reactive({ name: 'blog.test', type: 'php' as NewProject['type'], root: '', php: '8.5' as Php, spa: false, alias: 'www.blog.test', ssl: true })
const isStatic = computed(() => form.type === 'static')
const typeItems = [{ label: 'Projet PHP', value: 'php' }, { label: 'Site statique', value: 'static' }]
// Dossier servi par défaut, affiché en placeholder : `--root` est facultatif.
const rootPlaceholder = computed(() => defaultRoot(form.name.trim().toLowerCase() || 'blog.test', isStatic.value))
const command = computed(() => {
  const name = form.name.trim().toLowerCase() || '<domaine>'
  const root = form.root.trim() ? ` --root ${form.root.trim()}` : ''
  return isStatic.value ? `add-host ${name} --static${root}${form.spa ? ' --spa' : ''}` : `add-host ${name} --php ${form.php}${root}`
})
const errors = ref<FieldErrors>({})
const busy = ref(false)
const phpItems = PHP_VERSIONS.map((v) => ({ label: `PHP ${v}${DEPRECATED_PHP.includes(v) ? ' (dépréciée)' : ''}`, value: v }))

async function submit() {
  busy.value = true
  const err = await actions.addProject({ ...form, name: form.name.trim().toLowerCase(), alias: isStatic.value ? undefined : form.alias.trim().toLowerCase() || undefined })
  busy.value = false
  errors.value = err ?? {}
  if (!err) Object.assign(form, { name: '', root: '', alias: '', spa: false })
}

const count = (k: Domain['kind']) => active.value.filter((d) => d.kind === k).length
const stats = computed(() => [
  { label: 'Domaines', value: active.value.length, sub: `${count('php')} PHP · ${count('static')} statiques · ${count('proxy')} proxy${count('alias') ? ` · ${count('alias')} alias` : ''}` },
  { label: 'Versions PHP', value: phpVersions.value.length, sub: phpVersions.value.join(' / ') },
  { label: 'HTTPS', value: `${active.value.length} / ${active.value.length}`, sub: 'Certificats valides' },
])
const deprecated = computed(() => active.value.filter((d) => d.kind === 'php' && DEPRECATED_PHP.includes(d.php!)))
const isDeprecated = (d: Domain) => !isProxied(d) && DEPRECATED_PHP.includes(d.php!)
const sub = (d: Domain) => (d.kind === 'proxy' ? d.target : d.kind === 'alias' ? `alias de ${d.aliasOf}` : servedPath(d))
/** Dossier d'archive (v0.11.0) : sites/archive/php-<version>/ ou sites/archive/static/. */
const archiveDir = (d: Domain) => (d.kind === 'static' ? 'archive/static/' : `archive/php-${d.php}/`)

/** `restore-host --php <version>` (au choix, pas forcément celle d'origine) ou `restore-host --static`. */
function restoreMenu(d: Domain): DropdownMenuItem[][] {
  if (d.kind === 'static') return [[{ label: 'Restaurer l’hôte statique', icon: 'i-lucide-archive-restore', onSelect: () => actions.restoreDomain(d.name) }]]
  return [[
    { label: `Restaurer en PHP ${d.php}`, icon: 'i-lucide-archive-restore', onSelect: () => actions.restoreDomain(d.name, d.php) },
    {
      label: 'Restaurer avec une autre version', icon: 'i-lucide-git-branch',
      children: PHP_VERSIONS.filter((v) => v !== d.php).map((v) => ({ label: `PHP ${v}${DEPRECATED_PHP.includes(v) ? ' (dépréciée)' : ''}`, onSelect: () => actions.restoreDomain(d.name, v) })),
    },
  ]]
}

const columns: TableColumn<Domain>[] = [
  { accessorKey: 'name', header: 'Domaine / chemin' },
  { id: 'env', header: 'Environnement', meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: 'État', meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]
</script>

<template>
  <PageHeader title="Projets & domaines" subtitle="Vos projets PHP, sites statiques, alias et domaines locaux sur cette machine.">
    <UButton icon="i-lucide-plus" label="Ajouter un projet" size="lg" class="md:w-[205px]" @click="focusField('project-name')" />
  </PageHeader>
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-16 pb-2.5' }" />

  <TwoColumns>
    <ResourceTable
      :title="tab === 'active' ? 'Domaines enregistrés' : 'Domaines archivés'"
      :data="tab === 'active' ? active : archived"
      :columns="columns"
      search-key="name"
      :empty="tab === 'active' ? 'Aucun domaine.' : 'Aucun domaine archivé.'"
    >
      <template #name-cell="{ row }">
        <NameCell :icon="domainIcon(row.original)" :name="row.original.name" :sub="tab === 'archived' ? `caddy/sites/${archiveDir(row.original)}` : sub(row.original)" />
      </template>
      <template #env-cell="{ row }">
        <span :class="isDeprecated(row.original) ? 'text-warning' : ''">{{ envLabel(row.original) }}</span>
      </template>
      <template #state-cell="{ row }">
        <StateBadge v-if="tab === 'archived'" value="archived" />
        <StateBadge v-else :value="domainStatus(row.original) === 'online' && isDeprecated(row.original) ? 'warn' : domainStatus(row.original)" />
      </template>
      <template #actions-cell="{ row }">
        <DomainActions v-if="tab === 'active'" :domain="row.original" />
        <UDropdownMenu v-else :items="restoreMenu(row.original)" :content="{ align: 'end' }">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
        </UDropdownMenu>
      </template>
    </ResourceTable>

    <SideForm
      :title="isStatic ? 'Nouveau site statique' : 'Nouveau projet PHP'"
      :description="isStatic ? 'Un site HTML ou le build d’une app front, servi par le reverse proxy sans PHP.' : 'Créez un domaine local avec HTTPS automatique.'"
      :hint="isStatic ? 'Le Caddyfile du site est créé dans caddy/sites/static/, modifiable ensuite. Un hôte statique n’a pas d’alias.' : 'Les alias suivent la version PHP du domaine cible.'"
      @submit="submit"
    >
      <UFormField label="Type de site">
        <USelect v-model="form.type" :items="typeItems" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField label="Nom de domaine" :error="errors.name">
        <UInput id="project-name" v-model="form.name" placeholder="blog.test" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField label="Dossier servi (facultatif)" :help="`Relatif à ${PROJECTS_ROOT} ; son premier dossier, le projet, doit exister.`" :error="errors.root" :ui="{ help: 'text-[11px]' }">
        <UInput v-model="form.root" :placeholder="rootPlaceholder" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField v-if="!isStatic" label="Version de PHP">
        <USelect v-model="form.php" :items="phpItems" size="lg" class="w-full" />
      </UFormField>
      <UFormField v-if="!isStatic" label="Alias (facultatif)" :error="errors.alias">
        <UInput v-model="form.alias" placeholder="www.blog.test" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <USwitch v-if="isStatic" v-model="form.spa" label="Single-page app" description="Les chemins inconnus renvoient /index.html (routage côté client)." />
      <USwitch v-else v-model="form.ssl" label="Générer le certificat SSL" />
      <code class="truncate font-mono text-[10px] text-dimmed">$ frankenmanager {{ command }}</code>
      <UButton type="submit" :label="isStatic ? 'Ajouter le site' : 'Créer le projet'" size="lg" block :loading="busy" :disabled="!form.name" />
    </SideForm>
  </TwoColumns>

  <InfoBanner v-if="tab === 'archived'">
    Restaurez un projet PHP avec la version de votre choix ({{ archived[0] ? `frankenmanager ${restoreCommand(archived[0])}` : 'restore-host --php <version>' }}), un site statique avec <span class="font-mono">--static</span> : son dossier servi et son repli SPA sont relus dans son Caddyfile.
  </InfoBanner>
  <InfoBanner v-else-if="deprecated.length">
    PHP {{ DEPRECATED_PHP.join(', ') }} est déprécié. Passez {{ deprecated.map((d) => d.name).join(', ') }} à une version plus récente.
  </InfoBanner>
  <InfoBanner v-else>Toutes vos versions PHP sont à jour.</InfoBanner>
</template>
