<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { TableColumn } from '@nuxt/ui'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import TwoColumns from '../components/TwoColumns.vue'
import ResourceTable from '../components/ResourceTable.vue'
import NameCell from '../components/NameCell.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
import ConfirmModal from '../components/ConfirmModal.vue'
import PairingModal from '../components/PairingModal.vue'
import { focusField } from '../composables/focus'
import { accessMode, actions, passwordSet, remoteActive, remoteUrl, state, type AccessEvent, type FieldErrors, type Session } from '../composables/useFm'

// Remote access (v0.11.0): DASHBOARD_* settings hot-applied by the daemon, `PUT /api/v1/auth/password`,
// `GET|DELETE /api/v1/auth/sessions` and `GET /api/v1/auth/log`.
const c = state.config
const tab = ref('settings')
const tabs = [{ label: 'Réglages', value: 'settings' }, { label: 'Appareils', value: 'sessions' }, { label: 'Journal', value: 'log' }]

const MODES = [
  { value: 'local', label: 'Cette machine', description: 'Le dashboard ne répond que sur 127.0.0.1. Le mot de passe est facultatif.' },
  { value: 'lan', label: 'Réseau local', description: 'Aussi les adresses privées (Wi-Fi, Ethernet, Tailscale), en HTTPS. Mot de passe obligatoire.' },
  { value: 'internet', label: 'Internet', description: 'Toute adresse, via une redirection de port ou un tunnel, en HTTPS. Mot de passe obligatoire.' },
]
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
  ? 'Ouvrez d’abord l’accès distant (Réseau local ou Internet) : le téléphone doit pouvoir joindre le dashboard.'
  : 'L’accès distant est fermé tant que le dashboard n’a pas de mot de passe.')
function save() {
  if (state.connection.mode === 'remote') { confirmOpen.value = true; return }
  apply()
}
async function apply() { errors.value = (await actions.setAccess(values.value)) ?? {} }

const pw = reactive({ current: '', password: '', again: '' })
const pwErrors = ref<FieldErrors>({})
const pwBusy = ref(false)
async function submitPassword() {
  if (pw.password !== pw.again) { pwErrors.value = { again: 'Les mots de passe diffèrent.' }; return }
  pwBusy.value = true
  pwErrors.value = (await actions.setPassword(pw.current, pw.password)) ?? {}
  pwBusy.value = false
  if (!Object.keys(pwErrors.value).length) Object.assign(pw, { current: '', password: '', again: '' })
}

const remoteSessions = computed(() => state.access.sessions.filter((x) => x.remote).length)
const stats = computed(() => [
  { label: 'Accès', value: MODES.find((m) => m.value === accessMode())!.label, sub: accessMode() === 'local' ? '127.0.0.1 seulement' : remoteActive() ? 'HTTPS · mot de passe' : 'Fermé : pas de mot de passe' },
  { label: 'Adresse distante', value: remoteActive() ? remoteUrl().replace('https://', '') : '—', sub: remoteActive() ? `Certificat ${c.DASHBOARD_TLS_CERT ? 'fourni' : 'mkcert'}` : 'DASHBOARD_REMOTE_PORT' },
  { label: 'Appareils', value: state.access.sessions.length, sub: `${remoteSessions.value} à distance · 30 jours sans usage au plus` },
])

