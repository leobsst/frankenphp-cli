<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import type { DropdownMenuItem, TableColumn } from '@nuxt/ui'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import TwoColumns from '../components/TwoColumns.vue'
import ResourceTable from '../components/ResourceTable.vue'
import NameCell from '../components/NameCell.vue'
import StateBadge from '../components/StateBadge.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
import ConfirmModal from '../components/ConfirmModal.vue'
import { focusField } from '../composables/focus'
import { actions, state, toast, type Db, type DbUser, type FieldErrors } from '../composables/useFm'

// `/api/v1/databases`: MariaDB, MySQL and PostgreSQL (DB_ENGINES) databases and users, system accounts excluded.
// Passwords and queries go through the environment then the client's stdin, never on the command line.
const tab = ref('local')
const tabs = [{ label: 'Bases', value: 'local' }, { label: 'Utilisateurs', value: 'users' }, { label: 'Connexions', value: 'conn' }]
const form = reactive({ engine: 'PostgreSQL' as Db['engine'], name: 'blog', user: 'blog_app', password: 'S3cure-local-pass', grant: true })
const userForm = reactive({ engine: 'MariaDB' as Db['engine'], name: 'analytics', password: 'An4lytics-pass', database: 'shop' })
const errors = ref<FieldErrors>({})
const busy = ref(false)
const drop = ref<Db | null>(null)
const dropUser = ref<DbUser | null>(null)

const port = (e: Db['engine']) => (e === 'MariaDB' ? 3306 : 5432)
const up = (e: Db['engine']) => state.containers.find((c) => c.kind === (e === 'MariaDB' ? 'mariadb' : 'postgres'))?.status === 'running'
const maria = computed(() => state.databases.filter((d) => d.engine === 'MariaDB').length)
const engines = computed(() => (['MariaDB', 'PostgreSQL'] as const).filter(up).length)
const stats = computed(() => [
  { label: 'Bases', value: state.databases.length, sub: `${maria.value} MariaDB · ${state.databases.length - maria.value} PostgreSQL` },
  { label: 'Utilisateurs', value: state.dbUsers.length, sub: 'Comptes système exclus' },
  { label: 'Moteurs', value: engines.value, sub: engines.value === 2 ? 'Tous opérationnels' : 'Un moteur est arrêté' },
])
const dbItems = computed(() => [{ label: 'Aucune (droits plus tard)', value: '' }, ...state.databases.filter((d) => d.engine === userForm.engine).map((d) => ({ label: d.name, value: d.name }))])

async function submit() {
  busy.value = true
  const err = tab.value === 'users' ? await actions.createDbUser({ ...userForm }) : await actions.createDb({ ...form })
  busy.value = false
  errors.value = err ?? {}
  if (!err && tab.value === 'users') Object.assign(userForm, { name: '', password: '' })
  else if (!err) Object.assign(form, { name: '', user: '', password: '' })
}

// phpMyAdmin or pgAdmin URL provided by the daemon (dev only); empty: no admin interface to open.
const adminUrl = computed(() => state.adminUrl)
const openAdmin = (_e: Db['engine']) => { if (adminUrl.value) window.open(adminUrl.value, '_blank') }

function menuFor(d: Db): DropdownMenuItem[][] {
  const user = d.user || 'root'
  const url = `${d.engine === 'MariaDB' ? 'mysql' : 'pgsql'}://${user}@127.0.0.1:${port(d.engine)}/${d.name}`
  return [
    [
      ...(adminUrl.value ? [{ label: d.engine === 'MariaDB' ? 'Ouvrir phpMyAdmin' : 'Ouvrir pgAdmin', icon: 'i-lucide-external-link', onSelect: () => openAdmin(d.engine) }] : []),
      { label: 'Copier la chaîne de connexion', icon: 'i-lucide-copy', onSelect: () => { navigator.clipboard?.writeText(url); toast('info', 'Copié', url) } },
    ],
    [{ label: 'Supprimer la base', icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { drop.value = d } }],
  ]
}

