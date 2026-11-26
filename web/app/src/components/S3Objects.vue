<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DropdownMenuItem, TableColumn } from '@nuxt/ui'
import ResourceTable from './ResourceTable.vue'
import NameCell from './NameCell.vue'
import SideForm from './SideForm.vue'
import ConfirmModal from './ConfirmModal.vue'
import { formatBytes, formatDateTime, formatNumber } from '../i18n'
import { actions, bucketObjects, loadObjects, objectDownloadUrl, objectLink, objectPublic, state, toast, type S3Object } from '../composables/useFm'

// Object browser (v0.11.0): `GET /api/v1/s3/buckets/{bucket}/objects?prefix=` (one folder at a time),
// `PUT|GET …/object?key=` (upload, streamed download), `…/object/link` (public URL or presigned link),
// `…/objects/delete` and `…/objects/public` (public-read or private ACL).
const bucket = defineModel<string>('bucket', { required: true })
const { t } = useI18n()

const prefix = ref('')
watch(bucket, () => { prefix.value = '' })
// The daemon lists one folder at a time: the open folder is reloaded on every change (none before a bucket is chosen).
// Skeleton while a folder is read for the first time (its rows are not known yet).
const loadingFolder = ref(false)
watch([bucket, prefix], async ([b, p]) => {
  if (!b) return
  loadingFolder.value = true
  try {
    await loadObjects(b, p)
  } catch (e) {
    toast('error', t('s3Objects.readFailed'), String((e as Error)?.message ?? e))
  } finally {
    if (bucket.value === b && prefix.value === p) loadingFolder.value = false
  }
}, { immediate: true })
const b = computed(() => state.s3.buckets.find((x) => x.name === bucket.value))

interface Row { name: string; folder: boolean; obj?: S3Object; count: number; parent?: boolean }
/** Folder above the open one ('' for the root of the bucket). */
const parent = computed(() => prefix.value.replace(/[^/]+\/$/, ''))
const rows = computed<Row[]>(() => {
  const folders = new Map<string, number>()
  const files: Row[] = []
  for (const o of bucketObjects(bucket.value)) {
    if (!o.key.startsWith(prefix.value)) continue
    const rest = o.key.slice(prefix.value.length)
    const slash = rest.indexOf('/')
    // A subfolder comes in as a "folder/" entry: its content is only read when it is opened.
    if (slash >= 0) folders.set(rest.slice(0, slash + 1), (folders.get(rest.slice(0, slash + 1)) ?? 0) + (o.size || o.modified ? 1 : 0))
    else files.push({ name: rest, folder: false, obj: o, count: 1 })
  }
  // Inside a folder, the first row goes back up one level.
  const up: Row[] = prefix.value ? [{ name: '..', folder: true, count: 0, parent: true }] : []
  return [...up, ...[...folders].map(([name, count]) => ({ name, folder: true, count })), ...files]
})
const crumbs = computed(() => {
  const parts = prefix.value.split('/').filter(Boolean)
  return [{ label: bucket.value, to: '' }, ...parts.map((p, i) => ({ label: p, to: `${parts.slice(0, i + 1).join('/')}/` }))]
})

const ago = (time: number) => {
  const m = Math.round((Date.now() - time) / 60_000)
  return m < 60
    ? t('s3Objects.agoMinutes', { n: formatNumber(m) })
    : m < 1440 ? t('s3Objects.agoHours', { n: formatNumber(Math.round(m / 60)) }) : t('s3Objects.agoDays', { n: formatNumber(Math.round(m / 1440)) })
}

// Object link: if public, its URL; if private, a presigned link valid for `expires`.
const link = reactive({ open: false, key: '', expires: 3600 })
const linkFor = ref<{ url: string; public: boolean; expiresAt: number | null } | null>(null)
watch(() => [link.open, link.key, link.expires] as const, async ([open, key, expires]) => {
  linkFor.value = null
  const o = bucketObjects(bucket.value).find((x) => x.key === key)
  if (!open || !o) return
  try { linkFor.value = await objectLink(bucket.value, o, expires) } catch (e) { toast('error', t('s3Objects.linkFailed'), e instanceof Error ? e.message : String(e)) }
})
async function openObject(o: S3Object) {
  try { window.open((await objectLink(bucket.value, o)).url, '_blank') } catch (e) { toast('error', t('s3Objects.openFailed'), e instanceof Error ? e.message : String(e)) }
}
const expiries = computed(() => [
  { label: t('s3Objects.expiry15m'), value: 900 },
  { label: t('s3Objects.expiry1h'), value: 3600 },
  { label: t('s3Objects.expiry1d'), value: 86_400 },
  { label: t('s3Objects.expiry7d'), value: 604_800 },
])
const copyLink = () => { navigator.clipboard?.writeText(linkFor.value!.url); toast('info', t('s3Objects.linkCopied'), linkFor.value!.public ? t('s3Objects.linkCopiedPublic') : t('s3Objects.linkCopiedPresigned')) }

