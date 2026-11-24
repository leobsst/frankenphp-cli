<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { DropdownMenuItem, TableColumn } from '@nuxt/ui'
import ResourceTable from './ResourceTable.vue'
import NameCell from './NameCell.vue'
import SideForm from './SideForm.vue'
import ConfirmModal from './ConfirmModal.vue'
import { actions, bucketObjects, loadObjects, objectDownloadUrl, objectLink, objectPublic, state, toast, type S3Object } from '../composables/useFm'

// Explorateur d'objets (v0.11.0) : `GET /api/v1/s3/buckets/{bucket}/objects?prefix=` (un dossier à la fois),
// `PUT|GET …/object?key=` (téléversement, téléchargement en flux), `…/object/link` (URL publique ou lien pré-signé),
// `…/objects/delete` et `…/objects/public` (ACL public-read ou private).
const bucket = defineModel<string>('bucket', { required: true })

const prefix = ref('')
watch(bucket, () => { prefix.value = '' })
// Le daemon liste un dossier à la fois : le dossier ouvert est relu à chaque changement.
watch([bucket, prefix], ([b, p]) => { void loadObjects(b, p).catch((e) => toast('error', 'Lecture impossible', String(e?.message ?? e))) }, { immediate: true })
const b = computed(() => state.s3.buckets.find((x) => x.name === bucket.value))

interface Row { name: string; folder: boolean; obj?: S3Object; count: number }
const rows = computed<Row[]>(() => {
  const folders = new Map<string, number>()
  const files: Row[] = []
  for (const o of bucketObjects(bucket.value)) {
    if (!o.key.startsWith(prefix.value)) continue
    const rest = o.key.slice(prefix.value.length)
    const slash = rest.indexOf('/')
    // Un sous-dossier arrive comme une entrée « dossier/ » : son contenu n'est lu qu'en l'ouvrant.
    if (slash >= 0) folders.set(rest.slice(0, slash + 1), (folders.get(rest.slice(0, slash + 1)) ?? 0) + (o.size || o.modified ? 1 : 0))
    else files.push({ name: rest, folder: false, obj: o, count: 1 })
  }
  return [...[...folders].map(([name, count]) => ({ name, folder: true, count })), ...files]
})
const crumbs = computed(() => {
  const parts = prefix.value.split('/').filter(Boolean)
  return [{ label: bucket.value, to: '' }, ...parts.map((p, i) => ({ label: p, to: `${parts.slice(0, i + 1).join('/')}/` }))]
})

const size = (n: number) => (n >= 1_000_000 ? `${(n / 1_000_000).toLocaleString('fr-FR', { maximumFractionDigits: 1 })} Mo` : n >= 1000 ? `${Math.round(n / 1000)} Ko` : `${n} o`)
const ago = (t: number) => {
  const m = Math.round((Date.now() - t) / 60_000)
  return m < 60 ? `il y a ${m} min` : m < 1440 ? `il y a ${Math.round(m / 60)} h` : `il y a ${Math.round(m / 1440)} j`
}

// Lien d'un objet : public, son URL ; privé, un lien pré-signé valable `expires`.
const link = reactive({ open: false, key: '', expires: 3600 })
const linkFor = ref<{ url: string; public: boolean; expiresAt: number | null } | null>(null)
watch(() => [link.open, link.key, link.expires] as const, async ([open, key, expires]) => {
  linkFor.value = null
  const o = bucketObjects(bucket.value).find((x) => x.key === key)
  if (!open || !o) return
  try { linkFor.value = await objectLink(bucket.value, o, expires) } catch (e) { toast('error', 'Lien impossible', e instanceof Error ? e.message : String(e)) }
})
async function openObject(o: S3Object) {
  try { window.open((await objectLink(bucket.value, o)).url, '_blank') } catch (e) { toast('error', 'Ouverture impossible', e instanceof Error ? e.message : String(e)) }
}
const expiries = [{ label: '15 minutes', value: 900 }, { label: '1 heure', value: 3600 }, { label: '1 jour', value: 86_400 }, { label: '7 jours (maximum)', value: 604_800 }]
const copyLink = () => { navigator.clipboard?.writeText(linkFor.value!.url); toast('info', 'Lien copié', linkFor.value!.public ? 'URL publique.' : 'Lien temporaire pré-signé.') }

const drop = ref<string | null>(null)
function menu(r: Row): DropdownMenuItem[][] {
  const o = r.obj!
  const pub = objectPublic(bucket.value, o)
  return [
    [
      { label: pub ? 'Copier l’URL publique' : 'Lien temporaire…', icon: pub ? 'i-lucide-link' : 'i-lucide-clock', onSelect: () => Object.assign(link, { open: true, key: o.key }) },
      { label: 'Ouvrir', icon: 'i-lucide-external-link', onSelect: () => void openObject(o) },
      { label: 'Télécharger', icon: 'i-lucide-download', onSelect: () => { window.open(objectDownloadUrl(bucket.value, o.key), '_blank') } },
    ],
    [
      o.public
        ? { label: 'Rendre privé', icon: 'i-lucide-lock', onSelect: () => actions.setObjectsPublic(bucket.value, [o.key], false, prefix.value) }
        : { label: 'Rendre public (public-read)', icon: 'i-lucide-globe', onSelect: () => actions.setObjectsPublic(bucket.value, [o.key], true, prefix.value) },
    ],
    [{ label: 'Supprimer', icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { drop.value = o.key } }],
  ]
}

