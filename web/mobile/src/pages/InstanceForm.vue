<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ApiError, health, hostInfo, login, logout } from '@app/lib/api'
import { deviceName } from '../lib/device'
import { findInstance, newId, normalizeUrl, setCurrent, upsertInstance } from '../lib/instances'
import { openPairing, scanPairing } from '../lib/pairing'

// Adds an instance, or signs in again to an instance whose session expired (props.id).
const props = defineProps<{ id?: string }>()
const { t } = useI18n()
const router = useRouter()
const existing = computed(() => (props.id ? findInstance(props.id) : undefined))

const url = ref(existing.value?.url.replace('https://', '') ?? '')
const name = ref(existing.value?.name ?? '')
const password = ref('')
const busy = ref(false)
const error = ref('')

async function submit() {
  error.value = ''
  let origin: string
  try {
    origin = normalizeUrl(url.value)
  } catch (e) {
    error.value = (e as Error).message
    return
  }
  busy.value = true
  try {
    await health(origin)
    const { token, session } = await login(origin, password.value, await deviceName())
    const host = await hostInfo(origin, token).catch(() => null)
    // Signing in again: the previous session is closed on the daemon side, if it is still open.
    if (existing.value?.token) await logout(existing.value)
    const id = existing.value?.id ?? newId()
    await upsertInstance({
      id,
      name: name.value.trim() || host?.hostname || origin.replace('https://', ''),
      url: origin,
      token,
      session: session.id,
      version: host?.version,
      expired: false,
      added: existing.value?.added ?? Date.now(),
    })
    await setCurrent(id)
    router.replace('/')
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) error.value = t('mobile.form.wrongPassword')
    else if (e instanceof ApiError && e.status === 403) error.value = t('mobile.errors.forbidden', { message: e.message })
    else error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

/** Pairing with the QR code of the dashboard, instead of the address and password. */
async function scan() {
  error.value = ''
  try {
    const link = await scanPairing()
    if (link) await openPairing(router, link)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}
</script>

<template>
  <div class="h-dvh overflow-y-auto pt-safe pb-safe">
    <header class="px-safe flex items-center gap-2 pt-3 pb-2">
      <UButton icon="i-lucide-chevron-left" color="neutral" variant="ghost" size="lg" :aria-label="t('common.back')" to="/instances" />
      <h1 class="text-lg font-semibold text-highlighted">{{ existing ? t('mobile.instances.reconnect') : t('mobile.form.add') }}</h1>
    </header>

    <main class="px-safe space-y-5 py-3">
      <div class="space-y-2">
        <UButton icon="i-lucide-scan-qr-code" :label="t('mobile.pair.scan')" size="xl" block @click="scan" />
        <p class="text-center text-xs text-muted">{{ t('mobile.form.scanHelp') }}</p>
      </div>

      <USeparator :label="t('mobile.form.orPassword')" />

      <form class="space-y-5" @submit.prevent="submit">
        <UFormField :label="t('mobile.form.address')" :help="t('mobile.form.addressHelp')" required>
          <UInput
            v-model="url"
            placeholder="192.168.1.42:9443"
            size="xl"
            class="w-full"
            inputmode="url"
            autocapitalize="off"
            autocorrect="off"
            spellcheck="false"
            :disabled="!!existing"
          />
        </UFormField>
        <UFormField v-if="!existing" :label="t('common.name')" :help="t('mobile.form.nameHelp')">
          <UInput v-model="name" placeholder="MacBook-Pro" size="xl" class="w-full" />
        </UFormField>
        <UFormField :label="t('signIn.password')" required>
          <UInput v-model="password" type="password" size="xl" class="w-full" autocomplete="current-password" />
        </UFormField>

        <UAlert v-if="error" color="error" variant="subtle" icon="i-lucide-circle-alert" :description="error" />

        <UButton type="submit" :label="t('common.signIn')" color="neutral" variant="outline" size="xl" block :loading="busy" :disabled="!url || !password" />

        <div class="rounded-lg bg-elevated p-4 text-sm text-muted space-y-2">
          <p class="font-medium text-toned">{{ t('mobile.form.firstTime.title') }}</p>
          <ol class="list-decimal pl-5 space-y-1">
            <i18n-t keypath="mobile.form.firstTime.access" tag="li" scope="global">
              <template #access><code>frankenmanager configuration set DASHBOARD_ACCESS lan</code></template>
              <template #password><code>frankenmanager daemon password</code></template>
            </i18n-t>
            <i18n-t keypath="mobile.form.firstTime.ca" tag="li" scope="global">
              <template #trustCa><code>frankenmanager trust-ca on</code></template>
            </i18n-t>
            <li>{{ t('mobile.form.firstTime.ios') }}</li>
          </ol>
        </div>
      </form>
    </main>
  </div>
</template>
