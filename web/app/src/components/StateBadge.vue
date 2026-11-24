<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { DomainStatus } from '../composables/useFm'

// `ok`: label of the online state (e.g. "Approved"), "Online" by default.
const props = defineProps<{ value: DomainStatus | 'warn' | 'archived' | 'disabled'; ok?: string }>()
const { t } = useI18n()

const MAP = {
  online: { color: 'primary', label: () => props.ok ?? t('stateBadge.online') },
  warn: { color: 'warning', label: () => t('stateBadge.warn') },
  restarting: { color: 'warning', label: () => t('stateBadge.restarting') },
  offline: { color: 'error', label: () => t('stateBadge.offline') },
  archived: { color: 'neutral', label: () => t('stateBadge.archived') },
  disabled: { color: 'neutral', label: () => t('stateBadge.disabled') },
} as const
</script>

<template>
  <UBadge
    :color="MAP[value].color" variant="soft" size="md"
    :label="MAP[value].label()"
    class="min-w-[57px] justify-center"
    :class="{ 'animate-pulse': value === 'restarting' }"
  />
</template>