const columns: TableColumn<Row>[] = [
  { accessorKey: 'name', header: 'Objet / taille' },
  { id: 'modified', header: 'Modifié', meta: { class: { th: 'w-28 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'visibility', header: 'Visibilité', meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]

// Téléversement : les fichiers choisis vont dans le dossier courant, en flux par le daemon.
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
  <ResourceTable :title="`Objets de ${bucket}`" :data="rows" :columns="columns" search-key="name" empty="Ce dossier est vide.">
    <template #name-cell="{ row }">
      <button v-if="row.original.folder" type="button" class="text-left" @click="prefix += row.original.name">
        <NameCell icon="i-lucide-folder" :name="row.original.name" :sub="row.original.count ? `${row.original.count} objet(s)` : 'Dossier'" />
      </button>
      <NameCell v-else icon="i-lucide-file" :name="row.original.name" :sub="size(row.original.obj!.size)" />
    </template>
    <template #modified-cell="{ row }">{{ row.original.folder ? '—' : ago(row.original.obj!.modified) }}</template>
    <template #visibility-cell="{ row }">
      <template v-if="!row.original.folder">
        <UBadge
          v-if="objectPublic(bucket, row.original.obj!)" :label="b?.public ? 'Public (bucket)' : 'Public'" color="warning" variant="soft"
        />
        <UBadge v-else label="Privé" color="neutral" variant="soft" />
      </template>
    </template>
    <template #actions-cell="{ row }">
      <UDropdownMenu v-if="!row.original.folder" :items="menu(row.original)" :content="{ align: 'end' }">
        <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
      </UDropdownMenu>
    </template>
  </ResourceTable>

  <div class="flex flex-col gap-6">
    <UCard :ui="{ body: 'p-5 sm:p-5' }">
      <UFormField label="Bucket">
        <USelect v-model="bucket" :items="state.s3.buckets.map((x) => x.name)" size="lg" class="w-full" />
      </UFormField>
      <nav class="mt-4 flex flex-wrap items-center gap-1 font-mono text-[11px]" aria-label="Dossier">
        <template v-for="(cr, i) in crumbs" :key="cr.to">
          <UIcon v-if="i" name="i-lucide-chevron-right" class="size-3 text-dimmed" />
          <button type="button" class="rounded px-1 hover:bg-elevated" :class="cr.to === prefix ? 'text-highlighted' : 'text-muted'" @click="prefix = cr.to">{{ cr.label }}</button>
        </template>
      </nav>
    </UCard>
    <SideForm
      title="Téléverser"
      :description="`Vers ${bucket}/${prefix}`"
      hint="Le fichier passe en flux par le daemon, 5 Gio au plus. Privé, il se partage par un lien temporaire pré-signé."
      @submit="submitUpload"
    >
      <input ref="input" type="file" multiple class="hidden" @change="pick">
      <div class="flex items-center gap-3">
        <UButton color="neutral" variant="outline" icon="i-lucide-paperclip" label="Choisir des fichiers" @click="input?.click()" />
        <span class="truncate text-[11px] text-muted">{{ files.length ? files.map((f) => f.name).join(', ') : 'Aucun fichier choisi' }}</span>
      </div>
      <USwitch v-model="upload.public" label="Public (public-read)" description="Lisible par tous à son URL publique." />
      <UButton type="submit" icon="i-lucide-upload" :label="files.length ? `Téléverser ${files.length} fichier(s)` : 'Choisir et téléverser'" size="lg" block :loading="upload.busy" />
    </SideForm>
  </div>

  <UModal v-model:open="link.open" :title="linkFor?.public ? 'URL publique' : 'Lien temporaire'" :description="link.key" :ui="{ content: 'sm:max-w-[480px]', description: 'font-mono text-[11px] truncate' }">
    <template #body>
      <div v-if="linkFor" class="flex flex-col gap-4">
        <UFormField v-if="!linkFor.public" label="Valable">
          <USelect v-model="link.expires" :items="expiries" class="w-full" />
        </UFormField>
        <pre class="max-h-32 overflow-auto rounded-lg bg-sage-950 p-3 font-mono text-[11px] break-all whitespace-pre-wrap text-sage-200">{{ linkFor.url }}</pre>
        <p class="text-[11px] text-muted">
          <template v-if="linkFor.public">Objet public : cette URL ne change pas et n’expire pas.</template>
          <template v-else>Lien pré-signé (SigV4){{ linkFor.expiresAt ? ` jusqu’au ${new Date(linkFor.expiresAt).toLocaleString('fr-FR')}` : '' }} ; la passerelle le refuse ensuite.</template>
        </p>
        <UButton icon="i-lucide-copy" label="Copier le lien" block @click="copyLink" />
      </div>
    </template>
  </UModal>
  <ConfirmModal
    :open="!!drop"
    eyebrow="Action destructive"
    :title="`Supprimer ${drop?.split('/').pop()} ?`"
    :description="`L’objet ${drop} sera supprimé de ${bucket}. Cette action est irréversible.`"
    confirm="Supprimer l’objet"
    @update:open="(v: boolean) => { if (!v) drop = null }"
    @confirm="actions.deleteObjects(bucket, [drop!], prefix)"
  />
</template>
