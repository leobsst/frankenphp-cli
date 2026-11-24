<script setup lang="ts">
defineProps<{ title: string; description: string; hint?: string }>()
defineEmits<{ submit: [] }>()
</script>

<template>
  <UCard :ui="{ body: 'p-5 sm:p-5' }">
    <h2 class="text-base font-semibold text-highlighted">{{ title }}</h2>
    <p class="mt-1.5 text-[11px] text-muted">{{ description }}</p>
    <form class="mt-6 flex flex-col gap-4" @submit.prevent="$emit('submit')">
      <slot />
    </form>
    <p v-if="hint" class="mt-3 text-[10px] leading-[15px] text-muted">{{ hint }}</p>
  </UCard>
</template>
