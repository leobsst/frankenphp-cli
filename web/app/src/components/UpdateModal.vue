<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { checkUpdate, state, toast, updateAvailable, updates } from '../composables/useFm'
import { releaseBlocks } from '../lib/markdown'
import { formatDate, formatTime } from '../i18n'

// `frankenmanager update --check`, then the update itself in a terminal of the host: the daemon runs as the user and
// cannot replace a binary installed in a system directory (administrator rights).
const open = defineModel<boolean>('open', { default: false })
const { t } = useI18n()

const info = computed(() => updates.info)
const version = computed(() => info.value?.version || state.app.version)
const releases = computed(() => (info.value?.releases ?? []).map((r) => ({ ...r, blocks: releaseBlocks(r.notes) })))
const command = computed(() => info.value?.command ?? 'sudo frankenmanager update')
const host = computed(() => state.connection.host || t('updateModal.hostFallback'))

const date = (iso: string) => (iso ? formatDate(iso, { day: 'numeric', month: 'long', year: 'numeric' }) : '')
const checkedAt = computed(() => (info.value?.checked_at ? formatTime(info.value.checked_at, { hour: '2-digit', minute: '2-digit' }) : ''))

function copy() {
  navigator.clipboard?.writeText(command.value)
  toast('info', t('updateModal.copied'), command.value)
}
</script>

<template>
  <UModal v-model:open="open" :title="t('updateModal.title')" :description="t('updateModal.description')" :ui="{ content: 'sm:max-w-[520px]', footer: 'gap-2' }">
    <template #body>
      <div class="flex items-center gap-3 rounded-lg border border-default bg-muted p-3 text-xs">
        <div>
          <p class="text-muted">{{ t('updateModal.installed') }}</p>
          <p class="font-mono text-sm font-semibold text-highlighted">v{{ version || '…' }}</p>
        </div>
        <UIcon name="i-lucide-arrow-right" class="size-4 text-muted" />
        <div>
          <p class="text-muted">{{ t('updateModal.latest') }}</p>
          <p class="font-mono text-sm font-semibold text-highlighted">{{ info?.latest ? `v${info.latest}` : '?' }}</p>
        </div>
        <UBadge v-if="!info" :label="t('updateModal.unknown')" color="neutral" variant="soft" class="ml-auto" />
        <UBadge v-else-if="updateAvailable" :label="t('updateModal.available')" color="warning" variant="soft" class="ml-auto" />
        <UBadge v-else :label="t('updateModal.upToDate')" variant="soft" class="ml-auto" />
      </div>
      <p v-if="checkedAt" class="mt-1.5 text-[11px] text-dimmed">{{ t('updateModal.checkedAt', { time: checkedAt }) }}</p>

      <UAlert
        v-if="updates.error || info?.error"
        class="mt-4" color="warning" variant="soft" icon="i-lucide-cloud-off"
        :title="t('updateModal.checkFailed')"
        :description="info?.error ? t('updateModal.previousResult', { error: info.error }) : updates.error"
        :ui="{ description: 'text-xs' }"
      />

      <template v-if="updateAvailable">
        <div class="mt-2 max-h-[45vh] overflow-y-auto pr-1 scrollbar-thin">
          <section v-for="r in releases" :key="r.version">
            <p class="mt-4 text-sm font-medium text-highlighted">
              {{ t('updateModal.whatsNew', { version: r.version }) }}
              <span v-if="r.published_at" class="text-xs font-normal text-muted">· {{ date(r.published_at) }}</span>
              <ULink v-if="r.url" :to="r.url" target="_blank" class="ml-1 text-xs font-normal">GitHub</ULink>
            </p>
            <template v-for="(b, i) in r.blocks" :key="i">
              <p v-if="b.kind === 'heading'" class="mt-3 text-xs font-semibold text-highlighted">
                <template v-for="(s, j) in b.content" :key="j">{{ s.text }}</template>
              </p>
              <p v-else class="mt-1.5 flex gap-2 text-xs text-muted">
                <span v-if="b.kind === 'item'" aria-hidden="true">•</span>
                <span class="min-w-0 break-words">
                  <template v-for="(s, j) in b.content" :key="j">
                    <code v-if="s.kind === 'code'" class="rounded bg-elevated px-1 font-mono text-[11px] text-highlighted">{{ s.text }}</code>
                    <strong v-else-if="s.kind === 'bold'" class="font-semibold text-highlighted">{{ s.text }}</strong>
                    <ULink v-else-if="s.kind === 'link'" :to="s.href" target="_blank" class="font-mono">{{ s.text }}</ULink>
                    <template v-else>{{ s.text }}</template>
                  </template>
                </span>
              </p>
            </template>
          </section>
        </div>
        <UAlert
          class="mt-5" color="neutral" variant="soft" icon="i-lucide-info"
          :description="state.stack === 'running' ? t('updateModal.installRunning', { host }) : t('updateModal.installStopped', { host })"
          :ui="{ description: 'text-xs' }"
        />
        <div class="mt-3 flex items-center gap-2 rounded-lg border border-default bg-muted py-1 pr-1 pl-3">
          <code class="min-w-0 flex-1 truncate font-mono text-xs text-highlighted">$ {{ command }}</code>
          <UButton icon="i-lucide-copy" color="neutral" variant="ghost" size="sm" :aria-label="t('updateModal.copyCommand')" @click="copy" />
        </div>
      </template>
    </template>
    <template #footer="{ close }">
      <UButton color="neutral" variant="outline" icon="i-lucide-refresh-cw" :label="t('updateModal.check')" :loading="updates.checking" @click="checkUpdate()" />
      <UButton v-if="updateAvailable" icon="i-lucide-copy" :label="t('updateModal.copyCommand')" class="ml-auto" @click="copy" />
      <UButton v-else :label="t('common.close')" class="ml-auto" @click="close" />
    </template>
  </UModal>
</template>
