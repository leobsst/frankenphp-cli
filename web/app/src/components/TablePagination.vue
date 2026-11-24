<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMediaQuery } from '@vueuse/core'
import { PAGE_SIZES } from '../composables/usePagination'
import { formatNumber } from '../i18n'

/**
 * Footer of a paginated table or list: page size selector, displayed range and pager.
 * Hidden while everything fits on a page of the smallest size; the size selector stays reachable beyond that,
 * even when the chosen size shows every row on a single page.
 */
const props = defineProps<{ total: number; from: number; to: number; pageSize: number }>()
const page = defineModel<number>('page', { required: true })
const emit = defineEmits<{ 'update:pageSize': [size: number] }>()
const { t } = useI18n()

const narrow = useMediaQuery('(max-width: 639px)')
const sizes = PAGE_SIZES.map((n) => ({ label: String(n), value: n }))
const visible = computed(() => props.total > PAGE_SIZES[0]!)
const pages = computed(() => Math.ceil(props.total / props.pageSize))
</script>

<template>
  <div v-if="visible" class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2.5 pt-3 text-xs text-muted">
    <div class="flex items-center gap-2">
      <span>{{ t('pagination.perPage') }}</span>
      <USelect
        :model-value="pageSize" :items="sizes" size="xs" class="w-[68px]" :aria-label="t('pagination.perPageLabel')"
        @update:model-value="(v: number) => emit('update:pageSize', v)"
      />
      <span class="ml-1 tabular-nums">{{ t('pagination.range', { from: formatNumber(from), to: formatNumber(to), total: formatNumber(total) }) }}</span>
    </div>
    <UPagination
      v-if="pages > 1" v-model:page="page" :total="total" :items-per-page="pageSize" size="xs" show-edges
      :sibling-count="narrow ? 0 : 1"
    />
  </div>
</template>
