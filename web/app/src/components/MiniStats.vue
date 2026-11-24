<script setup lang="ts">
defineProps<{ items: { label: string; value: string | number; sub: string }[] }>()
</script>

<template>
  <div class="mt-6 grid grid-cols-2 gap-2.5 md:grid-cols-3 md:gap-3">
    <UCard
      v-for="s in items" :key="s.label"
      class="min-w-0 last:odd:max-md:col-span-2"
      :ui="{ body: 'p-0 sm:p-0 px-3.5 pt-2.5 pb-2 sm:px-3.5 sm:pt-2.5 sm:pb-2' }"
    >
      <p class="text-xs font-medium text-muted">{{ s.label }}</p>
      <p class="truncate text-2xl leading-[34px] font-semibold text-highlighted">{{ s.value }}</p>
      <p class="truncate text-[10px] text-muted">{{ s.sub }}</p>
    </UCard>
  </div>
</template>
