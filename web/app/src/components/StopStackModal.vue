<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import ConfirmModal from './ConfirmModal.vue'
import { actions, state } from '../composables/useFm'
import { formatNumber } from '../i18n'

const open = defineModel<boolean>('open', { default: false })
const { t } = useI18n()
</script>

<template>
  <ConfirmModal
    v-model:open="open"
    :eyebrow="t('stopStackModal.eyebrow')"
    :title="t('stopStackModal.title')"
    :description="t('stopStackModal.description', { n: formatNumber(state.containers.length) }, state.containers.length)"
    :note="t('stopStackModal.note')"
    :confirm="t('stopStackModal.confirm')"
    @confirm="actions.stopStack()"
  />
</template>
