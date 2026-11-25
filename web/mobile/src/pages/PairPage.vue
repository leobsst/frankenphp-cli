<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ApiError, hostInfo, logout, pair } from '@app/lib/api'
import { deviceName } from '../lib/device'
import { findByUrl, newId, setCurrent, upsertInstance } from '../lib/instances'
import { formatFingerprint, parsePairLink, type PairRequest } from '../lib/pairing'

/**
 * Confirmation of a pairing link (scanned in the app, or opened by the system camera through the frankenmanager://
 * scheme): shows what the phone is about to trust, then exchanges the one-time code for a session token.
 */
const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const parsed = computed((): { request: PairRequest; error?: undefined } | { request?: undefined; error: string } => {
  try {
    return { request: parsePairLink(String(route.query.link ?? '')) }
  } catch (e) {
    return { error: e instanceof Error ? e.message : String(e) }
  }
})
const request = computed(() => parsed.value.request)
/** Instance already in the app at this address: pairing replaces its session. */
const existing = computed(() => (request.value ? findByUrl(request.value.url) : undefined))

const busy = ref(false)
const error = ref('')

function explain(e: unknown) {
  if (!(e instanceof ApiError)) return e instanceof Error ? e.message : String(e)
  switch (e.status) {
    // Status 0: the request did not reach the daemon; the message already says so.
    case 0: return t('mobile.pair.errors.unreachable', { message: e.message })
    case 401: return t('mobile.pair.errors.invalidCode')
    case 403: return t('mobile.errors.forbidden', { message: e.message })
    case 404: return t('mobile.pair.errors.notSupported')
    case 429: return t('mobile.pair.errors.tooMany')
    default: return e.message
  }
}

async function confirm() {
  const req = request.value
  if (!req) return
  busy.value = true
  error.value = ''
  try {
    const { token, session } = await pair(req.url, req.code, await deviceName())
    const host = await hostInfo(req.url, token).catch(() => null)
    const prev = findByUrl(req.url)
    if (prev?.token) await logout(prev)
    const id = prev?.id ?? newId()
    await upsertInstance({
      id,
      name: prev?.name || req.name || host?.hostname || req.url.replace('https://', ''),
      url: req.url,
      token,
      session: session.id,
      version: host?.version,
      expired: false,
      added: prev?.added ?? Date.now(),
    })
    await setCurrent(id)
    await router.replace('/')
  } catch (e) {
    error.value = explain(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="fixed inset-0 overflow-y-auto pt-safe pb-safe">
    <header class="px-safe flex items-center gap-2 pt-3 pb-2">
      <UButton icon="i-lucide-chevron-left" color="neutral" variant="ghost" size="lg" :aria-label="t('common.back')" to="/instances" />
      <h1 class="text-lg font-semibold text-highlighted">{{ t('mobile.pair.title') }}</h1>
    </header>

    <main class="px-safe space-y-5 py-3">
      <template v-if="request">
        <div class="rounded-lg border border-default bg-default p-4 space-y-4">
          <div class="flex items-center gap-3">
            <div class="size-10 shrink-0 rounded-md bg-primary/10 grid place-items-center text-primary">
              <UIcon name="i-lucide-server-cog" class="size-5" />
            </div>
            <div class="min-w-0">
              <p class="font-semibold text-highlighted truncate">{{ request.name || t('mobile.pair.unnamed') }}</p>
              <p class="text-sm text-muted truncate">{{ request.url.replace('https://', '') }}</p>
            </div>
          </div>
          <div class="space-y-1">
            <p class="text-xs font-medium text-toned">{{ t('mobile.pair.fingerprint') }}</p>
            <p v-if="request.ca" class="font-mono text-xs break-all text-muted">{{ formatFingerprint(request.ca) }}</p>
            <p v-else class="text-xs text-muted">{{ t('mobile.pair.noFingerprint') }}</p>
          </div>
        </div>

        <p class="text-sm text-muted">{{ request.ca ? t('mobile.pair.verifyWithCa') : t('mobile.pair.verify') }}</p>

        <UAlert
          v-if="existing"
          color="warning" variant="subtle" icon="i-lucide-refresh-cw"
          :description="t('mobile.pair.existing', { name: existing.name })"
        />
        <UAlert v-if="error" color="error" variant="subtle" icon="i-lucide-circle-alert" :description="error" />

        <div class="space-y-2">
          <UButton icon="i-lucide-link" :label="t('mobile.pair.confirm')" size="xl" block :loading="busy" @click="confirm" />
          <UButton :label="t('common.cancel')" color="neutral" variant="ghost" size="xl" block :disabled="busy" to="/instances" />
        </div>
      </template>

      <template v-else>
        <UAlert color="error" variant="subtle" icon="i-lucide-qr-code" :title="t('mobile.pair.impossible')" :description="parsed.error" />
        <UButton :label="t('mobile.pair.backToList')" color="neutral" variant="outline" size="xl" block to="/instances" />
      </template>
    </main>
  </div>
</template>
