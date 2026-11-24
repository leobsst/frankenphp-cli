<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, hostInfo, logout, pair, UNREACHABLE } from '@app/lib/api'
import { deviceName } from '../lib/device'
import { findByUrl, newId, setCurrent, upsertInstance } from '../lib/instances'
import { formatFingerprint, parsePairLink, type PairRequest } from '../lib/pairing'

/**
 * Confirmation of a pairing link (scanned in the app, or opened by the system camera through the frankenmanager://
 * scheme): shows what the phone is about to trust, then exchanges the one-time code for a session token.
 */
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
    case 0: return `${UNREACHABLE} Le téléphone doit aussi être sur le même réseau que la machine.`
    case 401: return 'Code d’appairage invalide, expiré ou déjà utilisé : affichez-en un nouveau dans le dashboard.'
    case 403: return `${e.message} Vérifiez DASHBOARD_ACCESS et DASHBOARD_ALLOWED_IPS.`
    case 404: return 'Cette instance ne propose pas l’appairage par QR code : mettez FrankenManager à jour sur la machine.'
    case 429: return 'Trop de tentatives : patientez quelques minutes avant de réessayer.'
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
  <div class="h-dvh overflow-y-auto pt-safe pb-safe">
    <header class="px-safe flex items-center gap-2 pt-3 pb-2">
      <UButton icon="i-lucide-chevron-left" color="neutral" variant="ghost" size="lg" aria-label="Retour" to="/instances" />
      <h1 class="text-lg font-semibold text-highlighted">Appairer une instance</h1>
    </header>

    <main class="px-safe space-y-5 py-3">
      <template v-if="request">
        <div class="rounded-lg border border-default bg-default p-4 space-y-4">
          <div class="flex items-center gap-3">
            <div class="size-10 shrink-0 rounded-md bg-primary/10 grid place-items-center text-primary">
              <UIcon name="i-lucide-server-cog" class="size-5" />
            </div>
            <div class="min-w-0">
              <p class="font-semibold text-highlighted truncate">{{ request.name || 'Machine sans nom' }}</p>
              <p class="text-sm text-muted truncate">{{ request.url.replace('https://', '') }}</p>
            </div>
          </div>
          <div class="space-y-1">
            <p class="text-xs font-medium text-toned">Empreinte SHA-256 de la CA</p>
            <p v-if="request.ca" class="font-mono text-xs break-all text-muted">{{ formatFingerprint(request.ca) }}</p>
            <p v-else class="text-xs text-muted">Non communiquée par l’instance.</p>
          </div>
        </div>

        <p class="text-sm text-muted">
          Vérifiez que ce nom et cette adresse sont bien ceux affichés par le dashboard de la machine. Le certificat de l’accès
          distant doit être signé par cette CA, installée et approuvée sur le téléphone<template v-if="request.ca"> : comparez
          l’empreinte avec celle du certificat installé</template>.
        </p>

        <UAlert
          v-if="existing"
          color="warning" variant="subtle" icon="i-lucide-refresh-cw"
          :description="`« ${existing.name} » est déjà dans l’app à cette adresse : sa session sera remplacée.`"
        />
        <UAlert v-if="error" color="error" variant="subtle" icon="i-lucide-circle-alert" :description="error" />

        <div class="space-y-2">
          <UButton icon="i-lucide-link" label="Appairer" size="xl" block :loading="busy" @click="confirm" />
          <UButton label="Annuler" color="neutral" variant="ghost" size="xl" block :disabled="busy" to="/instances" />
        </div>
      </template>

      <template v-else>
        <UAlert color="error" variant="subtle" icon="i-lucide-qr-code" title="Appairage impossible" :description="parsed.error" />
        <UButton label="Retour aux instances" color="neutral" variant="outline" size="xl" block to="/instances" />
      </template>
    </main>
  </div>
</template>
