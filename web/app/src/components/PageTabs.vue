<script setup lang="ts">
import { computed, onBeforeUnmount, watchEffect } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import { shell, type PageTab } from '../lib/shell'

/**
 * Tabs of a page (above its content, below its stats). From the tablet up, the underlined tabs of the mockup. On a
 * phone, a segmented control: the native one when the app has one (iPhone: below the header, shell.pageTabs),
 * otherwise drawn in HTML with the same look.
 */
const props = defineProps<{ items: PageTab[] }>()
const model = defineModel<string>({ required: true })

const phone = useMediaQuery('(max-width: 719px)')
const native = computed(() => phone.value && !!shell.pageTabs)

// Native tabs: shown again on every change (labels in another language, selection), removed when leaving the page.
const owner = {}
watchEffect(() => {
  if (native.value) shell.pageTabs!.show(owner, props.items.map(({ label, value }) => ({ label, value })), model.value, (v) => { model.value = v })
  else shell.pageTabs?.hide(owner)
})
onBeforeUnmount(() => shell.pageTabs?.hide(owner))

// Underlined tabs (tablet, desktop) or HTML segmented control (phone).
const LINK = { trigger: 'text-xs px-0 me-16 pb-2.5' }
const SEGMENTED = {
  list: 'w-full overflow-x-auto p-0.5 rounded-lg bg-accented/60',
  indicator: 'rounded-md bg-default shadow-sm inset-y-0.5',
  trigger: 'flex-1 min-w-max justify-center px-3 py-1.5 text-xs font-medium data-[state=active]:text-highlighted',
}
</script>

<template>
  <UTabs
    v-if="!native"
    v-model="model" :items="items" :content="false" :variant="phone ? 'pill' : 'link'" :color="phone ? 'neutral' : 'primary'"
    class="mt-5 mb-2" :ui="phone ? SEGMENTED : LINK"
  />
  <!-- Native tabs below the header: the space they took between the stats and the content stays. -->
  <div v-else class="h-5" />
</template>