const drop = ref<string | null>(null)
function menu(r: Row): DropdownMenuItem[][] {
  const o = r.obj!
  const pub = objectPublic(bucket.value, o)
  return [
    [
      { label: pub ? t('s3Objects.copyPublicUrl') : t('s3Objects.presignedLink'), icon: pub ? 'i-lucide-link' : 'i-lucide-clock', onSelect: () => Object.assign(link, { open: true, key: o.key }) },
      { label: t('s3Objects.open'), icon: 'i-lucide-external-link', onSelect: () => void openObject(o) },
      { label: t('s3Objects.download'), icon: 'i-lucide-download', onSelect: () => { window.open(objectDownloadUrl(bucket.value, o.key), '_blank') } },
    ],
    [
      o.public
        ? { label: t('s3Objects.makePrivate'), icon: 'i-lucide-lock', onSelect: () => actions.setObjectsPublic(bucket.value, [o.key], false, prefix.value) }
        : { label: t('s3Objects.makePublic'), icon: 'i-lucide-globe', onSelect: () => actions.setObjectsPublic(bucket.value, [o.key], true, prefix.value) },
    ],
    [{ label: t('common.delete'), icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { drop.value = o.key } }],
  ]
}

const columns = computed<TableColumn<Row>[]>(() => [
  { accessorKey: 'name', header: t('s3Objects.columnName') },
  { id: 'modified', header: t('s3Objects.columnModified'), meta: { class: { th: 'w-28 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'visibility', header: t('s3Objects.columnVisibility'), meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])

// Upload: the chosen files go into the current folder, streamed through the daemon.
const upload = reactive({ public: false, busy: false })
const files = ref<File[]>([])
const input = ref<HTMLInputElement>()
function pick(e: Event) {
  files.value = [...((e.target as HTMLInputElement).files ?? [])]
}
async function submitUpload() {
  if (!files.value.length) { input.value?.click(); return }
  upload.busy = true
  await actions.uploadObjects(bucket.value, prefix.value, files.value, upload.public)
  upload.busy = false
  files.value = []
  if (input.value) input.value.value = ''
}
</script>

<template>
  <!-- No bucket chosen yet: the choice is on the side (below on a phone). -->
  <UCard v-if="!bucket" :ui="{ body: 'p-5 sm:p-5' }">
    <div class="flex flex-col items-center gap-2 py-10 text-center">
      <UIcon name="i-lucide-folder-search" class="size-8 text-dimmed" />
      <h2 class="text-sm font-semibold text-highlighted">{{ t('s3Objects.pickTitle') }}</h2>
      <p class="max-w-xs text-[11px] text-muted">{{ t('s3Objects.pickHelp') }}</p>
    </div>
  </UCard>
  <ResourceTable v-else :title="t('s3Objects.title', { bucket })" :loading="loadingFolder && !rows.some((r) => !r.parent)" :paginate="false" :data="rows" :columns="columns" search-key="name" :empty="t('s3Objects.empty')">
    <template #name-cell="{ row }">
      <button v-if="row.original.parent" type="button" class="text-left" @click="prefix = parent">
        <NameCell icon="i-lucide-corner-left-up" :name="t('s3Objects.parentFolder')" :sub="`${bucket}/${parent}`" />
      </button>
      <button v-else-if="row.original.folder" type="button" class="text-left" @click="prefix += row.original.name">
        <NameCell icon="i-lucide-folder" :name="row.original.name" :sub="row.original.count ? t('s3Objects.objects', { n: formatNumber(row.original.count) }, row.original.count) : t('s3Objects.folder')" />
      </button>
      <NameCell v-else icon="i-lucide-file" :name="row.original.name" :sub="formatBytes(row.original.obj!.size)" />
    </template>
    <template #modified-cell="{ row }">{{ row.original.parent ? '' : row.original.folder ? t('common.none') : ago(row.original.obj!.modified) }}</template>
    <template #visibility-cell="{ row }">
      <template v-if="!row.original.folder">
        <UBadge
          v-if="objectPublic(bucket, row.original.obj!)" :label="b?.public ? t('s3Objects.publicBucket') : t('s3Objects.public')" color="warning" variant="soft"
        />
        <UBadge v-else :label="t('s3Objects.private')" color="neutral" variant="soft" />
      </template>
    </template>
    <template #actions-cell="{ row }">
      <UDropdownMenu v-if="!row.original.folder" :items="menu(row.original)" :content="{ align: 'end' }">
        <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
      </UDropdownMenu>
    </template>
  </ResourceTable>

  <div class="flex flex-col gap-6">
    <UCard :ui="{ body: 'p-5 sm:p-5' }">
      <UFormField label="Bucket">
        <USelect v-model="bucket" :items="state.s3.buckets.map((x) => x.name)" :placeholder="t('s3Objects.bucketPlaceholder')" size="lg" class="w-full" />
      </UFormField>
      <nav v-if="bucket" class="mt-4 flex flex-wrap items-center gap-1 font-mono text-[11px]" :aria-label="t('s3Objects.folder')">
        <template v-for="(cr, i) in crumbs" :key="cr.to">
          <UIcon v-if="i" name="i-lucide-chevron-right" class="size-3 text-dimmed" />
          <button type="button" class="rounded px-1 hover:bg-elevated" :class="cr.to === prefix ? 'text-highlighted' : 'text-muted'" @click="prefix = cr.to">{{ cr.label }}</button>
        </template>
      </nav>
    </UCard>
    <SideForm
      v-if="bucket"
      :title="t('s3Objects.uploadTitle')"
      :description="t('s3Objects.uploadTo', { path: `${bucket}/${prefix}` })"
      :hint="t('s3Objects.uploadHint')"
      @submit="submitUpload"
    >
      <input ref="input" type="file" multiple class="hidden" @change="pick">
      <div class="flex items-center gap-3">
        <UButton color="neutral" variant="outline" icon="i-lucide-paperclip" :label="t('s3Objects.chooseFiles')" @click="input?.click()" />
        <span class="truncate text-[11px] text-muted">{{ files.length ? files.map((f) => f.name).join(', ') : t('s3Objects.noFile') }}</span>
      </div>
      <USwitch v-model="upload.public" :label="t('s3Objects.publicSwitch')" :description="t('s3Objects.publicSwitchHelp')" />
      <UButton type="submit" icon="i-lucide-upload" :label="files.length ? t('s3Objects.uploadFiles', { n: formatNumber(files.length) }, files.length) : t('s3Objects.chooseAndUpload')" size="lg" block :loading="upload.busy" />
    </SideForm>
  </div>

  <UModal v-model:open="link.open" :title="linkFor?.public ? t('s3Objects.publicUrl') : t('s3Objects.temporaryLink')" :description="link.key" :ui="{ content: 'sm:max-w-[480px]', description: 'font-mono text-[11px] truncate' }">
    <template #body>
      <div v-if="linkFor" class="flex flex-col gap-4">
        <UFormField v-if="!linkFor.public" :label="t('s3Objects.validFor')">
          <USelect v-model="link.expires" :items="expiries" class="w-full" />
        </UFormField>
        <pre class="max-h-32 overflow-auto rounded-lg bg-sage-950 p-3 font-mono text-[11px] break-all whitespace-pre-wrap text-sage-200">{{ linkFor.url }}</pre>
        <p class="text-[11px] text-muted">
          <template v-if="linkFor.public">{{ t('s3Objects.publicHelp') }}</template>
          <template v-else>{{ linkFor.expiresAt ? t('s3Objects.presignedUntil', { date: formatDateTime(linkFor.expiresAt) }) : t('s3Objects.presignedHelp') }}</template>
        </p>
        <UButton icon="i-lucide-copy" :label="t('s3Objects.copyLink')" block @click="copyLink" />
      </div>
    </template>
  </UModal>
  <ConfirmModal
    :open="!!drop"
    :eyebrow="t('s3Objects.deleteEyebrow')"
    :title="t('s3Objects.deleteTitle', { name: drop?.split('/').pop() ?? '' })"
    :description="t('s3Objects.deleteDescription', { key: drop ?? '', bucket })"
    :confirm="t('s3Objects.deleteConfirm')"
    @update:open="(v: boolean) => { if (!v) drop = null }"
    @confirm="actions.deleteObjects(bucket, [drop!], prefix)"
  />
</template>
