<script setup lang="ts">
import { ref, watch } from 'vue'
import { actions, dnsClientsConnected, loadPage, state } from '../composables/useFm'
import QrCode from './QrCode.vue'

const open = defineModel<boolean>('open', { default: false })

// The CA is downloaded from the daemon's sharing server (state.ssl.caUrl, on the host's LAN address): the phone
// opens that URL and receives the certificate file. Off: no QR code, the sharing is offered instead.
const toggling = ref(false)
async function share(on: boolean) {
  toggling.value = true
  try {
    await actions.setCaShared(on, state.ssl.caPort)
  } finally {
    toggling.value = false
  }
}

// The LAN DNS devices and the CA sharing state are reloaded on open.
watch(open, (o) => { if (o) void loadPage('dns') })
</script>

<template>
  <UModal v-model:open="open" title="Configurer un appareil" description="Accédez à vos domaines .test depuis un téléphone ou une tablette du même réseau." :ui="{ footer: 'justify-end' }">
    <template #body>
      <ol class="flex flex-col gap-4 text-sm">
        <li>
          <p class="font-medium text-highlighted">1. DNS</p>
          <p class="text-xs text-muted">Dans les réglages Wi-Fi de l’appareil, définissez le DNS sur <code class="font-mono text-default">{{ state.dns.ip }}</code>.</p>
          <p v-if="!state.dns.enabled" class="mt-2 flex items-center gap-2 text-xs text-warning">
            <UIcon name="i-lucide-triangle-alert" class="size-4" />Le serveur DNS est désactivé.
            <UButton size="xs" variant="soft" label="Activer" @click="actions.setService('dns', true)" />
          </p>
          <div v-else class="mt-3 rounded-lg border border-default bg-muted p-3 text-xs">
            <p class="flex items-center gap-2 font-medium text-highlighted">
              <span class="size-1.5 rounded-full" :class="state.dns.status === 'running' ? 'bg-primary animate-pulse' : 'bg-error'" />
              {{ dnsClientsConnected.length }} appareil{{ dnsClientsConnected.length > 1 ? 's' : '' }} connecté{{ dnsClientsConnected.length > 1 ? 's' : '' }} au DNS
            </p>
            <p class="mt-1 truncate text-muted">{{ dnsClientsConnected.slice(0, 4).map((c) => c.name ?? c.ip).join(' · ') || 'En attente d’un appareil…' }}</p>
          </div>
        </li>
        <li>
          <p class="font-medium text-highlighted">2. Autorité locale</p>
          <template v-if="state.ssl.caShared">
            <template v-if="state.ssl.caUrl">
              <p class="text-xs text-muted">Ouvrez <code class="font-mono break-all text-default">{{ state.ssl.caUrl }}</code> sur l’appareil pour télécharger la CA, puis installez-la.</p>
              <div class="mt-3 flex items-center gap-4 rounded-lg border border-default bg-muted p-3">
                <QrCode :value="state.ssl.caUrl" label="QR code de téléchargement de la CA" class="size-28 shrink-0" />
                <p class="text-xs text-muted">Scannez ce QR code avec l’appareil pour télécharger la CA, sans saisir l’adresse.</p>
              </div>
            </template>
            <p v-else class="mt-2 flex items-center gap-2 text-xs text-warning">
              <UIcon name="i-lucide-triangle-alert" class="size-4 shrink-0" />L’adresse de la machine sur le réseau local est inconnue : utilisez son IP et le port {{ state.ssl.caPort }}.
            </p>
            <UButton class="mt-3" size="xs" color="neutral" variant="outline" icon="i-lucide-circle-stop" label="Arrêter le partage" :loading="toggling" @click="share(false)" />
          </template>
          <template v-else>
            <p class="text-xs text-muted">L’appareil télécharge la CA depuis cette machine, sur le réseau local, le temps de l’installer.</p>
            <p class="mt-2 flex flex-wrap items-center gap-2 text-xs text-warning">
              <UIcon name="i-lucide-triangle-alert" class="size-4 shrink-0" />Le partage de la CA est désactivé : aucun QR code à scanner.
              <UButton size="xs" variant="soft" label="Activer le partage" :loading="toggling" @click="share(true)" />
            </p>
          </template>
        </li>
        <li>
          <p class="font-medium text-highlighted">3. Approuver</p>
          <p class="text-xs text-muted">iOS : Réglages › Général › Informations › Réglages des certificats. Désactivez le partage ensuite.</p>
        </li>
      </ol>
    </template>
    <template #footer="{ close }">
      <UButton label="Terminé" @click="close" />
    </template>
  </UModal>
</template>
