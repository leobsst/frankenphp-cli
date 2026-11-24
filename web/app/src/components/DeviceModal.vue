<script setup lang="ts">
import { watch } from 'vue'
import { actions, dnsClientsConnected, loadPage, state } from '../composables/useFm'
import QrCode from './QrCode.vue'

// POC : lien provisoire. En réel : `http://${state.dns.ip}:${state.ssl.caPort}` (téléchargement de la CA).
const CA_DOWNLOAD_URL = 'https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDdQw4w9WgXcQ'

const open = defineModel<boolean>('open', { default: false })

// Les appareils du DNS LAN et l'état du partage de la CA sont relus à l'ouverture.
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
          <p class="text-xs text-muted">Partagez la CA puis ouvrez <code class="font-mono text-default">http://{{ state.dns.ip }}:{{ state.ssl.caPort }}</code> sur l’appareil pour l’installer.</p>
          <USwitch class="mt-3" label="Partager la CA sur le réseau local" :model-value="state.ssl.caShared" @update:model-value="actions.setCaShared($event, state.ssl.caPort)" />
          <div v-if="state.ssl.caShared" class="mt-3 flex items-center gap-4 rounded-lg border border-default bg-muted p-3">
            <QrCode :value="CA_DOWNLOAD_URL" label="QR code de téléchargement de la CA" class="size-28 shrink-0" />
            <p class="text-xs text-muted">Scannez ce QR code avec l’appareil pour télécharger la CA, sans saisir l’adresse.</p>
          </div>
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
