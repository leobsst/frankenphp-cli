<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import type { DropdownMenuItem, TableColumn } from '@nuxt/ui'
import PageHeader from '../components/PageHeader.vue'
import MiniStats from '../components/MiniStats.vue'
import PasswordInput from '../components/PasswordInput.vue'
import PageTabs from '../components/PageTabs.vue'
import TwoColumns from '../components/TwoColumns.vue'
import ResourceTable from '../components/ResourceTable.vue'
import NameCell from '../components/NameCell.vue'
import StateBadge from '../components/StateBadge.vue'
import SideForm from '../components/SideForm.vue'
import InfoBanner from '../components/InfoBanner.vue'
import ConfirmModal from '../components/ConfirmModal.vue'
import { focusField } from '../composables/focus'
import { actions, dbEngines, state, toast, type Db, type DbUser, type FieldErrors } from '../composables/useFm'
import { formatMegabytes } from '../i18n'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

// `/api/v1/databases`: MariaDB, MySQL and PostgreSQL (DB_ENGINES) databases and users, system accounts excluded.
// Passwords and queries go through the environment then the client's stdin, never on the command line.
const tab = ref('local')
const tabs = computed(() => [{ label: t('databases.tabs.databases'), value: 'local' }, { label: t('databases.tabs.users'), value: 'users' }, { label: t('databases.tabs.connections'), value: 'conn' }])
// Nothing chosen or filled in in advance (examples as placeholders); the user of a new database is not created by default.
const form = reactive({ engine: undefined as Db['engine'] | undefined, name: '', user: '', password: '', grant: false })
const userForm = reactive({ engine: undefined as Db['engine'] | undefined, name: '', password: '', database: '' })
const errors = ref<FieldErrors>({})
const busy = ref(false)
const drop = ref<Db | null>(null)
const dropUser = ref<DbUser | null>(null)

// Container and port of each engine (the daemon's defaults when unset).
const CONTAINERS: Record<Db['engine'], string> = { MariaDB: 'franken_mariadb', MySQL: 'franken_mysql', PostgreSQL: 'franken_postgresql' }
const port = (e: Db['engine']) => ({ MariaDB: state.config.DB_PORT || '3306', MySQL: state.config.MYSQL_PORT || '3307', PostgreSQL: state.config.POSTGRES_PORT || '5432' })[e]
const up = (e?: Db['engine']) => !!e && state.containers.find((c) => c.id === CONTAINERS[e])?.status === 'running'
const maria = computed(() => state.databases.filter((d) => d.engine === 'MariaDB').length)
const engines = computed(() => dbEngines.value.filter(up).length)
const stats = computed(() => [
  { label: t('databases.tabs.databases'), value: state.databases.length, sub: `${maria.value} MariaDB · ${state.databases.length - maria.value} PostgreSQL` },
  { label: t('databases.tabs.users'), value: state.dbUsers.length, sub: t('databases.stats.usersSub') },
  { label: t('databases.stats.engines'), value: engines.value, sub: engines.value === dbEngines.value.length ? t('databases.stats.allUp') : t('databases.stats.oneDown') },
])
const dbItems = computed(() => [{ label: t('databases.userForm.noDatabase'), value: '' }, ...state.databases.filter((d) => d.engine === userForm.engine).map((d) => ({ label: d.name, value: d.name }))])

async function submit() {
  busy.value = true
  const err = tab.value === 'users'
    ? await actions.createDbUser({ ...userForm, engine: userForm.engine as Db['engine'] })
    : await actions.createDb({ ...form, engine: form.engine as Db['engine'] })
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
  const url = `${d.engine === 'PostgreSQL' ? 'pgsql' : 'mysql'}://${user}@127.0.0.1:${port(d.engine)}/${d.name}`
  return [
    [
      ...(adminUrl.value ? [{ label: t('databases.menu.openAdmin', { app: d.engine === 'MariaDB' ? 'phpMyAdmin' : 'pgAdmin' }), icon: 'i-lucide-external-link', onSelect: () => openAdmin(d.engine) }] : []),
      { label: t('databases.menu.copyUrl'), icon: 'i-lucide-copy', onSelect: () => { navigator.clipboard?.writeText(url); toast('info', t('databases.copied'), url) } },
    ],
    [{ label: t('databases.dropDb.confirm'), icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { drop.value = d } }],
  ]
}

