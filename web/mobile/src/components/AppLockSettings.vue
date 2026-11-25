<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { toast } from '@app/composables/useFm'
import { appLock, setAppLock } from '../lib/appLock'

// Settings card of the mobile app (shell.settingsCard): the lock. Applied right away, after the owner authenticated,
// unlike the other preferences, which wait for Save.
const { t } = useI18n()
const busy = ref(false)
const name = computed(() => t(`mobile.lock.biometry.${appLock.biometry}`))

async function toggle(on: boolean) {
  busy.value = true
  try {
    if (await setAppLock(on)) toast('success', on ? t('mobile.lock.enabled', { name: name.value }) : t('mobile.lock.disabled'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <UCard :ui="{ body: 'p-5 sm:p-5' }">
    <h2 class="text-[17px] font-semibold text-highlighted">{{ t('mobile.lock.settingsTitle') }}</h2>
    <div class="mt-5 flex items-center gap-4">
      <div class="flex-1">
        <p class="text-xs font-medium text-highlighted">{{ t('mobile.lock.toggle', { name }) }}</p>
        <p class="mt-1 text-[11px] text-muted">{{ t('mobile.lock.toggleHelp', { name }) }}</p>
      </div>
      <USwitch
        :model-value="appLock.enabled" :disabled="!appLock.supported || busy" :loading="busy"
        :aria-label="t('mobile.lock.toggle', { name })" @update:model-value="toggle"
      />
    </div>
    <p v-if="!appLock.supported" class="mt-3.5 flex gap-1.5 text-[11px] text-muted">
      <UIcon name="i-lucide-info" class="mt-px size-3.5 shrink-0" />
      <span>{{ t('mobile.lock.unsupported') }}</span>
    </p>
    <p class="mt-4 text-[10px] text-muted">{{ t('mobile.lock.immediate') }}</p>
  </UCard>
</template>
