<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { DropdownMenuItem, TableColumn } from '@nuxt/ui'
import { useI18n } from 'vue-i18n'
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
import { formatMegabytes, formatNumber } from '../i18n'

const { t } = useI18n()

// Garage S3 server: `frankenmanager service enable|disable s3`, `s3 bucket …`, `s3 key …`, `s3 info`.
const s3 = state.s3
const c = state.config
const tab = ref('buckets')
const tabs = computed(() => [
  { label: t('s3.tabs.buckets'), value: 'buckets' }, { label: t('s3.tabs.objects'), value: 'objects' },
  { label: t('s3.tabs.keys'), value: 'keys' }, { label: t('s3.tabs.laravel'), value: 'laravel' },
])
// Bucket browsed in the Objects tab.
const browsing = ref(s3.buckets[0]?.name ?? '')
watch(() => s3.buckets.map((b) => b.name), (names) => { if (!names.includes(browsing.value)) browsing.value = names[0] ?? '' })

const enabled = computed(() => serviceEnabled('s3'))
const busy = computed(() => isServiceBusy('s3'))
const up = computed(() => s3Up())

const totalSize = computed(() => s3.buckets.reduce((a, b) => a + b.size, 0))
const stats = computed(() => [
  {
    label: t('common.status'),
    value: up.value ? t('s3.stats.online') : enabled.value ? t('s3.stats.stopped') : t('s3.stats.disabled'),
    sub: up.value ? t('s3.stats.onlineSub') : enabled.value ? t('s3.stats.stoppedSub') : 'service enable s3',
  },
  { label: t('s3.stats.endpoint'), value: S3_DOMAIN, sub: t('s3.stats.endpointSub', { port: c.S3_PORT, region: c.S3_REGION }) },
  { label: t('s3.tabs.buckets'), value: s3.buckets.length, sub: t('s3.stats.bucketsSub', { size: formatMegabytes(totalSize.value), dir: c.S3_DIR }) },
])

const keysOf = (b: string) => s3.keys.filter((k) => k.bucket === b).length
const bucketUrl = (b: string) => `https://${S3_DOMAIN}/${b}`
const copy = (title: string, v: string) => { navigator.clipboard?.writeText(v); toast('info', title, v) }