const ago = (t: number) => {
  const m = Math.round((Date.now() - t) / 60_000)
  return m < 2 ? 'à l’instant' : m < 60 ? `il y a ${m} min` : m < 1440 ? `il y a ${Math.round(m / 60)} h` : `il y a ${Math.round(m / 1440)} j`
}
const revoke = ref<Session | null>(null)
const sessionColumns: TableColumn<Session>[] = [
  { accessorKey: 'device', header: 'Appareil / adresse' },
  { id: 'seen', header: 'Dernier usage', meta: { class: { th: 'w-32 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'kind', header: 'Accès', meta: { class: { th: 'w-24' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]

const EVENTS: Record<AccessEvent['event'], { label: string; color: 'success' | 'error' | 'warning' | 'neutral' }> = {
  login: { label: 'Connexion', color: 'success' },
  login_failed: { label: 'Mot de passe erroné', color: 'error' },
  logout: { label: 'Déconnexion', color: 'neutral' },
  revoked: { label: 'Appareil déconnecté', color: 'neutral' },
  refused: { label: 'Adresse refusée', color: 'warning' },
  confirm_failed: { label: 'Confirmation refusée', color: 'error' },
  password_changed: { label: 'Mot de passe changé', color: 'neutral' },
  paired: { label: 'Appareil appairé', color: 'success' },
  pair_failed: { label: 'Code d’appairage erroné', color: 'error' },
}
const eventOf = (e: AccessEvent) => EVENTS[e.event] ?? { label: e.event, color: 'neutral' as const }
const logColumns: TableColumn<AccessEvent>[] = [
  { accessorKey: 'event', header: 'Événement / appareil' },
  { id: 'ip', header: 'Adresse', meta: { class: { th: 'w-32 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'at', header: 'Quand', meta: { class: { th: 'w-28' } } },
]
</script>

<template>
  <PageHeader title="Accès distant" subtitle="Ouvrez le dashboard à vos autres appareils, en HTTPS et avec un mot de passe.">
    <UTooltip :text="pairingHint" :disabled="remoteActive()">
      <UButton icon="i-lucide-qr-code" label="Appairer un appareil" size="lg" color="neutral" variant="outline" :disabled="!remoteActive()" @click="pairingOpen = true" />
    </UTooltip>
    <UButton icon="i-lucide-key-round" :label="'Mot de passe'" size="lg" class="md:w-[205px]" @click="tab = 'settings'; focusField(passwordSet() ? 'pw-current' : 'pw-new')" />
  </PageHeader>
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-16 pb-2.5' }" />

  <TwoColumns>
    <UCard v-if="tab === 'settings'" :ui="{ body: 'p-5 sm:p-5 flex flex-col gap-5' }">
      <div>
        <h2 class="text-[15px] font-semibold text-highlighted">Qui accède au dashboard</h2>
        <p class="mt-1 text-[11px] text-muted">DASHBOARD_ACCESS · appliqué à chaud par le daemon, sans redémarrage.</p>
      </div>
      <URadioGroup v-model="form.mode" :items="MODES" variant="card" indicator="end" :ui="{ fieldset: 'gap-2', label: 'text-[13px]', description: 'text-[11px]' }" />
      <UAlert v-if="errors.DASHBOARD_ACCESS" color="error" variant="soft" :description="errors.DASHBOARD_ACCESS" :ui="{ description: 'text-xs' }" />
      <template v-if="form.mode !== 'local'">
        <div class="grid gap-4 sm:grid-cols-[140px_1fr]">
          <UFormField label="Port HTTPS" :error="errors.DASHBOARD_REMOTE_PORT">
            <UInput v-model="form.port" inputmode="numeric" size="lg" class="w-full" />
          </UFormField>
          <UFormField label="Adresses autorisées (facultatif)" help="IPv4, IPv6 ou CIDR, séparés par des virgules ; vide : toutes." :error="errors.DASHBOARD_ALLOWED_IPS" :ui="{ help: 'text-[11px]' }">
            <UInput v-model="form.allowed" placeholder="192.168.1.0/24, 2001:db8::/32" size="lg" class="w-full font-mono" />
          </UFormField>
        </div>
        <USwitch v-model="form.custom" label="Utiliser mon propre certificat" description="Pour un nom public ou un tunnel ; sinon mkcert le génère pour l’IP du réseau local et le nom de la machine." />
        <div v-if="form.custom" class="grid gap-4 sm:grid-cols-2">
          <UFormField label="Certificat (PEM)"><UInput v-model="form.cert" placeholder="/etc/ssl/dashboard.pem" size="lg" class="w-full font-mono" /></UFormField>
          <UFormField label="Clé privée (PEM)"><UInput v-model="form.key" placeholder="/etc/ssl/dashboard-key.pem" size="lg" class="w-full font-mono" /></UFormField>
        </div>
      </template>
      <USwitch v-model="form.localAuth" label="Exiger le mot de passe sur cette machine aussi" description="DASHBOARD_LOCAL_AUTH : sinon, 127.0.0.1 n’a besoin que du jeton du daemon." />
      <UButton label="Enregistrer l’accès" size="lg" class="self-end" @click="save" />
    </UCard>
    <ResourceTable v-else-if="tab === 'sessions'" storage-key="access-sessions" title="Appareils connectés" :data="state.access.sessions" :columns="sessionColumns" search-key="device" empty="Aucun appareil connecté.">
      <template #device-cell="{ row }">
        <NameCell :icon="row.original.remote ? 'i-lucide-smartphone' : 'i-lucide-laptop'" :name="row.original.device" :sub="`${row.original.ip}${row.original.current ? ' · cet appareil' : ''}`" />
      </template>
      <template #seen-cell="{ row }">{{ ago(row.original.lastSeen) }}</template>
      <template #kind-cell="{ row }"><UBadge :label="row.original.remote ? 'Distant' : 'Local'" :color="row.original.remote ? 'warning' : 'primary'" variant="soft" /></template>
      <template #actions-cell="{ row }">
        <UDropdownMenu :content="{ align: 'end' }" :items="[[{ label: 'Déconnecter l’appareil', icon: 'i-lucide-log-out', color: 'error', disabled: row.original.current, onSelect: () => { revoke = row.original } }]]">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <ResourceTable v-else title="Journal des connexions" storage-key="access-log" :data="state.access.log" :columns="logColumns" empty="Aucune connexion.">
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
      :title="passwordSet() ? 'Changer le mot de passe' : 'Définir le mot de passe'"
      description="Stocké haché (PBKDF2-SHA256), jamais en clair."
      hint="Le changer déconnecte les autres appareils. En ligne de commande : frankenmanager daemon password. Cinq mots de passe erronés verrouillent une adresse 15 minutes."
      @submit="submitPassword"
    >
      <UFormField v-if="passwordSet()" label="Mot de passe actuel" :error="pwErrors.current">
        <UInput id="pw-current" v-model="pw.current" type="password" size="lg" class="w-full" @update:model-value="pwErrors = {}" />
      </UFormField>
      <UFormField label="Nouveau mot de passe" help="Au moins 10 caractères." :error="pwErrors.password" :ui="{ help: 'text-[11px]' }">
        <UInput id="pw-new" v-model="pw.password" type="password" size="lg" class="w-full" @update:model-value="pwErrors = {}" />
      </UFormField>
      <UFormField label="Confirmer" :error="pwErrors.again">
        <UInput v-model="pw.again" type="password" size="lg" class="w-full" @update:model-value="pwErrors = {}" />
      </UFormField>
      <UButton type="submit" label="Enregistrer le mot de passe" size="lg" block :loading="pwBusy" :disabled="!pw.password" />
    </SideForm>
  </TwoColumns>

  <InfoBanner v-if="remoteActive()">
    Ouvrez {{ remoteUrl() }} sur l’autre appareil après y avoir installé la CA racine (Certificats SSL › partager la CA). À distance, les actions destructives redemandent le mot de passe.
  </InfoBanner>
  <InfoBanner v-else-if="accessMode() !== 'local'" tone="warning">L’accès distant reste fermé tant que le dashboard n’a pas de mot de passe.</InfoBanner>
  <InfoBanner v-else>Le dashboard ne répond que sur cette machine (127.0.0.1:{{ c.DASHBOARD_PORT }}, avec le jeton du daemon).</InfoBanner>

  <PairingModal v-model:open="pairingOpen" />
  <ConfirmModal
    v-model:open="confirmOpen"
    eyebrow="Réglage sensible"
    title="Modifier l’accès au dashboard ?"
    description="Ces réglages peuvent ouvrir le dashboard plus largement : confirmez avec le mot de passe."
    confirm="Enregistrer"
    @confirm="apply"
  />
  <ConfirmModal
    :open="!!revoke"
    eyebrow="Accès distant"
    :title="`Déconnecter ${revoke?.device} ?`"
    description="Sa session est révoquée : il devra saisir de nouveau le mot de passe."
    confirm="Déconnecter"
    @update:open="(v: boolean) => { if (!v) revoke = null }"
    @confirm="actions.revokeSession(revoke!.id)"
  />
</template>
