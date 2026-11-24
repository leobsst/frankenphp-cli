<script setup lang="ts">
import ConfirmModal from './ConfirmModal.vue'
import { actions, state } from '../composables/useFm'

const open = defineModel<boolean>('open', { default: false })
</script>

<template>
  <ConfirmModal
    v-model:open="open"
    eyebrow="Confirmation"
    title="Arrêter toute la stack ?"
    :description="`Les ${state.containers.length} conteneurs seront arrêtés. Les domaines locaux seront indisponibles jusqu’au prochain démarrage.`"
    note="Les volumes et les données seront conservés."
    confirm="Arrêter la stack"
    @confirm="actions.stopStack()"
  />
</template>
