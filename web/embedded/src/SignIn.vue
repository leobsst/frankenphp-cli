<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { session, signIn } from './session'

const { t } = useI18n()

// Dashboard password: remote access opened in a browser, or DASHBOARD_LOCAL_AUTH on this machine.
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  busy.value = true
  error.value = (await signIn(password.value)) ?? ''
  busy.value = false
  if (error.value) password.value = ''
}
</script>

<template>
  <div class="grid min-h-dvh place-items-center bg-muted p-6">
    <UCard class="w-full max-w-sm" :ui="{ body: 'p-6 sm:p-6' }">
      <form class="flex flex-col gap-5" @submit.prevent="submit">
        <div class="flex items-center gap-3">
          <span class="grid size-9 place-items-center rounded-lg bg-primary text-inverted"><UIcon name="i-lucide-square-terminal" class="size-5" /></span>
          <div>
            <h1 class="text-base font-semibold text-highlighted">frankenmanager</h1>
            <p class="text-xs text-muted">{{ session.mode === 'local' ? t('signIn.localHint', { env: 'DASHBOARD_LOCAL_AUTH' }) : t('signIn.remoteAccess') }}</p>
          </div>
        </div>
        <UFormField :label="t('signIn.password')" :error="error || undefined">
          <UInput v-model="password" type="password" autocomplete="current-password" autofocus size="lg" class="w-full" @update:model-value="error = ''" />
        </UFormField>
        <UButton type="submit" :label="t('common.signIn')" size="lg" block :loading="busy" :disabled="!password" />
      </form>
    </UCard>
  </div>
</template>