// ---------- Buckets ----------
const bucketColumns = computed<TableColumn<S3Bucket>[]>(() => [
  { accessorKey: 'name', header: t('s3.columns.bucket') },
  { id: 'visibility', header: t('s3.columns.visibility'), meta: { class: { th: 'w-44 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'keys', header: t('s3.columns.keys'), meta: { class: { th: 'w-16 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])
const confirm = reactive({ open: false, kind: 'empty' as 'empty' | 'delete' | 'public', bucket: '' })
const ask = (kind: typeof confirm.kind, bucket: string) => Object.assign(confirm, { open: true, kind, bucket })
const confirmed = computed(() => s3.buckets.find((b) => b.name === confirm.bucket))
const confirmText = computed(() => {
  const count = confirmed.value?.objects ?? 0
  const params = { bucket: confirm.bucket, n: formatNumber(count), keys: keysOf(confirm.bucket) }
  if (confirm.kind === 'public') return {
    eyebrow: t('s3.confirm.public.eyebrow'), title: t('s3.confirm.public.title', params), confirm: t('s3.confirm.public.confirm'),
    description: t('s3.confirm.public.description', params),
    hint: t('s3.confirm.public.hint'),
  }
  if (confirm.kind === 'delete') return {
    eyebrow: t('s3.confirm.destructive'), title: t('s3.confirm.delete.title', params), confirm: t('s3.confirm.delete.confirm'),
    description: count ? t('s3.confirm.delete.description', params, count) : t('s3.confirm.delete.descriptionEmpty', params),
    hint: t('s3.confirm.delete.hint'),
  }
  return {
    eyebrow: t('s3.confirm.destructive'), title: t('s3.confirm.empty.title', params), confirm: t('s3.confirm.empty.confirm'),
    description: t('s3.confirm.empty.description', params, count),
    hint: t('s3.confirm.empty.hint'),
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
      { label: t('s3.menu.browse'), icon: 'i-lucide-folder-open', onSelect: () => { browsing.value = b.name; tab.value = 'objects' } },
      { label: t('s3.menu.copyUrl'), icon: 'i-lucide-copy', onSelect: () => copy(t('s3.copied.url'), bucketUrl(b.name)) },
      b.public
        ? { label: t('s3.menu.makePrivate'), icon: 'i-lucide-lock', onSelect: () => actions.setBucketPublic(b.name, false) }
        : { label: t('s3.menu.makePublic'), icon: 'i-lucide-globe', onSelect: () => ask('public', b.name) },
      { label: t('s3.keyForm.title'), icon: 'i-lucide-key-round', onSelect: () => { tab.value = 'keys'; keyForm.bucket = b.name } },
    ],
    [
      { label: t('s3.menu.empty'), icon: 'i-lucide-eraser', color: 'error', disabled: !b.objects, onSelect: () => ask('empty', b.name) },
      { label: t('common.delete'), icon: 'i-lucide-trash-2', color: 'error', onSelect: () => ask('delete', b.name) },
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
const keyColumns = computed<TableColumn<S3Key>[]>(() => [
  { accessorKey: 'name', header: t('s3.columns.key') },
  { id: 'perm', header: t('s3.columns.permissions'), meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])
function keyMenu(k: S3Key): DropdownMenuItem[][] {
  return [
    [
      { label: revealed.value === k.id ? t('s3.menu.hideSecret') : t('s3.menu.showSecret'), icon: revealed.value === k.id ? 'i-lucide-eye-off' : 'i-lucide-eye', onSelect: () => { revealed.value = revealed.value === k.id ? null : k.id } },
      { label: t('s3.menu.copyId'), icon: 'i-lucide-copy', onSelect: () => copy(t('s3.copied.id'), k.id) },
      { label: t('s3.menu.copySecret'), icon: 'i-lucide-copy', onSelect: () => copy(t('s3.copied.secret'), k.secret) },
    ],
    [{ label: t('common.delete'), icon: 'i-lucide-trash-2', color: 'error', onSelect: () => actions.deleteS3Key(k.id) }],
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
  toast('info', t('s3.laravel.copied'), t('s3.laravel.copiedHelp'))
}
</script>

<template>
  <PageHeader :title="t('nav.s3')" :subtitle="t('s3.subtitle')">
    <UButton
      v-if="enabled" color="neutral" variant="outline" icon="i-lucide-power-off" :label="t('s3.disable')" size="lg" class="md:w-[205px]"
      :loading="busy" @click="actions.setService('s3', false)"
    />
    <UButton v-else icon="i-lucide-power" :label="t('s3.enable')" size="lg" class="md:w-[205px]" :loading="busy" @click="actions.setService('s3', true)" />
  </PageHeader>
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-4 md:me-16 pb-2.5' }" />

  <TwoColumns>
    <!-- Buckets -->
    <template v-if="tab === 'buckets'">
      <ResourceTable :title="t('s3.tabs.buckets')" storage-key="s3-buckets" :data="s3.buckets" :columns="bucketColumns" search-key="name" :empty="t('s3.empty.buckets')">
        <template #name-cell="{ row }">
          <NameCell
            icon="i-lucide-folder" :name="row.original.name"
            :sub="t('s3.bucketSub', { n: formatNumber(row.original.objects), size: formatMegabytes(row.original.size) }, row.original.objects)"
          />
        </template>
        <template #visibility-cell="{ row }">
          <UBadge v-if="row.original.public" :label="t('s3.badges.public')" color="warning" variant="soft" />
          <span v-else class="inline-flex flex-wrap items-center gap-1.5">
            <UBadge :label="t('s3.badges.private')" color="neutral" variant="soft" />
            <span v-if="row.original.publicObjects" class="text-[11px]">{{ t('s3.publicObjects', { n: formatNumber(row.original.publicObjects) }, row.original.publicObjects) }}</span>
          </span>
        </template>
        <template #keys-cell="{ row }">{{ keysOf(row.original.name) }}</template>
        <template #actions-cell="{ row }">
          <UDropdownMenu :items="bucketMenu(row.original)" :content="{ align: 'end' }">
            <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
          </UDropdownMenu>
        </template>
      </ResourceTable>

      <SideForm
        :title="t('s3.bucketForm.title')"
        :description="t('s3.bucketForm.description')"
        :hint="t('s3.bucketForm.hint', { domain: S3_DOMAIN })"
        @submit="submitBucket"
      >
        <UFormField :label="t('s3.bucketForm.name')" :error="bucketError ?? bucketLive ?? undefined" :help="t('s3.bucketForm.nameHelp')">
          <UInput v-model="bucketForm.name" placeholder="media" size="lg" class="w-full" :disabled="!enabled" @update:model-value="bucketError = undefined" />
        </UFormField>
        <USwitch
          v-model="bucketForm.public" :label="t('s3.bucketForm.public')" :disabled="!enabled"
          :description="t('s3.bucketForm.publicHelp')"
        />
        <UButton type="submit" :label="t('s3.bucketForm.submit')" size="lg" block :disabled="!enabled || !bucketForm.name || !!bucketLive" />
      </SideForm>
    </template>

    <!-- Access keys -->
    <template v-else-if="tab === 'keys'">
      <ResourceTable :title="t('s3.keysTitle')" storage-key="s3-keys" :data="s3.keys" :columns="keyColumns" search-key="name" :empty="t('s3.empty.keys')">
        <template #name-cell="{ row }">
          <NameCell icon="i-lucide-key-round" :name="row.original.name" :sub="revealed === row.original.id ? `${row.original.id} · ${row.original.secret}` : row.original.id" />
        </template>
        <template #perm-cell="{ row }">
          <UBadge :label="row.original.readOnly ? t('s3.badges.readOnly') : t('s3.badges.readWrite')" :color="row.original.readOnly ? 'neutral' : 'primary'" variant="soft" />
        </template>
        <template #actions-cell="{ row }">
          <UDropdownMenu :items="keyMenu(row.original)" :content="{ align: 'end' }">
            <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
          </UDropdownMenu>
        </template>
      </ResourceTable>

      <SideForm
        :title="t('s3.keyForm.title')"
        :description="t('s3.keyForm.description')"
        :hint="t('s3.keyForm.hint')"
        @submit="submitKey"
      >
        <UFormField :label="t('s3.keyForm.bucket')" :error="keyErrors.bucket">
          <USelect v-model="keyForm.bucket" :items="s3.buckets.map((b) => b.name)" size="lg" class="w-full" :disabled="!enabled || !s3.buckets.length" />
        </UFormField>
        <UFormField :label="t('common.name')" :error="keyErrors.name" :help="t('s3.keyForm.nameHelp', { path: `${keyForm.bucket || '<bucket>'}/${keyForm.name || 'app'}` })">
          <UInput v-model="keyForm.name" placeholder="app" size="lg" class="w-full" :disabled="!enabled" />
        </UFormField>
        <USwitch v-model="keyForm.readOnly" :label="t('s3.badges.readOnly')" :description="t('s3.keyForm.readOnlyHelp')" :disabled="!enabled" />
        <UButton type="submit" :label="t('s3.keyForm.submit')" size="lg" block :disabled="!enabled || !keyForm.bucket" />
      </SideForm>
    </template>

    <!-- Objects -->
    <S3Objects v-else-if="tab === 'objects' && browsing" v-model:bucket="browsing" />

    <!-- Laravel -->
    <template v-else>
      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <div class="flex flex-wrap items-center gap-3">
          <div class="min-w-0">
            <h2 class="text-[15px] font-semibold text-highlighted">{{ t('s3.laravel.title') }}</h2>
            <p class="mt-1 text-[11px] text-muted">{{ t('s3.laravel.subtitle') }}</p>
          </div>
          <div class="ml-auto flex items-center gap-2">
            <USelect v-model="envBucket" :items="s3.buckets.map((b) => b.name)" size="sm" class="w-32" :aria-label="t('s3.keyForm.bucket')" />
            <UButton color="neutral" variant="outline" icon="i-lucide-copy" :label="t('common.copy')" size="sm" @click="copyEnv" />
          </div>
        </div>
        <pre class="mt-3.5 max-h-[420px] overflow-auto rounded-lg bg-sage-950 p-4 font-mono text-xs leading-[19px] text-sage-200">{{ envFile(mask) }}</pre>
        <i18n-t keypath="s3.laravel.note" tag="p" scope="global" class="mt-3 text-[11px] leading-4 text-muted">
          <template #domain>{{ S3_DOMAIN }}</template>
          <template #method><code class="font-mono">temporaryUrl()</code></template>
        </i18n-t>
      </UCard>

      <UCard :ui="{ body: 'p-5 sm:p-5' }">
        <div class="flex items-center gap-3">
          <h2 class="text-base font-semibold text-highlighted">{{ t('s3.globalKey.title') }}</h2>
          <UButton
            color="neutral" variant="ghost" size="sm" class="ml-auto" :icon="reveal ? 'i-lucide-eye-off' : 'i-lucide-eye'"
            :label="reveal ? t('common.hide') : t('common.show')" :aria-pressed="reveal" @click="reveal = !reveal"
          />
        </div>
        <p class="mt-1.5 text-[11px] text-muted">{{ t('s3.globalKey.subtitle') }}</p>
        <dl class="mt-5 flex flex-col gap-3 text-xs">
          <div><dt class="text-muted">Access key ID</dt><dd class="mt-0.5 truncate font-mono text-highlighted">{{ c.S3_ACCESS_KEY_ID }}</dd></div>
          <div><dt class="text-muted">Secret access key</dt><dd class="mt-0.5 truncate font-mono text-highlighted">{{ mask(c.S3_SECRET_ACCESS_KEY) }}</dd></div>
          <div><dt class="text-muted">{{ t('s3.globalKey.region') }}</dt><dd class="mt-0.5 font-mono text-highlighted">{{ c.S3_REGION }}</dd></div>
          <div><dt class="text-muted">{{ t('s3.globalKey.production') }}</dt><dd class="mt-0.5 font-mono text-highlighted">http://127.0.0.1:{{ c.S3_PORT }}</dd></div>
        </dl>
        <UButton color="neutral" variant="outline" :label="t('s3.globalKey.edit')" to="/configuration" block class="mt-5" />
        <i18n-t keypath="s3.globalKey.note" tag="p" scope="global" class="mt-3 text-[10px] leading-[15px] text-muted">
          <template #aws><code class="font-mono">s3 aws</code></template>
          <template #garage><code class="font-mono">s3 garage</code></template>
        </i18n-t>
      </UCard>
    </template>
  </TwoColumns>

  <InfoBanner v-if="!enabled">{{ t('s3.banner.disabled', { domain: S3_DOMAIN, dir: c.S3_DIR }) }}</InfoBanner>
  <InfoBanner v-else>{{ t('s3.banner.enabled') }}</InfoBanner>

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
