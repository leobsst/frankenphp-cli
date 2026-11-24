<script setup lang="ts">
import type { DomainStatus } from '../composables/useFm'

defineProps<{ value: DomainStatus; hideLabel?: boolean }>()
const LABEL = { online: 'En ligne', offline: 'Hors ligne', restarting: 'Redémarrage…' }
</script>

<template>
  <span
    class="inline-flex items-center gap-[7px] text-xs font-medium whitespace-nowrap"
    :class="{ 'text-primary': value === 'online', 'text-error': value === 'offline', 'text-warning': value === 'restarting' }"
  >
    <span class="size-1.5 rounded-full bg-current" :class="{ 'animate-pulse': value === 'restarting' }" />
    <template v-if="!hideLabel">{{ LABEL[value] }}</template>
  </span>
</template>
