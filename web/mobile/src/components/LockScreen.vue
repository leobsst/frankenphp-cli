<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { appLock, unlock } from '../lib/appLock'
import { splashDone } from '../lib/splash'

// Covers the whole app while it is locked, or hidden in the background (app switcher). role="dialog": the native tab
// bar hides itself under it (nativeTabBar.ts).
const { t } = useI18n()

const icon = computed(() => (appLock.biometry === 'faceId' ? 'i-lucide-scan-face' : appLock.biometry === 'none' ? 'i-lucide-lock-keyhole' : 'i-lucide-fingerprint'))
const label = computed(() => t(`mobile.lock.unlockWith.${appLock.biometry}`))

// Cold start: the prompt opens once the splash screen is gone.
onMounted(() => { void splashDone.then(() => { if (appLock.locked) void unlock() }) })
</script>

<template>
  <div
    v-if="appLock.locked || appLock.covered"
    role="dialog" aria-modal="true" :aria-label="t('mobile.lock.title')"
    class="fixed inset-0 z-[1000] flex flex-col items-center justify-center gap-6 bg-default px-safe pt-safe pb-safe"
  >
    <!-- Bound: Vite leaves it to public/ (served next to index.html). -->
    <img :src="'assets/icons/icon-192.webp'" alt="" class="size-20 rounded-[22%] shadow-lg">
    <div v-if="appLock.locked" class="flex flex-col items-center gap-6 text-center">
      <div>
        <p class="text-lg font-semibold text-highlighted">{{ t('mobile.lock.title') }}</p>
        <p class="mt-1 text-sm text-muted">{{ t('mobile.lock.help') }}</p>
      </div>
      <UButton :icon="icon" :label="label" size="xl" :loading="appLock.busy" @click="unlock()" />
    </div>
  </div>
</template>
