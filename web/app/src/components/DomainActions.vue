<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DropdownMenuItem } from '@nuxt/ui'
import ConfirmModal from './ConfirmModal.vue'
import AliasModal from './AliasModal.vue'
import CaddyfileEditor from './CaddyfileEditor.vue'
import { PHP_VERSIONS, actions, containerFor, hasSiteCaddyfile, state, type Domain } from '../composables/useFm'

const props = defineProps<{ domain: Domain }>()
const { t } = useI18n()
const archiveOpen = ref(false)
const aliasOpen = ref(false)
const caddyOpen = ref(false)

const items = computed<DropdownMenuItem[][]>(() => {
  const d = props.domain
  const c = containerFor(d)
  const groups: DropdownMenuItem[][] = [[{ label: t('domainActions.open', { url: `https://${d.name}` }), icon: 'i-lucide-external-link', onSelect: () => actions.openSite(d.name) }]]
  if (d.kind === 'php') {
    groups.push([
      { type: 'label', label: t('domainActions.phpVersion') },
      ...PHP_VERSIONS.map((v) => ({
        label: `PHP ${v}`, type: 'checkbox' as const, checked: v === d.php, disabled: v === d.php,
        onSelect: () => actions.switchPhp(d.name, v),
      })),
    ])
  }
  const tools: DropdownMenuItem[] = []
  // PHP projects (caddy/sites/php-<version>/) and static hosts (caddy/sites/static/) have their own Caddyfile.
  if (hasSiteCaddyfile(d)) tools.push({ label: t('domainActions.editCaddyfile'), icon: 'i-lucide-file-code', onSelect: () => { caddyOpen.value = true } })
  if (d.kind !== 'alias') tools.push({ label: t('domainActions.regenerateCert'), icon: 'i-lucide-shield-check', disabled: state.stack !== 'running', onSelect: () => actions.regenerateCert(d.name) })
  // add-alias accepts a PHP project or a proxy host as target, not a static host.
  if (d.kind === 'php' || d.kind === 'proxy') tools.push({ label: t('domainActions.addAlias'), icon: 'i-lucide-link', onSelect: () => { aliasOpen.value = true } })
  if (tools.length) groups.push(tools)
  if (c && d.kind !== 'alias') {
    groups[groups.length - 1].push({
      label: t('domainActions.restart', { container: c.id }), icon: 'i-lucide-refresh-cw',
      disabled: c.status === 'restarting' || state.stack !== 'running' || !state.docker,
      onSelect: () => actions.restartContainer(c.id),
    })
  }
  groups.push([{ label: d.kind === 'alias' ? t('domainActions.deleteAlias') : t('domainActions.archive'), icon: 'i-lucide-archive', color: 'error', onSelect: () => { archiveOpen.value = true } }])
  return groups
})
</script>

<template>
  <UDropdownMenu :items="items" :content="{ align: 'end' }">
    <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
  </UDropdownMenu>
  <ConfirmModal
    v-model:open="archiveOpen"
    :eyebrow="t('domainActions.archiveEyebrow')"
    :title="t('domainActions.archiveTitle', { name: domain.name })"
    :description="t('domainActions.archiveDescription')"
    :note="t('domainActions.archiveNote')"
    note-tone="primary"
    :confirm="t('domainActions.archive')"
    @confirm="actions.archiveDomain(domain.name)"
  />
  <AliasModal v-model:open="aliasOpen" :target="domain.name" />
  <CaddyfileEditor v-if="hasSiteCaddyfile(domain)" v-model:open="caddyOpen" :domain="domain.name" />
</template>
