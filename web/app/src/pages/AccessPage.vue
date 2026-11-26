<script setup lang="ts">
import PasswordInput from '../components/PasswordInput.vue'
import { computed, reactive, ref, watch } from 'vue'
import type { TableColumn } from '@nuxt/ui'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import PageTabs from '../components/PageTabs.vue'
import TwoColumns from '../components/TwoColumns.vue'
import ResourceTable from '../components/ResourceTable.vue'
import NameCell from '../components/NameCell.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
import ConfirmModal from '../components/ConfirmModal.vue'
import PairingModal from '../components/PairingModal.vue'
import { focusField } from '../composables/focus'
import { accessMode, actions, passwordSet, remoteActive, remoteUrl, state, type AccessEvent, type FieldErrors, type Session } from '../composables/useFm'
import { localeTag } from '../i18n'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

// Remote access (v0.11.0): DASHBOARD_* settings hot-applied by the daemon, `PUT /api/v1/auth/password`,
// `GET|DELETE /api/v1/auth/sessions` and `GET /api/v1/auth/log`.
const c = state.config
const tab = ref('settings')
const tabs = computed(() => [{ label: t('access.tabs.settings'), value: 'settings' }, { label: t('access.tabs.devices'), value: 'sessions' }, { label: t('access.tabs.log'), value: 'log' }])

const modes = computed(() => [
  { value: 'local', label: t('access.modes.local.label'), description: t('access.modes.local.description') },
  { value: 'lan', label: t('access.modes.lan.label'), description: t('access.modes.lan.description') },
  { value: 'internet', label: t('access.modes.internet.label'), description: t('access.modes.internet.description') },
])
const form = reactive({ mode: c.DASHBOARD_ACCESS, port: c.DASHBOARD_REMOTE_PORT, allowed: c.DASHBOARD_ALLOWED_IPS, localAuth: c.DASHBOARD_LOCAL_AUTH === 'true', custom: !!c.DASHBOARD_TLS_CERT, cert: c.DASHBOARD_TLS_CERT, key: c.DASHBOARD_TLS_KEY })
watch(() => c.DASHBOARD_ACCESS, (v) => { form.mode = v })
const errors = ref<FieldErrors>({})
const values = computed(() => ({
  DASHBOARD_ACCESS: form.mode, DASHBOARD_REMOTE_PORT: form.port.trim(), DASHBOARD_ALLOWED_IPS: form.allowed.trim(),
  DASHBOARD_LOCAL_AUTH: String(form.localAuth), DASHBOARD_TLS_CERT: form.custom ? form.cert.trim() : '', DASHBOARD_TLS_KEY: form.custom ? form.key.trim() : '',
}))
// From remote access, a DASHBOARD_* setting asks for the password again: it could expose the dashboard more widely.
const confirmOpen = ref(false)
// Pairing of the mobile app by QR code: the phone reaches the remote access, which must be open and listening.
const pairingOpen = ref(false)
const pairingHint = computed(() => accessMode() === 'local'
  ? t('access.pairingHint.local')
  : t('access.closedNoPassword'))
function save() {
  if (state.connection.mode === 'remote') { confirmOpen.value = true; return }
  apply()
}
async function apply() { errors.value = (await actions.setAccess(values.value)) ?? {} }

const pw = reactive({ current: '', password: '', again: '' })
const pwErrors = ref<FieldErrors>({})
const pwBusy = ref(false)
async function submitPassword() {
  if (pw.password !== pw.again) { pwErrors.value = { again: t('access.password.mismatch') }; return }
  pwBusy.value = true
  pwErrors.value = (await actions.setPassword(pw.current, pw.password)) ?? {}
  pwBusy.value = false
  if (!Object.keys(pwErrors.value).length) Object.assign(pw, { current: '', password: '', again: '' })
}

const remoteSessions = computed(() => state.access.sessions.filter((x) => x.remote).length)
const stats = computed(() => [
  {
    label: t('access.stats.access'), value: modes.value.find((m) => m.value === accessMode())!.label,
    sub: accessMode() === 'local' ? t('access.stats.localOnly') : remoteActive() ? t('access.stats.httpsPassword') : t('access.stats.closed'),
  },
  {
    label: t('access.stats.remoteAddress'), value: remoteActive() ? remoteUrl().replace('https://', '') : t('common.none'),
    sub: remoteActive() ? (c.DASHBOARD_TLS_CERT ? t('access.stats.certProvided') : t('access.stats.certMkcert')) : 'DASHBOARD_REMOTE_PORT',
  },
  { label: t('access.tabs.devices'), value: state.access.sessions.length, sub: t('access.stats.devicesSub', { n: remoteSessions.value }) },
])

