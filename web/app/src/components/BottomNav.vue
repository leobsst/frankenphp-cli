<script setup lang="ts">
const items = [
  { label: 'Aperçu', icon: 'i-lucide-layout-grid', to: '/' },
  { label: 'Projets', icon: 'i-lucide-globe', to: '/projets' },
  { label: 'Services', icon: 'i-lucide-box', to: '/conteneurs' },
  { label: 'Réglages', icon: 'i-lucide-sliders-horizontal', to: '/parametres' },
]
</script>

<template>
  <nav data-bottom-nav class="fixed inset-x-0 bottom-0 z-20 grid box-content h-16 grid-cols-4 border-t border-default bg-default pb-[max(0.875rem,var(--sab))] md:hidden">
    <RouterLink
      v-for="item in items" :key="item.to" :to="item.to"
      class="flex flex-col items-center justify-center gap-1.5 text-[10px] font-medium text-muted"
      exact-active-class="!text-primary font-semibold"
    >
      <UIcon :name="item.icon" class="size-5" />
      {{ item.label }}
    </RouterLink>
  </nav>
</template>
