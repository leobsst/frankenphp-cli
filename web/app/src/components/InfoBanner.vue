<script setup lang="ts">
defineProps<{ tone?: 'warning' }>()
</script>

<template>
  <UAlert
    class="mt-6"
    :color="tone ?? 'primary'"
    variant="soft"
    icon="i-lucide-circle-help"
    :ui="{ root: 'px-5 py-4', description: 'text-xs font-medium', icon: 'size-[18px]' }"
  >
    <template #description><slot /></template>
  </UAlert>
</template>