/** Relative time in the active language ("5 min ago"), short style. */
const ago = (at: number) => {
  const m = Math.round((Date.now() - at) / 60_000)
  if (m < 2) return t('access.justNow')
  const rtf = new Intl.RelativeTimeFormat(localeTag.value, { style: 'short' })
  return m < 60 ? rtf.format(-m, 'minute') : m < 1440 ? rtf.format(-Math.round(m / 60), 'hour') : rtf.format(-Math.round(m / 1440), 'day')
}
const revoke = ref<Session | null>(null)
const sessionColumns = computed<TableColumn<Session>[]>(() => [
  { accessorKey: 'device', header: t('access.columns.device') },
  { id: 'seen', header: t('access.columns.lastSeen'), meta: { class: { th: 'w-32 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'kind', header: t('access.stats.access'), meta: { class: { th: 'w-24' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])

// Event label keys, resolved when rendered so that they follow the language.
const EVENTS: Record<AccessEvent['event'], { key: string; color: 'success' | 'error' | 'warning' | 'neutral' }> = {
  login: { key: 'access.events.login', color: 'success' },
  login_failed: { key: 'access.events.loginFailed', color: 'error' },
  logout: { key: 'access.events.logout', color: 'neutral' },
  revoked: { key: 'access.events.revoked', color: 'neutral' },
  refused: { key: 'access.events.refused', color: 'warning' },
  confirm_failed: { key: 'access.events.confirmFailed', color: 'error' },
  password_changed: { key: 'access.events.passwordChanged', color: 'neutral' },
  paired: { key: 'access.events.paired', color: 'success' },
  pair_failed: { key: 'access.events.pairFailed', color: 'error' },
}
const eventOf = (e: AccessEvent) => {
  const ev = EVENTS[e.event]
  return ev ? { label: t(ev.key), color: ev.color } : { label: e.event, color: 'neutral' as const }
}
const logColumns = computed<TableColumn<AccessEvent>[]>(() => [
  { accessorKey: 'event', header: t('access.columns.event') },
  { id: 'ip', header: t('access.columns.address'), meta: { class: { th: 'w-32 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'at', header: t('access.columns.when'), meta: { class: { th: 'w-28' } } },
])
</script>

<template>
  <PageHeader :title="t('nav.access')" :subtitle="t('access.subtitle')">
    <UTooltip :text="pairingHint" :disabled="remoteActive()">
      <UButton icon="i-lucide-qr-code" :label="t('access.pair')" size="lg" color="neutral" variant="outline" :disabled="!remoteActive()" @click="pairingOpen = true" />
    </UTooltip>
    <UButton icon="i-lucide-key-round" :label="t('common.password')" size="lg" class="md:w-[205px]" @click="tab = 'settings'; focusField(passwordSet() ? 'pw-current' : 'pw-new')" />
  </PageHeader>
  <MiniStats :items="stats" />
  <PageTabs v-model="tab" :items="tabs" />

  <TwoColumns>
    <UCard v-if="tab === 'settings'" :ui="{ body: 'p-5 sm:p-5 flex flex-col gap-5' }">
      <div>
        <h2 class="text-[15px] font-semibold text-highlighted">{{ t('access.settings.title') }}</h2>
        <p class="mt-1 text-[11px] text-muted">{{ t('access.settings.subtitle', { key: 'DASHBOARD_ACCESS' }) }}</p>
      </div>
      <URadioGroup v-model="form.mode" :items="modes" variant="card" indicator="end" :ui="{ fieldset: 'gap-2', label: 'text-[13px]', description: 'text-[11px]' }" />
      <UAlert v-if="errors.DASHBOARD_ACCESS" color="error" variant="soft" :description="errors.DASHBOARD_ACCESS" :ui="{ description: 'text-xs' }" />
      <template v-if="form.mode !== 'local'">
        <div class="grid gap-4 sm:grid-cols-[140px_1fr]">
          <UFormField :label="t('access.settings.port')" :error="errors.DASHBOARD_REMOTE_PORT">
            <UInput v-model="form.port" inputmode="numeric" size="lg" class="w-full" />
          </UFormField>
          <UFormField :label="t('access.settings.allowed')" :help="t('access.settings.allowedHelp')" :error="errors.DASHBOARD_ALLOWED_IPS" :ui="{ help: 'text-[11px]' }">
            <UInput v-model="form.allowed" placeholder="192.168.1.0/24, 2001:db8::/32" size="lg" class="w-full font-mono" />
          </UFormField>
        </div>
        <USwitch v-model="form.custom" :label="t('access.settings.customCert')" :description="t('access.settings.customCertHelp')" />
        <div v-if="form.custom" class="grid gap-4 sm:grid-cols-2">
          <UFormField :label="t('access.settings.cert')"><UInput v-model="form.cert" placeholder="/etc/ssl/dashboard.pem" size="lg" class="w-full font-mono" /></UFormField>
          <UFormField :label="t('access.settings.key')"><UInput v-model="form.key" placeholder="/etc/ssl/dashboard-key.pem" size="lg" class="w-full font-mono" /></UFormField>
        </div>
      </template>
      <USwitch v-model="form.localAuth" :label="t('access.settings.localAuth')" :description="t('access.settings.localAuthHelp', { key: 'DASHBOARD_LOCAL_AUTH' })" />
      <UButton :label="t('access.settings.save')" size="lg" class="self-end" @click="save" />
    </UCard>
    <ResourceTable v-else-if="tab === 'sessions'" storage-key="access-sessions" :title="t('access.sessions.title')" :data="state.access.sessions" :columns="sessionColumns" search-key="device" :empty="t('access.sessions.empty')">
      <template #device-cell="{ row }">
        <NameCell :icon="row.original.remote ? 'i-lucide-smartphone' : 'i-lucide-laptop'" :name="row.original.device" :sub="row.original.current ? t('access.sessions.thisDevice', { ip: row.original.ip }) : row.original.ip" />
      </template>
      <template #seen-cell="{ row }">{{ ago(row.original.lastSeen) }}</template>
      <template #kind-cell="{ row }"><UBadge :label="row.original.remote ? t('common.remote') : t('common.local')" :color="row.original.remote ? 'warning' : 'primary'" variant="soft" /></template>
      <template #actions-cell="{ row }">
        <UDropdownMenu :content="{ align: 'end' }" :items="[[{ label: t('access.sessions.revoke'), icon: 'i-lucide-log-out', color: 'error', disabled: row.original.current, onSelect: () => { revoke = row.original } }]]">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <ResourceTable v-else :title="t('access.log.title')" storage-key="access-log" :data="state.access.log" :columns="logColumns" :empty="t('access.log.empty')">
      <template #event-cell="{ row }">
        <div class="flex min-w-0 items-center gap-3">
          <UBadge :label="eventOf(row.original).label" :color="eventOf(row.original).color" variant="soft" class="shrink-0" />
          <span class="truncate text-[11px] text-muted">{{ row.original.device }}</span>
        </div>
      </template>
      <template #ip-cell="{ row }"><span class="font-mono">{{ row.original.ip }}</span></template>
      <template #at-cell="{ row }">{{ ago(row.original.at) }}</template>
    </ResourceTable>

    <SideForm
      :title="passwordSet() ? t('access.password.change') : t('access.password.set')"
      :description="t('access.password.description')"
      :hint="t('access.password.hint', { cmd: 'frankenmanager daemon password' })"
      @submit="submitPassword"
    >
      <UFormField v-if="passwordSet()" :label="t('access.password.current')" :error="pwErrors.current">
        <PasswordInput id="pw-current" v-model="pw.current" autocomplete="current-password" size="lg" class="w-full" @update:model-value="pwErrors = {}" />
      </UFormField>
      <UFormField :label="t('access.password.new')" :help="t('access.password.newHelp')" :error="pwErrors.password" :ui="{ help: 'text-[11px]' }">
        <PasswordInput id="pw-new" v-model="pw.password" autocomplete="new-password" size="lg" class="w-full" @update:model-value="pwErrors = {}" />
      </UFormField>
      <UFormField :label="t('common.confirm')" :error="pwErrors.again">
        <PasswordInput v-model="pw.again" autocomplete="new-password" size="lg" class="w-full" @update:model-value="pwErrors = {}" />
      </UFormField>
      <UButton type="submit" :label="t('access.password.save')" size="lg" block :loading="pwBusy" :disabled="!pw.password" />
    </SideForm>
  </TwoColumns>

  <InfoBanner v-if="remoteActive()">
    {{ t('access.banner.remote', { url: remoteUrl() }) }}
  </InfoBanner>
  <InfoBanner v-else-if="accessMode() !== 'local'" tone="warning">{{ t('access.banner.closed') }}</InfoBanner>
  <InfoBanner v-else>{{ t('access.banner.local', { address: `127.0.0.1:${c.DASHBOARD_PORT}` }) }}</InfoBanner>

  <PairingModal v-model:open="pairingOpen" />
  <ConfirmModal
    v-model:open="confirmOpen"
    :eyebrow="t('access.confirmSettings.eyebrow')"
    :title="t('access.confirmSettings.title')"
    :description="t('access.confirmSettings.description')"
    :confirm="t('common.save')"
    @confirm="apply"
  />
  <ConfirmModal
    :open="!!revoke"
    :eyebrow="t('nav.access')"
    :title="t('access.confirmRevoke.title', { device: revoke?.device })"
    :description="t('access.confirmRevoke.description')"
    :confirm="t('access.confirmRevoke.confirm')"
    @update:open="(v: boolean) => { if (!v) revoke = null }"
    @confirm="actions.revokeSession(revoke!.id)"
  />
</template>
