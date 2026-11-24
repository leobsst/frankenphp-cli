<script setup lang="ts">
import { state } from '../composables/useFm'

// The daemon answers but does not reach Docker: the containers cannot be driven until it is back (the daemon pushes
// the change on its event stream, the page then updates by itself). Hidden while the daemon itself is unreachable.
</script>

<template>
  <UAlert
    v-if="state.loaded && !state.error && !state.docker"
    color="warning" variant="soft" icon="i-lucide-triangle-alert" class="mt-6"
    title="Docker est injoignable"
    description="Démarrez Docker Desktop (ou le service Docker) : les conteneurs ne peuvent pas être pilotés d’ici là. La page se met à jour d’elle-même dès son retour."
  />
</template>
