import { computed, ref, toValue, watch, type MaybeRefOrGetter, type WatchSource } from 'vue'

/** Page sizes offered by every paginated table or list. */
export const PAGE_SIZES: readonly number[] = [10, 25, 50, 100]
const DEFAULT_SIZE = PAGE_SIZES[0]!
const STORAGE_KEY = 'fm-page-sizes'

/** Page sizes chosen per table, persisted in localStorage (keyed by table). */
function readSizes(): Record<string, number> {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? JSON.parse(raw) ?? {} : {}
  } catch {
    return {}
  }
}

function storedSize(key: string | undefined): number {
  const size = key ? readSizes()[key] : undefined
  return size !== undefined && PAGE_SIZES.includes(size) ? size : DEFAULT_SIZE
}

function storeSize(key: string, size: number) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ ...readSizes(), [key]: size }))
  } catch { /* storage unavailable */ }
}

export interface PaginationOptions {
  /** Remembers the chosen page size under this key (one per table); not remembered when omitted. */
  key?: MaybeRefOrGetter<string | undefined>
  /** Goes back to page 1 whenever this source changes (search, filters, switched data set). */
  resetOn?: WatchSource | WatchSource[]
}

/**
 * Client-side pagination of a list already held by the store.
 * The page is clamped when rows disappear (e.g. after a delete) so it never shows an empty page.
 */
export function usePagination<T>(items: MaybeRefOrGetter<T[]>, options: PaginationOptions = {}) {
  const page = ref(1)
  const pageSize = ref(storedSize(toValue(options.key)))

  const total = computed(() => toValue(items).length)
  const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
  const rows = computed(() => toValue(items).slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
  /** 1-based index of the first and last rows displayed (0 when empty). */
  const from = computed(() => (total.value ? (page.value - 1) * pageSize.value + 1 : 0))
  const to = computed(() => Math.min(total.value, page.value * pageSize.value))

  watch(pageCount, (n) => { if (page.value > n) page.value = n })
  if (options.resetOn) watch(options.resetOn, () => { page.value = 1 })
  // Another table (or a switched data set) may carry its own remembered size.
  watch(() => toValue(options.key), (k) => { pageSize.value = storedSize(k) })

  /** Changes the page size while keeping the first displayed row on screen. */
  function setPageSize(size: number) {
    const first = from.value
    pageSize.value = size
    page.value = first ? Math.ceil(first / size) : 1
    const key = toValue(options.key)
    if (key) storeSize(key, size)
  }

  return { page, pageSize, total, pageCount, rows, from, to, setPageSize }
}

export type Pagination = ReturnType<typeof usePagination>
