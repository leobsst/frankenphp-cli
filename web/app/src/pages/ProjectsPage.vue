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
import { formatNumber } from '../i18n'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
/** Plural count message, the number formatted in the active language. */
const plural = (key: string, n: number) => t(key, { n: formatNumber(n) }, n)

const tab = ref('active')
const tabs = computed(() => [{ label: t('projects.tabs.active'), value: 'active' }, { label: t('projects.tabs.archived'), value: 'archived' }])

const form = reactive({ name: 'blog.test', type: 'php' as NewProject['type'], root: '', php: '8.5' as Php, spa: false, alias: 'www.blog.test', ssl: true })
const isStatic = computed(() => form.type === 'static')
const typeItems = computed(() => [{ label: t('projects.types.php'), value: 'php' }, { label: t('projects.types.static'), value: 'static' }])
// Default served folder, shown as placeholder: `--root` is optional.
const rootPlaceholder = computed(() => defaultRoot(form.name.trim().toLowerCase() || 'blog.test', isStatic.value))
const command = computed(() => {
  const name = form.name.trim().toLowerCase() || t('projects.form.domainArg')
  const root = form.root.trim() ? ` --root ${form.root.trim()}` : ''
  return isStatic.value ? `add-host ${name} --static${root}${form.spa ? ' --spa' : ''}` : `add-host ${name} --php ${form.php}${root}`
})
const errors = ref<FieldErrors>({})
const busy = ref(false)
/** PHP version label, flagged when deprecated. */
const phpLabel = (v: Php) => (DEPRECATED_PHP.includes(v) ? t('projects.phpDeprecated', { version: v }) : `PHP ${v}`)
const phpItems = computed(() => PHP_VERSIONS.map((v) => ({ label: phpLabel(v), value: v })))

async function submit() {
  busy.value = true
  const err = await actions.addProject({ ...form, name: form.name.trim().toLowerCase(), alias: isStatic.value ? undefined : form.alias.trim().toLowerCase() || undefined })
  busy.value = false
  errors.value = err ?? {}
  if (!err) Object.assign(form, { name: '', root: '', alias: '', spa: false })
}

const count = (k: Domain['kind']) => active.value.filter((d) => d.kind === k).length
const stats = computed(() => [
  {
    label: t('projects.stats.domains'), value: active.value.length,
    sub: [
      plural('projects.stats.php', count('php')), plural('projects.stats.static', count('static')), plural('projects.stats.proxy', count('proxy')),
      ...(count('alias') ? [plural('projects.stats.alias', count('alias'))] : []),
    ].join(' · '),
  },
  { label: t('projects.stats.phpVersions'), value: phpVersions.value.length, sub: phpVersions.value.join(' / ') },
  { label: 'HTTPS', value: `${active.value.length} / ${active.value.length}`, sub: t('projects.stats.validCertificates') },
])
const deprecated = computed(() => active.value.filter((d) => d.kind === 'php' && DEPRECATED_PHP.includes(d.php!)))
const isDeprecated = (d: Domain) => !isProxied(d) && DEPRECATED_PHP.includes(d.php!)
const sub = (d: Domain) => (d.kind === 'proxy' ? d.target : d.kind === 'alias' ? t('projects.aliasOf', { name: d.aliasOf }) : servedPath(d))
/** Archive folder (v0.11.0): sites/archive/php-<version>/ or sites/archive/static/. */
const archiveDir = (d: Domain) => (d.kind === 'static' ? 'archive/static/' : `archive/php-${d.php}/`)

/** `restore-host --php <version>` (any version, not necessarily the original one) or `restore-host --static`. */
function restoreMenu(d: Domain): DropdownMenuItem[][] {
  if (d.kind === 'static') return [[{ label: t('projects.restore.static'), icon: 'i-lucide-archive-restore', onSelect: () => actions.restoreDomain(d.name) }]]
  return [[
    { label: t('projects.restore.samePhp', { version: d.php }), icon: 'i-lucide-archive-restore', onSelect: () => actions.restoreDomain(d.name, d.php) },
    {
      label: t('projects.restore.otherVersion'), icon: 'i-lucide-git-branch',
      children: PHP_VERSIONS.filter((v) => v !== d.php).map((v) => ({ label: phpLabel(v), onSelect: () => actions.restoreDomain(d.name, v) })),
    },
  ]]
}

