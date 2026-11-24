<script setup lang="ts">
defineProps<{ title: string; subtitle: string; large?: boolean }>()
</script>

<template>
  <div class="flex flex-col gap-4 md:flex-row md:flex-wrap md:items-start">
    <div class="min-w-0">
      <h1 class="font-semibold tracking-tight text-highlighted" :class="large ? 'text-2xl md:text-[28px] md:leading-[41px]' : 'text-2xl md:text-[28px] md:leading-[41px]'">{{ title }}</h1>
      <p class="mt-1 text-muted" :class="large ? 'text-sm' : 'text-xs'">{{ subtitle }}</p>
    </div>
    <div v-if="$slots.default" class="flex gap-3 md:ml-auto max-md:*:flex-1 max-md:*:justify-center" :class="large ? 'md:mt-2' : ''">
      <slot />
    </div>
  </div>
</template>
