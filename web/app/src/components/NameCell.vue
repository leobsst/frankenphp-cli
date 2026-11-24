<script setup lang="ts">
defineProps<{ icon: string; name: string; sub?: string }>()
</script>

<template>
  <div class="flex min-w-0 items-center gap-3 max-md:max-w-[44vw]">
    <UIcon :name="icon" class="size-[18px] shrink-0 text-muted" />
    <div class="min-w-0">
      <p class="truncate text-[13px] leading-[19px] font-medium text-highlighted">{{ name }}</p>
      <p v-if="sub" class="truncate text-[10px] leading-[15px] text-muted">{{ sub }}</p>
    </div>
  </div>
</template>