const columns = computed<TableColumn<Domain>[]>(() => [
  { accessorKey: 'name', header: t('projects.columns.name') },
  { id: 'env', header: t('projects.columns.env'), meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: t('common.status'), meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])
</script>

<template>
  <PageHeader :title="t('nav.projects')" :subtitle="t('projects.subtitle')">
    <UButton icon="i-lucide-plus" :label="t('projects.add')" size="lg" class="md:w-[205px]" @click="focusField('project-name')" />
  </PageHeader>
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-16 pb-2.5' }" />

  <TwoColumns>
    <ResourceTable
      storage-key="projects"
      :title="tab === 'active' ? t('projects.table.active') : t('projects.table.archived')"
      :data="tab === 'active' ? active : archived"
      :columns="columns"
      search-key="name"
      :empty="tab === 'active' ? t('projects.table.empty') : t('projects.table.emptyArchived')"
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
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
        </UDropdownMenu>
      </template>
    </ResourceTable>

    <SideForm
      :title="isStatic ? t('projects.form.titleStatic') : t('projects.form.titlePhp')"
      :description="isStatic ? t('projects.form.descriptionStatic') : t('projects.form.descriptionPhp')"
      :hint="isStatic ? t('projects.form.hintStatic', { path: 'caddy/sites/static/' }) : t('projects.form.hintPhp')"
      @submit="submit"
    >
      <UFormField :label="t('projects.form.type')">
        <USelect v-model="form.type" :items="typeItems" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField :label="t('projects.form.name')" :error="errors.name">
        <UInput id="project-name" v-model="form.name" placeholder="blog.test" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField :label="t('projects.form.root')" :help="t('projects.form.rootHelp', { root: PROJECTS_ROOT })" :error="errors.root" :ui="{ help: 'text-[11px]' }">
        <UInput v-model="form.root" :placeholder="rootPlaceholder" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField v-if="!isStatic" :label="t('projects.form.php')">
        <USelect v-model="form.php" :items="phpItems" size="lg" class="w-full" />
      </UFormField>
      <UFormField v-if="!isStatic" :label="t('projects.form.alias')" :error="errors.alias">
        <UInput v-model="form.alias" placeholder="www.blog.test" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <USwitch v-if="isStatic" v-model="form.spa" :label="t('projects.form.spa')" :description="t('projects.form.spaHelp', { index: '/index.html' })" />
      <USwitch v-else v-model="form.ssl" :label="t('projects.form.ssl')" />
      <code class="truncate font-mono text-[10px] text-dimmed">$ frankenmanager {{ command }}</code>
      <UButton type="submit" :label="isStatic ? t('projects.form.submitStatic') : t('projects.form.submitPhp')" size="lg" block :loading="busy" :disabled="!form.name" />
    </SideForm>
  </TwoColumns>

  <InfoBanner v-if="tab === 'archived'">
    <i18n-t keypath="projects.banner.archived" tag="span" scope="global">
      <template #command>{{ archived[0] ? `frankenmanager ${restoreCommand(archived[0])}` : 'restore-host --php <version>' }}</template>
      <template #flag><span class="font-mono">--static</span></template>
    </i18n-t>
  </InfoBanner>
  <InfoBanner v-else-if="deprecated.length">
    {{ t('projects.banner.deprecated', { versions: DEPRECATED_PHP.join(', '), domains: deprecated.map((d) => d.name).join(', ') }) }}
  </InfoBanner>
  <InfoBanner v-else>{{ t('projects.banner.upToDate') }}</InfoBanner>
</template>
