<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { answerConfirm, confirmState } from '../lib/confirm'

const { t } = useI18n()

// Destructive action requested remotely: the daemon asks for the dashboard password again (X-FM-Password).
const password = ref('')
watch(
  () => confirmState.open,
  (open) => {
    if (open) password.value = ''
  },
)
const submit = () => password.value && answerConfirm(password.value)
const onOpen = (open: boolean) => {
  if (!open) answerConfirm(null)
}
</script>

<template>
  <UModal :open="confirmState.open" :title="t('confirmPassword.title')" :description="confirmState.action" @update:open="onOpen">
    <template #body>
      <form class="space-y-4" @submit.prevent="submit">
        <p class="text-sm text-muted">{{ t('confirmPassword.help') }}</p>
        <UInput v-model="password" type="password" :placeholder="t('common.password')" autocomplete="current-password" autofocus size="xl" class="w-full" />
        <div class="flex justify-end gap-2">
          <UButton color="neutral" variant="ghost" :label="t('common.cancel')" size="lg" @click="answerConfirm(null)" />
          <UButton type="submit" color="error" :label="t('common.confirm')" size="lg" :disabled="!password" />
        </div>
      </form>
    </template>
  </UModal>
</template>
