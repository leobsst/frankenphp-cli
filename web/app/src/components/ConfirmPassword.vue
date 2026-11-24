<script setup lang="ts">
import { ref, watch } from 'vue'
import { answerConfirm, confirmState } from '../lib/confirm'

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
  <UModal :open="confirmState.open" title="Mot de passe du dashboard" :description="confirmState.action" @update:open="onOpen">
    <template #body>
      <form class="space-y-4" @submit.prevent="submit">
        <p class="text-sm text-muted">À distance, cette action redemande le mot de passe du dashboard.</p>
        <UInput v-model="password" type="password" placeholder="Mot de passe" autocomplete="current-password" autofocus size="xl" class="w-full" />
        <div class="flex justify-end gap-2">
          <UButton color="neutral" variant="ghost" label="Annuler" size="lg" @click="answerConfirm(null)" />
          <UButton type="submit" color="error" label="Confirmer" size="lg" :disabled="!password" />
        </div>
      </form>
    </template>
  </UModal>
</template>