const columns: TableColumn<Db>[] = [
  { accessorKey: 'name', header: 'Base / moteur' },
  { id: 'size', header: 'Taille', meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: 'État', meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]
const userColumns: TableColumn<DbUser>[] = [
  { accessorKey: 'name', header: 'Utilisateur / moteur' },
  { id: 'db', header: 'Droits', meta: { class: { th: 'w-40 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
]
</script>

<template>
  <PageHeader title="Bases de données" subtitle="Gérez les bases et les utilisateurs de MariaDB et PostgreSQL sur la machine hôte.">
    <UButton
      icon="i-lucide-plus" :label="tab === 'users' ? 'Créer un utilisateur' : 'Créer une base'" size="lg" class="md:w-[205px]"
      @click="focusField(tab === 'users' ? 'db-user' : 'db-name')"
    />
  </PageHeader>
  <MiniStats :items="stats" />
  <UTabs v-model="tab" :items="tabs" :content="false" variant="link" class="mt-5 mb-2" :ui="{ trigger: 'text-xs px-0 me-16 pb-2.5' }" @update:model-value="errors = {}" />

  <TwoColumns>
    <ResourceTable v-if="tab === 'local'" title="Bases de données" :data="state.databases" :columns="columns" search-key="name">
      <template #name-cell="{ row }"><NameCell icon="i-lucide-database" :name="row.original.name" :sub="`${row.original.engine} · :${port(row.original.engine)}`" /></template>
      <template #size-cell="{ row }">{{ row.original.size ? `${row.original.size} Mo` : '—' }}</template>
      <template #state-cell="{ row }"><StateBadge :value="up(row.original.engine) ? 'online' : 'offline'" /></template>
      <template #actions-cell="{ row }">
        <UDropdownMenu :items="menuFor(row.original)" :content="{ align: 'end' }">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <ResourceTable v-else-if="tab === 'users'" title="Utilisateurs" :data="state.dbUsers" :columns="userColumns" search-key="name" empty="Aucun utilisateur.">
      <template #name-cell="{ row }">
        <NameCell icon="i-lucide-user-round" :name="row.original.host ? `${row.original.name}@'${row.original.host}'` : row.original.name" :sub="row.original.engine" />
      </template>
      <template #db-cell="{ row }">
        <span :class="row.original.database ? '' : 'text-muted'">{{ row.original.database ? `Tous sur ${row.original.database}` : 'Aucune base' }}</span>
      </template>
      <template #actions-cell="{ row }">
        <UDropdownMenu
          :content="{ align: 'end' }"
          :items="[[{ label: 'Supprimer l’utilisateur', icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { dropUser = row.original } }]]"
        >
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" aria-label="Actions" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <UCard v-else :ui="{ body: 'p-5 sm:p-5 flex flex-col gap-3 text-xs text-muted' }">
      <h2 class="text-[15px] font-semibold text-highlighted">Connexions</h2>
      <p>MariaDB · <span class="font-mono">DB_HOST=franken_mariadb DB_PORT=3306</span></p>
      <p>PostgreSQL · <span class="font-mono">DB_HOST=franken_postgresql DB_PORT=5432</span></p>
      <p>Depuis la machine hôte : 127.0.0.1<template v-if="adminUrl"> · administration : phpMyAdmin et pgAdmin sur {{ adminUrl }}</template></p>
    </UCard>

    <SideForm
      v-if="tab === 'users'"
      title="Créer un utilisateur"
      description="Un compte avec tous les droits sur une base."
      hint="Sur MariaDB il se connecte de partout ('%'), vos applications tournant dans d’autres conteneurs ; sur PostgreSQL il devient propriétaire de la base."
      @submit="submit"
    >
      <UFormField label="Moteur de base de données">
        <USelect v-model="userForm.engine" :items="['MariaDB', 'PostgreSQL']" size="lg" class="w-full" @update:model-value="userForm.database = ''" />
      </UFormField>
      <UFormField label="Nom d’utilisateur" :error="errors.user">
        <UInput id="db-user" v-model="userForm.name" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField label="Mot de passe" :error="errors.password">
        <UInput v-model="userForm.password" type="password" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField label="Base">
        <USelect v-model="userForm.database" :items="dbItems" size="lg" class="w-full" />
      </UFormField>
      <UButton type="submit" label="Créer l’utilisateur" size="lg" block :loading="busy" :disabled="!userForm.name || !up(userForm.engine)" />
    </SideForm>
    <SideForm
      v-else
      title="Créer une base"
      description="utf8mb4 sur MariaDB, UTF8 sur PostgreSQL."
      hint="Lettres, chiffres et _ uniquement. Les identifiants n’apparaissent ni dans les journaux ni en ligne de commande."
      @submit="submit"
    >
      <UFormField label="Moteur de base de données">
        <USelect v-model="form.engine" :items="['MariaDB', 'PostgreSQL']" size="lg" class="w-full" />
      </UFormField>
      <UFormField label="Nom de la base" :error="errors.name">
        <UInput id="db-name" v-model="form.name" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <USwitch v-model="form.grant" label="Créer aussi son utilisateur" description="Avec tous les droits sur cette base." />
      <template v-if="form.grant">
        <UFormField label="Nom d’utilisateur" :error="errors.user"><UInput v-model="form.user" size="lg" class="w-full" @update:model-value="errors = {}" /></UFormField>
        <UFormField label="Mot de passe" :error="errors.password">
          <UInput v-model="form.password" type="password" size="lg" class="w-full" @update:model-value="errors = {}" />
        </UFormField>
      </template>
      <UButton type="submit" label="Créer la base de données" size="lg" block :loading="busy" :disabled="!form.name || !up(form.engine)" />
    </SideForm>
  </TwoColumns>

  <InfoBanner>Les bases et comptes système (mysql, postgres, root…) ne sont ni listés ni supprimables.<template v-if="adminUrl"> Pour le reste, phpMyAdmin et pgAdmin restent sur {{ adminUrl }}.</template></InfoBanner>
  <ConfirmModal
    :open="!!drop"
    eyebrow="Action destructive"
    :title="`Supprimer la base ${drop?.name} ?`"
    :description="`La base ${drop?.engine} et toutes ses tables seront supprimées. Cette action est irréversible.`"
    type-to-confirm="SUPPRIMER"
    confirm="Supprimer la base"
    @update:open="(v: boolean) => { if (!v) drop = null }"
    @confirm="actions.dropDb(drop!.name, drop!.engine)"
  />
  <ConfirmModal
    :open="!!dropUser"
    eyebrow="Action destructive"
    :title="`Supprimer l’utilisateur ${dropUser?.name} ?`"
    :description="`Le compte ne pourra plus se connecter à ${dropUser?.engine}. ${dropUser?.engine === 'PostgreSQL' ? 'Ses objets passent à l’administrateur.' : 'Tous ses hôtes sont supprimés.'}`"
    confirm="Supprimer l’utilisateur"
    @update:open="(v: boolean) => { if (!v) dropUser = null }"
    @confirm="actions.dropDbUser(dropUser!.name, dropUser!.engine)"
  />
</template>
