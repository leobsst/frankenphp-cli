<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { DropdownMenuItem, TableColumn } from '@nuxt/ui'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import TwoColumns from '../components/TwoColumns.vue'
import ResourceTable from '../components/ResourceTable.vue'
import NameCell from '../components/NameCell.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
import ConfirmModal from '../components/ConfirmModal.vue'
import S3Objects from '../components/S3Objects.vue'
import {
  S3_DOMAIN, actions, isServiceBusy, s3Up, serviceEnabled, state, toast, validateBucket,
  type FieldErrors, type S3Bucket, type S3Key,
} from '../composables/useFm'

// Garage S3 server: `frankenmanager service enable|disable s3`, `s3 bucket …`, `s3 key …`, `s3 info`.
const s3 = state.s3
const c = state.config
const tab = ref('buckets')
const tabs = [{ label: 'Buckets', value: 'buckets' }, { label: 'Objets', value: 'objects' }, { label: 'Clés d’accès', value: 'keys' }, { label: 'Laravel', value: 'laravel' }]
// Bucket browsed in the Objects tab.
const browsing = ref(s3.buckets[0]?.name ?? '')
watch(() => s3.buckets.map((b) => b.name), (names) => { if (!names.includes(browsing.value)) browsing.value = names[0] ?? '' })

const enabled = computed(() => serviceEnabled('s3'))
const busy = computed(() => isServiceBusy('s3'))
const up = computed(() => s3Up())

const fmtSize = (mo: number) => (mo >= 1000 ? `${(mo / 1000).toLocaleString('fr-FR', { maximumFractionDigits: 1 })} Go` : `${mo.toLocaleString('fr-FR')} Mo`)
const totalSize = computed(() => s3.buckets.reduce((a, b) => a + b.size, 0))
const stats = computed(() => [
  { label: 'État', value: up.value ? 'En ligne' : enabled.value ? 'Arrêté' : 'Désactivé', sub: up.value ? 'Garage + passerelle ACL' : enabled.value ? 'Démarrera avec la stack' : 'service enable s3' },
  { label: 'Endpoint', value: S3_DOMAIN, sub: `Applications : 127.0.0.1:${c.S3_PORT} · ${c.S3_REGION}` },
  { label: 'Buckets', value: s3.buckets.length, sub: `${fmtSize(totalSize.value)} dans ${c.S3_DIR}` },
])

const keysOf = (b: string) => s3.keys.filter((k) => k.bucket === b).length
const bucketUrl = (b: string) => `https://${S3_DOMAIN}/${b}`
const copy = (label: string, v: string) => { navigator.clipboard?.writeText(v); toast('info', `${label} copié`, v) }

