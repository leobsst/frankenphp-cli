<script setup lang="ts">
import { useMediaQuery } from '@vueuse/core'
import SidebarContent from './SidebarContent.vue'

// Tablette : barre d'icônes de 72px ; bureau : barre complète de 228px.
const collapsed = useMediaQuery('(max-width: 1179px)')
</script>

<template>
  <aside class="fixed inset-y-0 left-0 z-20 hidden w-[72px] flex-col border-r border-default bg-default px-3 pt-[calc(1.25rem+var(--sat))] md:flex xl:w-[228px] xl:pt-[calc(26px+var(--sat))]">
    <SidebarContent :collapsed="collapsed" />
  </aside>
</template>