const columns = computed<TableColumn<Db>[]>(() => [
  { accessorKey: 'name', header: t('databases.columns.database') },
  { id: 'size', header: t('databases.columns.size'), meta: { class: { th: 'w-36 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'state', header: t('common.status'), meta: { class: { th: 'w-28' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])
const userColumns = computed<TableColumn<DbUser>[]>(() => [
  { accessorKey: 'name', header: t('databases.columns.user') },
  { id: 'db', header: t('databases.columns.permissions'), meta: { class: { th: 'w-40 max-md:hidden', td: 'max-md:hidden' } } },
  { id: 'actions', header: '', meta: { class: { th: 'w-10', td: 'pr-0' } } },
])
</script>

<template>
  <PageHeader :title="t('nav.databases')" :subtitle="t('databases.subtitle')">
    <UButton
      icon="i-lucide-plus" :label="tab === 'users' ? t('databases.userForm.title') : t('databases.dbForm.title')" size="lg" class="md:w-[205px]"
      @click="focusField(tab === 'users' ? 'db-user' : 'db-name')"
    />
  </PageHeader>
  <MiniStats :items="stats" />
  <PageTabs v-model="tab" :items="tabs" @update:model-value="errors = {}" />

  <TwoColumns>
    <ResourceTable v-if="tab === 'local'" :loading="!state.pageData.databases" storage-key="databases" :title="t('nav.databases')" :data="state.databases" :columns="columns" search-key="name">
      <template #name-cell="{ row }"><NameCell icon="i-lucide-database" :name="row.original.name" :sub="`${row.original.engine} · :${port(row.original.engine)}`" /></template>
      <template #size-cell="{ row }">{{ row.original.size ? formatMegabytes(row.original.size) : t('common.none') }}</template>
      <template #state-cell="{ row }"><StateBadge :value="up(row.original.engine) ? 'online' : 'offline'" /></template>
      <template #actions-cell="{ row }">
        <UDropdownMenu :items="menuFor(row.original)" :content="{ align: 'end' }">
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <ResourceTable v-else-if="tab === 'users'" :loading="!state.pageData.databases" storage-key="db-users" :title="t('databases.tabs.users')" :data="state.dbUsers" :columns="userColumns" search-key="name" :empty="t('databases.empty.users')">
      <template #name-cell="{ row }">
        <NameCell icon="i-lucide-user-round" :name="row.original.host ? `${row.original.name}@'${row.original.host}'` : row.original.name" :sub="row.original.engine" />
      </template>
      <template #db-cell="{ row }">
        <span :class="row.original.database ? '' : 'text-muted'">{{ row.original.database ? t('databases.allOn', { database: row.original.database }) : t('databases.noDatabase') }}</span>
      </template>
      <template #actions-cell="{ row }">
        <UDropdownMenu
          :content="{ align: 'end' }"
          :items="[[{ label: t('databases.dropUser.confirm'), icon: 'i-lucide-trash-2', color: 'error', onSelect: () => { dropUser = row.original } }]]"
        >
          <UButton color="neutral" variant="ghost" icon="i-lucide-ellipsis" :aria-label="t('common.actions')" />
        </UDropdownMenu>
      </template>
    </ResourceTable>
    <UCard v-else :ui="{ body: 'p-5 sm:p-5 flex flex-col gap-3 text-xs text-muted' }">
      <h2 class="text-[15px] font-semibold text-highlighted">{{ t('databases.tabs.connections') }}</h2>
      <p>MariaDB · <span class="font-mono">DB_HOST=franken_mariadb DB_PORT=3306</span></p>
      <p>PostgreSQL · <span class="font-mono">DB_HOST=franken_postgresql DB_PORT=5432</span></p>
      <p>{{ t('databases.conn.host') }}<template v-if="adminUrl"> · {{ t('databases.conn.admin', { url: adminUrl }) }}</template></p>
    </UCard>

    <SideForm
      v-if="tab === 'users'"
      :title="t('databases.userForm.title')"
      :description="t('databases.userForm.description')"
      :hint="t('databases.userForm.hint')"
      @submit="submit"
    >
      <UFormField :label="t('databases.form.engine')">
        <USelect v-model="userForm.engine" :items="dbEngines" :placeholder="t('databases.form.chooseEngine')" size="lg" class="w-full" @update:model-value="userForm.database = ''" />
      </UFormField>
      <UFormField :label="t('common.username')" :error="errors.user">
        <UInput id="db-user" v-model="userForm.name" placeholder="analytics" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField :label="t('common.password')" :error="errors.password">
        <PasswordInput v-model="userForm.password" autocomplete="new-password" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <UFormField :label="t('databases.userForm.database')">
        <USelect v-model="userForm.database" :items="dbItems" size="lg" class="w-full" />
      </UFormField>
      <UButton type="submit" :label="t('databases.userForm.submit')" size="lg" block :loading="busy" :disabled="!userForm.engine || !userForm.name || !up(userForm.engine)" />
    </SideForm>
    <SideForm
      v-else
      :title="t('databases.dbForm.title')"
      :description="t('databases.dbForm.description')"
      :hint="t('databases.dbForm.hint')"
      @submit="submit"
    >
      <UFormField :label="t('databases.form.engine')">
        <USelect v-model="form.engine" :items="dbEngines" :placeholder="t('databases.form.chooseEngine')" size="lg" class="w-full" />
      </UFormField>
      <UFormField :label="t('databases.dbForm.name')" :error="errors.name">
        <UInput id="db-name" v-model="form.name" placeholder="blog" size="lg" class="w-full" @update:model-value="errors = {}" />
      </UFormField>
      <USwitch v-model="form.grant" :label="t('databases.dbForm.grant')" :description="t('databases.dbForm.grantHelp')" />
      <template v-if="form.grant">
        <UFormField :label="t('common.username')" :error="errors.user"><UInput v-model="form.user" placeholder="blog_app" size="lg" class="w-full" @update:model-value="errors = {}" /></UFormField>
        <UFormField :label="t('common.password')" :error="errors.password">
          <PasswordInput v-model="form.password" autocomplete="new-password" size="lg" class="w-full" @update:model-value="errors = {}" />
        </UFormField>
      </template>
      <UButton type="submit" :label="t('databases.dbForm.submit')" size="lg" block :loading="busy" :disabled="!form.engine || !form.name || !up(form.engine)" />
    </SideForm>
  </TwoColumns>

  <InfoBanner>{{ t('databases.banner.system') }}<template v-if="adminUrl"> {{ t('databases.banner.admin', { url: adminUrl }) }}</template></InfoBanner>
  <ConfirmModal
    :open="!!drop"
    :eyebrow="t('databases.destructive')"
    :title="t('databases.dropDb.title', { name: drop?.name })"
    :description="t('databases.dropDb.description', { engine: drop?.engine })"
    :type-to-confirm="t('databases.dropDb.typeToConfirm')"
    :confirm="t('databases.dropDb.confirm')"
    @update:open="(v: boolean) => { if (!v) drop = null }"
    @confirm="actions.dropDb(drop!.name, drop!.engine)"
  />
  <ConfirmModal
    :open="!!dropUser"
    :eyebrow="t('databases.destructive')"
    :title="t('databases.dropUser.title', { name: dropUser?.name })"
    :description="dropUser?.engine === 'PostgreSQL' ? t('databases.dropUser.descriptionPostgres', { engine: dropUser?.engine }) : t('databases.dropUser.description', { engine: dropUser?.engine })"
    :confirm="t('databases.dropUser.confirm')"
    @update:open="(v: boolean) => { if (!v) dropUser = null }"
    @confirm="actions.dropDbUser(dropUser!.name, dropUser!.engine)"
  />
</template>