// ---------- Buckets ----------
const bucketColumns: TableColumn<S3Bucket>[] = [
  { accessorKey: 'name', header: 'Bucket / occupation' },
  { id: 'visibility', header: 'Visibilité', meta: { class: { th: 'w-44 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'keys', header: 'Clés', meta: { class: { th: 'w-16 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]
const confirm = reactive({ open: false, kind: 'empty' as 'empty' | 'delete' | 'public', bucket: '' })
const ask = (kind: typeof confirm.kind, bucket: string) => Object.assign(confirm, { open: true, kind, bucket })
const confirmed = computed(() => s3.buckets.find((b) => b.name === confirm.bucket))
const confirmText = computed(() => {
  const b = confirmed.value
  const n = (b?.objects ?? 0).toLocaleString('fr-FR')
  if (confirm.kind === 'public') return {
    eyebrow: 'Exposition des données', title: `Rendre ${confirm.bucket} public ?`, confirm: 'Rendre public',
    description: `Tous les objets de ${confirm.bucket} pourront être lus et téléchargés sans identifiants par quiconque joint cette machine, y compris ceux stockés en privé.`,
    hint: 'Pour ne rendre publics que certains objets, gardez le bucket privé et utilisez l’ACL public-read (visibilité « public » de Laravel).',
  }
  if (confirm.kind === 'delete') return {
    eyebrow: 'Action destructive', title: `Supprimer le bucket ${confirm.bucket} ?`, confirm: 'Supprimer le bucket',
    description: b?.objects
      ? `Le bucket contient ${n} objets : ils seront supprimés avant lui (--force), ainsi que ses ${keysOf(confirm.bucket)} clé(s) d’accès.`
      : `Le bucket est vide. Ses ${keysOf(confirm.bucket)} clé(s) d’accès seront aussi supprimées.`,
    hint: 'Ses hôtes et enregistrements DNS sont retirés.',
  }
  return {
    eyebrow: 'Action destructive', title: `Vider le bucket ${confirm.bucket} ?`, confirm: 'Vider le bucket',
    description: `Les ${n} objets de ${confirm.bucket} seront supprimés. Cette action est irréversible.`,
    hint: 'Le bucket et ses clés d’accès sont conservés.',
  }
})
function onConfirm() {
  if (confirm.kind === 'public') actions.setBucketPublic(confirm.bucket, true)
  else if (confirm.kind === 'delete') actions.deleteBucket(confirm.bucket)
  else actions.emptyBucket(confirm.bucket)
}

function bucketMenu(b: S3Bucket): DropdownMenuItem[][] {
  return [
    [
      { label: 'Parcourir les objets', icon: 'i-lucide-folder-open', onSelect: () => { browsing.value = b.name; tab.value = 'objects' } },
      { label: 'Copier l’URL', icon: 'i-lucide-copy', onSelect: () => copy('URL', bucketUrl(b.name)) },
      b.public
        ? { label: 'Rendre privé', icon: 'i-lucide-lock', onSelect: () => actions.setBucketPublic(b.name, false) }
        : { label: 'Rendre public', icon: 'i-lucide-globe', onSelect: () => ask('public', b.name) },
      { label: 'Créer une clé d’accès', icon: 'i-lucide-key-round', onSelect: () => { tab.value = 'keys'; keyForm.bucket = b.name } },
    ],
    [
      { label: 'Vider', icon: 'i-lucide-eraser', color: 'error', disabled: !b.objects, onSelect: () => ask('empty', b.name) },
      { label: 'Supprimer', icon: 'i-lucide-trash-2', color: 'error', onSelect: () => ask('delete', b.name) },
    ],
  ]
}

const bucketForm = reactive({ name: '', public: false })
const bucketError = ref<string>()
const bucketLive = computed(() => (bucketForm.name ? validateBucket(bucketForm.name) : null))
async function submitBucket() {
  bucketError.value = (await actions.createBucket(bucketForm.name.trim(), bucketForm.public)) ?? undefined
  if (!bucketError.value) Object.assign(bucketForm, { name: '', public: false })
}

// ---------- Access keys ----------
const revealed = ref<string | null>(null)
const keyColumns: TableColumn<S3Key>[] = [
  { accessorKey: 'name', header: 'Clé / identifiant' },
  { id: 'perm', header: 'Droits', meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]
function keyMenu(k: S3Key): DropdownMenuItem[][] {
  return [
    [
      { label: revealed.value === k.id ? 'Masquer le secret' : 'Afficher le secret', icon: revealed.value === k.id ? 'i-lucide-eye-off' : 'i-lucide-eye', onSelect: () => { revealed.value = revealed.value === k.id ? null : k.id } },
      { label: 'Copier l’identifiant', icon: 'i-lucide-copy', onSelect: () => copy('Identifiant', k.id) },
      { label: 'Copier le secret', icon: 'i-lucide-copy', onSelect: () => copy('Secret', k.secret) },
    ],
    [{ label: 'Supprimer', icon: 'i-lucide-trash-2', color: 'error', onSelect: () => actions.deleteS3Key(k.id) }],
  ]
}
const keyForm = reactive({ bucket: s3.buckets[0]?.name ?? '', name: '', readOnly: false })
watch(() => s3.buckets.map((b) => b.name), (names) => { if (!names.includes(keyForm.bucket)) keyForm.bucket = names[0] ?? '' })
const keyErrors = ref<FieldErrors>({})
async function submitKey() {
  keyErrors.value = (await actions.createS3Key(keyForm.bucket, keyForm.name, keyForm.readOnly)) ?? {}
  if (!Object.keys(keyErrors.value).length) keyForm.name = ''
}

// ---------- Laravel (`frankenmanager s3 info`) ----------
const reveal = ref(false)
const mask = (v: string) => (reveal.value ? v : '••••••••')
const envBucket = ref(s3.buckets[0]?.name ?? 'media')
watch(() => s3.buckets.map((b) => b.name), (names) => { if (!names.includes(envBucket.value)) envBucket.value = names[0] ?? 'media' })
const envFile = (secret: (v: string) => string) => [
  'FILESYSTEM_DISK=s3',
  `AWS_ACCESS_KEY_ID=${c.S3_ACCESS_KEY_ID}`,
  `AWS_SECRET_ACCESS_KEY=${secret(c.S3_SECRET_ACCESS_KEY)}`,
  `AWS_DEFAULT_REGION=${c.S3_REGION}`,
  `AWS_BUCKET=${envBucket.value}`,
  `AWS_ENDPOINT=https://${S3_DOMAIN}`,
  '# Storage::url()',
  `AWS_URL=https://${S3_DOMAIN}/${envBucket.value}`,
  'AWS_USE_PATH_STYLE_ENDPOINT=true',
].join('\n')
function copyEnv() {
  navigator.clipboard?.writeText(envFile((v) => v))
  toast('info', 'Variables copiées', 'Collez-les dans le .env de votre projet Laravel.')
}
</script>

<template>
  <PageHeader title="Stockage S3" subtitle="Serveur S3 Garage pour les disques s3 de vos applications.">
    <UButton
      v-if="enabled" color="neutral" variant="outline" icon="i-lucide-power-off" label="Désactiver S3" size="lg" class="md:w-[205px]"
      :loading="busy" @click="actions.setService('s3', false)"
    />
    <UButton v-else icon="i-lucide-power" label="Activer S3" size="lg" class="md:w-[205px]" :loading="busy" @click="actions.setService('s3', true)" />
  </PageHeader>
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-4 md:me-16 pb-2.5' }" />

  <TwoColumns>
    <!-- Buckets -->
    <template v-if="tab === 'buckets'">
      <ResourceTable title="Buckets" storage-key="s3-buckets" :data="s3.buckets" :columns="bucketColumns" search-key="name" empty="Aucun bucket. Créez-en un avec le formulaire.">
        <template #name-cell="{ row }">
          <NameCell
            icon="i-lucide-folder" :name="row.original.name"
            :sub="`${row.original.objects.toLocaleString('fr-FR')} objets · ${fmtSize(row.original.size)}`"
          />
        </template>
        <template #visibility-cell="{ row }">
          <UBadge v-if="row.original.public" label="Public" color="warning" variant="soft" />
          <span v-else class="inline-flex flex-wrap items-center gap-1.5">
            <UBadge label="Privé" color="neutral" variant="soft" />
            <span v-if="row.original.publicObjects" class="text-[11px]">{{ row.original.publicObjects.toLocaleString('fr-FR') }} objets publics</span>
          </span>
        </template>
        <template #keys-cell="{ row }">{{ keysOf(row.original.name) }}</template>
        <template #actions-cell="{ row }">
          <UDropdownMenu :items="bucketMenu(row.original)" :content="{ align: 'end' }">
            <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
          </UDropdownMenu>
        </template>
      </ResourceTable>

      <SideForm
        title="Créer un bucket"
        description="Accessible à la clé globale, servi en path style et en virtual-host style."
        :hint="`https://${S3_DOMAIN}/<bucket> et https://<bucket>.${S3_DOMAIN} : le certificat, les hosts et le DNS sont mis à jour.`"
        @submit="submitBucket"
      >
        <UFormField label="Nom du bucket" :error="bucketError ?? bucketLive ?? undefined" help="3 à 63 minuscules, chiffres et tirets, sans point.">
          <UInput v-model="bucketForm.name" placeholder="media" size="lg" class="w-full" :disabled="!enabled" @update:model-value="bucketError = undefined" />
        </UFormField>
        <USwitch
          v-model="bucketForm.public" label="Bucket public" :disabled="!enabled"
          description="Tous les objets sont lisibles sans identifiants. Sinon, la visibilité se règle objet par objet (ACL)."
        />
        <UButton type="submit" label="Créer le bucket" size="lg" block :disabled="!enabled || !bucketForm.name || !!bucketLive" />
      </SideForm>
    </template>

    <!-- Access keys -->
    <template v-else-if="tab === 'keys'">
      <ResourceTable title="Clés limitées à un bucket" storage-key="s3-keys" :data="s3.keys" :columns="keyColumns" search-key="name" empty="Aucune clé : les applications utilisent la clé globale.">
        <template #name-cell="{ row }">
          <NameCell icon="i-lucide-key-round" :name="row.original.name" :sub="revealed === row.original.id ? `${row.original.id} · ${row.original.secret}` : row.original.id" />
        </template>
        <template #perm-cell="{ row }">
          <UBadge :label="row.original.readOnly ? 'Lecture seule' : 'Lecture et écriture'" :color="row.original.readOnly ? 'neutral' : 'primary'" variant="soft" />
        </template>
        <template #actions-cell="{ row }">
          <UDropdownMenu :items="keyMenu(row.original)" :content="{ align: 'end' }">
            <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
          </UDropdownMenu>
        </template>
      </ResourceTable>

      <SideForm
        title="Créer une clé d’accès"
        description="Pour une application qui ne doit voir que son bucket."
        hint="La clé globale (S3_ACCESS_KEY_ID) reste autorisée sur tous les buckets. Supprimer un bucket supprime ses clés."
        @submit="submitKey"
      >
        <UFormField label="Bucket" :error="keyErrors.bucket">
          <USelect v-model="keyForm.bucket" :items="s3.buckets.map((b) => b.name)" size="lg" class="w-full" :disabled="!enabled || !s3.buckets.length" />
        </UFormField>
        <UFormField label="Nom" :error="keyErrors.name" :help="`Enregistrée sous ${keyForm.bucket || '<bucket>'}/${keyForm.name || 'app'}`">
          <UInput v-model="keyForm.name" placeholder="app" size="lg" class="w-full" :disabled="!enabled" />
        </UFormField>
        <USwitch v-model="keyForm.readOnly" label="Lecture seule" description="La clé lit les objets sans pouvoir les écrire ni les supprimer." :disabled="!enabled" />
        <UButton type="submit" label="Créer la clé" size="lg" block :disabled="!enabled || !keyForm.bucket" />
      </SideForm>
    </template>

    <!-- Objects -->
    <S3Objects v-else-if="tab === 'objects' && browsing" v-model:bucket="browsing" />

    <!-- Laravel -->
    <template v-else>
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <div class="flex flex-wrap items-center gap-3">
          <div class="min-w-0">
            <h2 class="text-[15px] font-semibold text-highlighted">Variables pour Laravel</h2>
            <p class="mt-1 text-[11px] text-muted">Disque s3 avec la clé globale, comme le montre s3 info.</p>
          </div>
          <div class="ml-auto flex items-center gap-2">
            <USelect v-model="envBucket" :items="s3.buckets.map((b) => b.name)" size="sm" class="w-32" aria-label="Bucket" />
            <UButton color="neutral" variant="outline" icon="i-lucide-copy" label="Copier" size="sm" @click="copyEnv" />
          </div>
        </div>
        <pre class="mt-3.5 max-h-[420px] overflow-auto rounded-lg bg-sage-950 p-4 font-mono text-xs leading-[19px] text-sage-200">{{ envFile(mask) }}</pre>
        <p class="mt-3 text-[11px] leading-4 text-muted">
          Les applications PHP résolvent aussi {{ S3_DOMAIN }} et font confiance à la CA mkcert : les URL signées (<code class="font-mono">temporaryUrl()</code>) s’ouvrent dans le navigateur et sur les autres appareils.
        </p>
      </UCard>

      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <div class="flex items-center gap-3">
          <h2 class="text-base font-semibold text-highlighted">Clé globale</h2>
          <UButton
            color="neutral" variant="ghost" size="sm" class="ml-auto" :icon="reveal ? 'i-lucide-eye-off' : 'i-lucide-eye'"
            :label="reveal ? 'Masquer' : 'Afficher'" :aria-pressed="reveal" @click="reveal = !reveal"
          />
        </div>
        <p class="mt-1.5 text-[11px] text-muted">Générée au premier démarrage, autorisée sur tous les buckets.</p>
        <dl class="mt-5 flex flex-col gap-3 text-xs">
          <div><dt class="text-muted">Access key ID</dt><dd class="mt-0.5 truncate font-mono text-highlighted">{{ c.S3_ACCESS_KEY_ID }}</dd></div>
          <div><dt class="text-muted">Secret access key</dt><dd class="mt-0.5 truncate font-mono text-highlighted">{{ mask(c.S3_SECRET_ACCESS_KEY) }}</dd></div>
          <div><dt class="text-muted">Région</dt><dd class="mt-0.5 font-mono text-highlighted">{{ c.S3_REGION }}</dd></div>
          <div><dt class="text-muted">Production (sans TLS)</dt><dd class="mt-0.5 font-mono text-highlighted">http://127.0.0.1:{{ c.S3_PORT }}</dd></div>
        </dl>
        <UButton color="neutral" variant="outline" label="Modifier dans Configuration" to="/configuration" block class="mt-5" />
        <p class="mt-3 text-[10px] leading-[15px] text-muted">Un port, la région ou un identifiant modifié s’applique au prochain stop / start. En ligne de commande : <code class="font-mono">s3 aws</code> (AWS CLI préconfigurée) et <code class="font-mono">s3 garage</code>.</p>
      </UCard>
    </template>
  </TwoColumns>

  <InfoBanner v-if="!enabled">S3 est désactivé. L’activer construit la passerelle ACL, démarre Garage et crée le certificat, les hosts, la route du reverse proxy et les enregistrements DNS de {{ S3_DOMAIN }} et des buckets. Les objets restent dans {{ c.S3_DIR }} quand on le désactive.</InfoBanner>
  <InfoBanner v-else>Les buckets sont privés par défaut. Comme sur AWS, un objet écrit avec l’ACL public-read (visibilité « public » de Laravel) se lit sans identifiants ; les autres répondent 403.</InfoBanner>

  <ConfirmModal
    v-model:open="confirm.open"
    :eyebrow="confirmText.eyebrow"
    :title="confirmText.title"
    :description="confirmText.description"
    :type-to-confirm="confirm.kind === 'public' ? undefined : confirm.bucket.toUpperCase()"
    :hint="confirmText.hint"
    :confirm="confirmText.confirm"
    @confirm="onConfirm"
  />
</template>
