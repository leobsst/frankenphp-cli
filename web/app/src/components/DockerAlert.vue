<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { state } from '../composables/useFm'

const { t } = useI18n()

// The daemon answers but does not reach Docker: the containers cannot be driven until it is back (the daemon pushes
// the change on its event stream, the page then updates by itself). Hidden while the daemon itself is unreachable.
</script>

<template>
  <UAlert
    v-if="state.loaded && !state.error && !state.docker"
    color="warning" variant="soft" icon="i-lucide-triangle-alert" class="mt-6"
    :title="t('dockerAlert.title')"
    :description="t('dockerAlert.description')"
  />
</template>
