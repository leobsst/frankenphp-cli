<script setup lang="ts">
import type { DomainStatus } from '../composables/useFm'

const props = withDefaults(defineProps<{ value: DomainStatus | 'warn' | 'archived' | 'disabled'; ok?: string }>(), { ok: 'En ligne' })

const MAP = {
  online: { color: 'primary', label: () => props.ok },
  warn: { color: 'warning', label: () => 'À migrer' },
  restarting: { color: 'warning', label: () => 'Redémarrage…' },
  offline: { color: 'error', label: () => 'Hors ligne' },
  archived: { color: 'neutral', label: () => 'Archivé' },
  disabled: { color: 'neutral', label: () => 'Désactivé' },
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
