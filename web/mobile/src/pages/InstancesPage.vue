<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { DropdownMenuItem } from '@nuxt/ui'
import { client, logout, type Status } from '@app/lib/api'
import { formatNumber } from '@app/i18n'
import { addDemoInstance, current, displayAddress, displayName, isDemo, removeInstance, setCurrent, store, type Instance } from '../lib/instances'
import { openPairing, scanPairing } from '../lib/pairing'

const { t } = useI18n()
const router = useRouter()
const version = __APP_VERSION__
const build = __APP_BUILD__

/** State of each instance at the last contact, for the list. */
interface Probe {
  state: 'loading' | 'online' | 'offline' | 'expired'
  hostname?: string
  running?: boolean
  hosts?: number
  error?: string
}
const probes = reactive<Record<string, Probe>>({})

async function probe(inst: Instance) {
  if (inst.expired) {
    probes[inst.id] = { state: 'expired' }
    return
  }
  probes[inst.id] = { ...probes[inst.id], state: 'loading' }
  try {
    const api = client(inst)
    const [host, status] = await Promise.all([api.get<{ hostname: string }>('/api/v1/host'), api.get<Status>('/api/v1/status')])
    probes[inst.id] = { state: 'online', hostname: host.hostname, running: status.running, hosts: status.hosts.length }
  } catch (e) {
    probes[inst.id] = { state: inst.expired ? 'expired' : 'offline', error: e instanceof Error ? e.message : String(e) }
  }
}
const probeAll = () => store.list.forEach(probe)
onMounted(probeAll)

/** Opens the dashboard on this instance, or its sign-in again if the session expired. */
async function open(inst: Instance) {
  if (inst.expired) return router.push(`/instances/${inst.id}/connexion`)
  await setCurrent(inst.id)
  router.push('/')
}

/** Demo mode: a simulated instance (src/demo), to try the app without a machine running FrankenManager. */
async function tryDemo() {
  await open(await addDemoInstance())
}

/** Pairing with the QR code of the dashboard. */
const scanError = ref('')
async function scan() {
  scanError.value = ''
  try {
    const link = await scanPairing()
    if (link) await openPairing(router, link)
  } catch (e) {
    scanError.value = e instanceof Error ? e.message : String(e)
  }
}

const menu = (inst: Instance): DropdownMenuItem[] => [
  // The demo has no session to open again.
  ...(isDemo(inst) ? [] : [{ label: t('mobile.instances.reconnect'), icon: 'i-lucide-key-round', onSelect: () => router.push(`/instances/${inst.id}/connexion`) }]),
  { label: t('mobile.instances.remove'), icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { removing.value = inst } },
]

// Removal, from the menu or a swipe: always confirmed first.
const removing = ref<Instance | null>(null)
const removingName = computed(() => (removing.value ? displayName(removing.value) : ''))
async function confirmRemove() {
  const inst = removing.value
  removing.value = null
  if (!inst) return
  if (!isDemo(inst)) await logout(inst)
  await removeInstance(inst.id)
  if (current.id === inst.id) await setCurrent(null)
}

/**
 * Swipe of a row: to the right opens the instance, to the left asks to remove it, once released past SWIPE. The
 * direction is decided after a few pixels: a vertical move stays a scroll of the list (touch-action: pan-y).
 */
