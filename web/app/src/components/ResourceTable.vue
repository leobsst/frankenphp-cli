<script setup lang="ts" generic="T extends Record<string, any>">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TableColumn } from '@nuxt/ui'
import TablePagination from './TablePagination.vue'
import { usePagination } from '../composables/usePagination'

/**
 * "List" card of the resource pages: title, collapsible search, UTable and pagination footer.
 * The `<column>-cell` slots are forwarded to UTable.
 * The page size is remembered under `storageKey` (the title by default: pass a key when the title is dynamic);
 * the page goes back to 1 when the search or the title (i.e. the displayed data set) changes.
 * `paginate: false` shows every row (e.g. a folder explorer, where pages would split a directory).
 * `loading`: the data has not been read yet: skeleton rows instead of the "nothing here" message.
 */
const props = withDefaults(defineProps<{
  title: string; data: T[]; columns: TableColumn<T>[]; searchKey?: keyof T; empty?: string; storageKey?: string; paginate?: boolean
  loading?: boolean
}>(), { paginate: true })
const { t } = useI18n()

const q = ref('')
const searching = ref(false)
const filtered = computed(() => {
  const key = props.searchKey
  const s = q.value.trim().toLowerCase()
  return key && s ? props.data.filter((r) => String(r[key]).toLowerCase().includes(s)) : props.data
})
const pager = usePagination(filtered, { key: () => props.storageKey ?? props.title, resetOn: [q, () => props.title] })
const { page, pageSize, total, from, to } = pager
const rows = computed(() => (props.paginate ? pager.rows.value : filtered.value))
</script>

<template>
  <UCard :ui="{ header: 'flex items-center justify-between gap-3 px-5 py-3.5 sm:px-5 border-none', body: 'p-0 sm:p-0 px-5 pb-3 sm:px-5 sm:pb-3' }">
    <template #header>
      <h2 class="text-[15px] font-semibold text-highlighted">{{ title }}</h2>
      <!-- Button and field of the same height (h-8): the header does not move when one replaces the other. -->
      <UInput
        v-if="searching" v-model="q" autofocus size="sm" icon="i-lucide-search" :placeholder="t('resourceTable.filter')" class="w-48" :ui="{ base: 'h-8' }"
        @blur="!q && (searching = false)"
      />
      <UButton v-else-if="searchKey" color="neutral" variant="ghost" icon="i-lucide-search" class="size-8 justify-center" :aria-label="t('common.search')" @click="searching = true" />
    </template>
    <!-- Loading: rows shaped like the real ones (an icon, a name and its detail, a badge). -->
    <div v-if="loading" class="flex flex-col divide-y divide-default" :aria-busy="true" :aria-label="t('common.loading')">
      <div v-for="i in 4" :key="i" class="flex items-center gap-3 py-3">
        <USkeleton class="size-5 shrink-0 rounded-md" />
        <div class="flex min-w-0 flex-1 flex-col gap-1.5">
          <USkeleton class="h-3 w-2/5" />
          <USkeleton class="h-2.5 w-1/4" />
        </div>
        <USkeleton class="h-5 w-16 rounded-md" />
      </div>
    </div>
    <UTable
      v-else
      :data="rows"
      :columns="columns"
      :empty="empty ?? t('resourceTable.empty')"
      :ui="{
        th: 'px-0 pr-4 py-2 text-[10px] font-semibold uppercase text-muted',
        td: 'px-0 pr-4 py-3 text-xs text-muted',
        separator: 'bg-(--ui-border)',
      }"
    >
      <template v-for="(_, name) in $slots" #[name]="slotData">
        <slot :name="name" v-bind="slotData" />
      </template>
    </UTable>
    <TablePagination
      v-if="paginate && !loading" v-model:page="page" :total="total" :from="from" :to="to" :page-size="pageSize"
      class="border-t border-default" @update:page-size="pager.setPageSize"
    />
  </UCard>
</template>
