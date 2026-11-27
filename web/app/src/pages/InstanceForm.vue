<script setup lang="ts">
import PasswordInput from '../components/PasswordInput.vue'
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ApiError, health, hostInfo, login, logout } from '../lib/api'
import {
  ADDRESS_PLACEHOLDER, DEFAULT_REMOTE_PORT, findInstance, instancePlatform, INSTANCES_PATH, newId, originParts, remoteOrigin, setCurrent,
  splitAddress, subnetPlaceholder, upsertInstance,
} from '../lib/instances'

// Adds an instance, or signs in again to an instance whose session expired (props.id). Shared by the mobile app and
// the dashboard (PWA).
const props = defineProps<{ id?: string }>()
const { t } = useI18n()
const router = useRouter()
const platform = instancePlatform()
const web = platform.kind === 'web'
const existing = computed(() => (props.id ? findInstance(props.id) : undefined))

// The dashboard's machine signs in with the page itself (its own sign-in screen).
if (existing.value?.home) void router.replace('/')

// Address and port of the remote access, apart: the port is the default one (DASHBOARD_REMOTE_PORT) unless changed.
const parts = existing.value ? originParts(existing.value.url) : { host: '', port: String(DEFAULT_REMOTE_PORT) }
const host = ref(parts.host)
const port = ref(parts.port)
const name = ref(existing.value?.name ?? '')
const password = ref('')
const busy = ref(false)
const error = ref('')

// Address placeholder on the network of this device (192.168.0.x), when the platform can tell it.
const placeholder = ref(ADDRESS_PLACEHOLDER)
if (!existing.value) {
  void platform.localNetwork?.().then((net) => {
    const p = net && subnetPlaceholder(net)
    if (p) placeholder.value = p
  }, () => {})
}

/** An address pasted with its port (« mac.local:8443 », a URL): the port goes to its own field. */
function splitTyped() {
  const typed = splitAddress(host.value)
  if (typed.port) port.value = typed.port
  if (typed.host !== host.value) host.value = typed.host
}

async function submit() {
  error.value = ''
  let origin: string
  try {
    if (existing.value) {
      origin = existing.value.url
    } else {
      splitTyped()
      origin = remoteOrigin(host.value, port.value)
    }
  } catch (e) {
    error.value = (e as Error).message
    return
  }
  busy.value = true
  try {
    await health(origin)
    const { token, session } = await login(origin, password.value, await platform.deviceName())
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
    setCurrent(id)
    await router.replace('/')
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) error.value = t('mobile.form.wrongPassword')
    else if (e instanceof ApiError && e.status === 403) error.value = t('mobile.errors.forbidden', { message: e.message })
    else error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

/** Pairing with the QR code of the dashboard (the mobile app), instead of the address and password. */
async function scan() {
  error.value = ''
  try {
    await platform.scan?.(router)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}
</script>

<template>
  <div class="fixed inset-0 overflow-y-auto pt-safe pb-safe">
    <div class="mx-auto w-full max-w-2xl">
      <header class="px-safe flex items-center gap-2 pt-3 pb-2">
        <UButton icon="i-lucide-chevron-left" color="neutral" variant="ghost" size="lg" :aria-label="t('common.back')" :to="INSTANCES_PATH" />
        <h1 class="text-lg font-semibold text-highlighted">{{ existing ? t('mobile.instances.reconnect') : t('mobile.form.add') }}</h1>
      </header>

      <main class="px-safe space-y-5 py-3">
        <template v-if="platform.scan">
          <div class="space-y-2">
            <UButton icon="i-lucide-scan-qr-code" :label="t('mobile.pair.scan')" size="xl" block @click="scan" />
            <p class="text-center text-xs text-muted">{{ t('mobile.form.scanHelp') }}</p>
          </div>

          <USeparator :label="t('mobile.form.orPassword')" />
        </template>

        <form class="space-y-5" @submit.prevent="submit">
          <div>
            <div class="grid grid-cols-[minmax(0,1fr)_6.5rem] gap-3">
              <UFormField :label="t('mobile.form.address')" required>
                <UInput
                  v-model="host"
                  :placeholder="placeholder"
                  size="xl"
                  class="w-full"
                  inputmode="url"
                  autocapitalize="off"
                  autocorrect="off"
                  spellcheck="false"
                  :disabled="!!existing"
                  @blur="splitTyped"
                />
              </UFormField>
              <UFormField :label="t('mobile.form.port')" required>
                <UInput
                  v-model="port"
                  :placeholder="String(DEFAULT_REMOTE_PORT)"
                  size="xl"
                  class="w-full"
                  inputmode="numeric"
                  maxlength="5"
                  autocomplete="off"
                  :disabled="!!existing"
                />
              </UFormField>
            </div>
            <p class="mt-2 text-sm text-muted">{{ t('mobile.form.addressHelp') }}</p>
          </div>
          <UFormField v-if="!existing" :label="t('common.name')" :help="t('mobile.form.nameHelp')">
            <UInput v-model="name" placeholder="MacBook-Pro" size="xl" class="w-full" />
          </UFormField>
          <UFormField :label="t('signIn.password')" required>
            <PasswordInput v-model="password" size="xl" class="w-full" autocomplete="current-password" />
          </UFormField>

          <UAlert v-if="error" color="error" variant="subtle" icon="i-lucide-circle-alert" :description="error" />

          <UButton
            type="submit" :label="t('common.signIn')" :color="platform.scan ? 'neutral' : 'primary'" :variant="platform.scan ? 'outline' : 'solid'"
            size="xl" block :loading="busy" :disabled="!host || !port || !password"
          />

          <div class="rounded-lg bg-elevated p-4 text-sm text-muted space-y-2">
            <p class="font-medium text-toned">{{ web ? t('instances.firstTime.title') : t('mobile.form.firstTime.title') }}</p>
            <ol class="list-decimal pl-5 space-y-1">
              <i18n-t keypath="mobile.form.firstTime.access" tag="li" scope="global">
                <template #access><code>frankenmanager configuration set DASHBOARD_ACCESS lan</code></template>
                <template #password><code>frankenmanager daemon password</code></template>
              </i18n-t>
              <i18n-t :keypath="web ? 'instances.firstTime.ca' : 'mobile.form.firstTime.ca'" tag="li" scope="global">
                <template #trustCa><code>frankenmanager trust-ca on</code></template>
              </i18n-t>
              <li>{{ t('mobile.form.firstTime.ios') }}</li>
              <!-- Another dashboard calls the machine across origins: older daemons refuse it. -->
              <i18n-t v-if="web" keypath="instances.firstTime.version" tag="li" scope="global">
                <template #update><code>frankenmanager update</code></template>
              </i18n-t>
            </ol>
          </div>
        </form>
      </main>
    </div>
  </div>
</template>