const SWIPE = 96
const swipe = reactive({ id: '', dx: 0, dragging: false })
let gesture: { id: string; x: number; y: number; axis: '' | 'x' | 'y' } | null = null
/** A swipe just ended: the click that follows it does not open the instance. */
let swiped = false
function down(e: PointerEvent, inst: Instance) {
  if (e.pointerType === 'mouse' && e.button !== 0) return
  gesture = { id: inst.id, x: e.clientX, y: e.clientY, axis: '' }
  swiped = false
}
function move(e: PointerEvent) {
  if (!gesture) return
  const dx = e.clientX - gesture.x
  const dy = e.clientY - gesture.y
  if (!gesture.axis) {
    if (Math.abs(dx) < 8 && Math.abs(dy) < 8) return
    gesture.axis = Math.abs(dx) > Math.abs(dy) ? 'x' : 'y'
    if (gesture.axis === 'x') (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  }
  if (gesture.axis !== 'x') return
  swiped = true
  // Past the threshold the row follows the finger less: the action is armed.
  const past = Math.max(0, Math.abs(dx) - SWIPE)
  Object.assign(swipe, { id: gesture.id, dx: Math.sign(dx) * (Math.min(Math.abs(dx), SWIPE) + past * 0.3), dragging: true })
}
function up(inst: Instance) {
  if (!gesture) return
  const dx = swipe.id === inst.id ? swipe.dx : 0
  gesture = null
  Object.assign(swipe, { dx: 0, dragging: false })
  if (dx >= SWIPE) void open(inst)
  else if (dx <= -SWIPE) removing.value = inst
}
const dxOf = (inst: Instance) => (swipe.id === inst.id ? swipe.dx : 0)
function click(inst: Instance) {
  if (swiped) { swiped = false; return }
  void open(inst)
}

/** Badges not known yet (first probe): skeletons of their size, so that the row does not jump. */
const probing = (inst: Instance) => {
  const p = probes[inst.id]
  return !inst.expired && (!p || p.state === 'loading') && p?.hosts === undefined
}

const dot: Record<Probe['state'], string> = {
  loading: 'bg-dimmed animate-pulse',
  online: 'bg-primary',
  offline: 'bg-error',
  expired: 'bg-warning',
}
</script>

<template>
  <div class="fixed inset-0 overflow-y-auto pt-safe pb-safe">
    <header class="px-safe flex items-center justify-between gap-3 pt-4 pb-2">
      <div class="flex items-center gap-2">
        <div class="size-9 rounded-md bg-primary grid place-items-center text-white">
          <UIcon name="i-lucide-server-cog" class="size-5" />
        </div>
        <div>
          <h1 class="text-lg font-semibold text-highlighted leading-tight">FrankenManager</h1>
          <p class="text-xs text-muted">{{ t('mobile.instances.title') }}</p>
        </div>
      </div>
      <UButton icon="i-lucide-refresh-cw" color="neutral" variant="ghost" size="lg" :aria-label="t('mobile.instances.refresh')" @click="probeAll" />
    </header>

    <main class="px-safe space-y-3 py-3">
      <UAlert v-if="scanError" color="error" variant="subtle" icon="i-lucide-circle-alert" :description="scanError" />

      <div v-if="!store.list.length" class="rounded-lg border border-dashed border-accented bg-default p-6 text-center space-y-3">
        <UIcon name="i-lucide-smartphone" class="size-10 text-primary mx-auto" />
        <h2 class="font-semibold text-highlighted">{{ t('mobile.instances.empty') }}</h2>
        <i18n-t keypath="mobile.instances.emptyHelp" tag="p" scope="global" class="text-sm text-muted">
          <template #env><code>DASHBOARD_ACCESS=lan</code></template>
        </i18n-t>
        <UButton icon="i-lucide-scan-qr-code" :label="t('mobile.pair.scan')" size="lg" block @click="scan" />
        <UButton to="/instances/ajouter" icon="i-lucide-plus" :label="t('mobile.instances.addWithPassword')" color="neutral" variant="outline" size="lg" block />
        <div class="pt-2 border-t border-default space-y-2">
          <p class="text-sm text-muted">{{ t('mobile.demo.help') }}</p>
          <UButton icon="i-lucide-play" :label="t('mobile.demo.try')" color="primary" variant="soft" size="lg" block @click="tryDemo" />
        </div>
      </div>

      <div v-for="inst in store.list" :key="inst.id" class="relative overflow-hidden rounded-lg border border-default">
        <!-- Action revealed under the row by a swipe: open (to the right) or remove (to the left). -->
        <div
          v-if="dxOf(inst)" aria-hidden="true"
          class="absolute inset-0 flex items-center px-5 text-sm font-semibold text-inverted"
          :class="dxOf(inst) > 0 ? 'justify-start bg-primary' : 'justify-end bg-error'"
        >
          <span class="flex items-center gap-2" :class="Math.abs(dxOf(inst)) < SWIPE && 'opacity-60'">
            <template v-if="dxOf(inst) > 0"><UIcon name="i-lucide-log-in" class="size-5" />{{ t('mobile.instances.connect') }}</template>
            <template v-else>{{ t('mobile.instances.swipeRemove') }}<UIcon name="i-lucide-trash-2" class="size-5" /></template>
          </span>
        </div>
        <div
          class="relative flex items-stretch bg-default touch-pan-y select-none active:bg-elevated"
          :style="{ transform: `translateX(${dxOf(inst)}px)`, transition: swipe.dragging && swipe.id === inst.id ? 'none' : 'transform 200ms ease' }"
          @pointerdown="down($event, inst)" @pointermove="move" @pointerup="up(inst)" @pointercancel="up(inst)"
          >
          <button type="button" class="flex-1 min-w-0 text-left p-4 flex gap-3 items-start" @click="click(inst)">
            <span class="mt-1.5 size-2.5 shrink-0 rounded-full" :class="dot[probes[inst.id]?.state ?? 'loading']" />
            <span class="min-w-0 flex-1 space-y-1">
              <span class="block font-semibold text-highlighted truncate">{{ displayName(inst) }}</span>
              <span class="block text-xs text-muted truncate">{{ displayAddress(inst) }}<template v-if="inst.version"> · v{{ inst.version }}</template></span>
              <span class="flex flex-wrap gap-1.5 pt-1">
                <UBadge v-if="isDemo(inst)" color="primary" variant="solid" icon="i-lucide-flask-conical" :label="t('mobile.demo.badge')" />
                <template v-if="probing(inst)">
                  <USkeleton class="h-5 w-24 rounded-md" /><USkeleton class="h-5 w-20 rounded-md" />
                </template>
                <template v-else-if="probes[inst.id]?.state === 'online'">
                  <UBadge :color="probes[inst.id]?.running ? 'success' : 'neutral'" variant="subtle" :label="probes[inst.id]?.running ? t('mobile.instances.stackOnline') : t('mobile.instances.stackStopped')" />
                  <UBadge color="neutral" variant="outline" :label="t('mobile.instances.domains', { n: formatNumber(probes[inst.id]?.hosts ?? 0) }, probes[inst.id]?.hosts ?? 0)" />
                </template>
                <UBadge v-else-if="probes[inst.id]?.state === 'expired'" color="warning" variant="subtle" :label="t('mobile.instances.expired')" />
                <UBadge v-else-if="probes[inst.id]?.state === 'offline'" color="error" variant="subtle" :label="t('mobile.instances.unreachable')" />
              </span>
            </span>
          </button>
          <UDropdownMenu :items="menu(inst)" :content="{ align: 'end' }">
            <UButton icon="i-lucide-ellipsis-vertical" color="neutral" variant="ghost" class="rounded-none px-3" :aria-label="t('common.actions')" />
          </UDropdownMenu>
        </div>
      </div>

      <template v-if="store.list.length">
        <UButton icon="i-lucide-scan-qr-code" :label="t('mobile.pair.scan')" size="lg" block @click="scan" />
        <UButton to="/instances/ajouter" icon="i-lucide-plus" :label="t('mobile.instances.addWithPassword')" color="neutral" variant="outline" size="lg" block />
        <UButton v-if="!store.list.some(isDemo)" icon="i-lucide-play" :label="t('mobile.demo.try')" color="primary" variant="soft" size="lg" block @click="tryDemo" />
      </template>

      <p class="pt-4 text-center text-xs text-dimmed">{{ t('mobile.instances.version', { version, build }) }}</p>
    </main>

    <UModal
      :open="!!removing" :title="t('mobile.instances.removeTitle', { name: removingName })"
      :description="removing && isDemo(removing) ? t('mobile.instances.removeDemoHelp') : t('mobile.instances.removeHelp')"
      :ui="{ footer: 'justify-end gap-2' }" @update:open="(v: boolean) => { if (!v) removing = null }"
    >
      <template #footer>
        <UButton color="neutral" variant="outline" :label="t('common.cancel')" @click="removing = null" />
        <UButton color="error" icon="i-lucide-trash-2" :label="t('mobile.instances.remove')" @click="confirmRemove" />
      </template>
    </UModal>
  </div>
</template>
