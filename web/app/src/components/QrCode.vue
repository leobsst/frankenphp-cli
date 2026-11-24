<script setup lang="ts">
import { computed } from 'vue'
import { encode } from 'uqr'

/** QR code généré localement (aucun appel réseau), dessiné en SVG avec la couleur du texte courant. */
const props = defineProps<{ value: string; label: string }>()

const qr = computed(() => encode(props.value, { border: 2 }))
const path = computed(() => qr.value.data
  .flatMap((row, y) => row.map((on, x) => (on ? `M${x} ${y}h1v1h-1z` : '')))
  .join(''))
</script>

<template>
  <svg
    :viewBox="`0 0 ${qr.size} ${qr.size}`" role="img" :aria-label="label"
    class="rounded-lg bg-white text-black" shape-rendering="crispEdges"
  >
    <path :d="path" fill="currentColor" />
  </svg>
</template>
