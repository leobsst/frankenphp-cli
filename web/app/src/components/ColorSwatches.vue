<script setup lang="ts">
import { computed } from 'vue'

// Preset color swatches, followed by a "custom" swatch that opens a color picker.
const props = defineProps<{ presets: string[]; label: string }>()
const model = defineModel<string>({ required: true })

const selected = computed(() => model.value.toUpperCase())
const custom = computed(() => !props.presets.includes(selected.value))
</script>

<template>
  <div class="flex flex-wrap gap-3">
    <button
      v-for="c in presets" :key="c" type="button" :aria-label="c"
      class="grid size-8 place-items-center rounded-lg text-white ring-offset-2 ring-offset-(--ui-bg)"
      :class="{ 'ring-2': selected === c }"
      :style="{ background: c, '--tw-ring-color': c }"
      @click="model = c"
    >
      <UIcon v-if="selected === c" name="i-lucide-check" class="size-[18px]" />
    </button>

    <UPopover :content="{ side: 'bottom', align: 'start' }">
      <UTooltip :text="`${label} personnalisée`">
        <button
          type="button" :aria-label="`${label} personnalisée`"
          class="grid size-8 place-items-center rounded-lg text-white ring-offset-2 ring-offset-(--ui-bg)"
          :class="{ 'ring-2': custom }"
          :style="custom
            ? { background: model, '--tw-ring-color': model }
            : { background: 'conic-gradient(from 90deg, #ef4444, #f59e0b, #22c55e, #06b6d4, #3b82f6, #a855f7, #ef4444)' }"
        >
          <UIcon :name="custom ? 'i-lucide-check' : 'i-lucide-pipette'" class="size-[18px] drop-shadow-sm" />
        </button>
      </UTooltip>
      <template #content>
        <UColorPicker v-model="model" class="p-3" />
      </template>
    </UPopover>
  </div>
</template>
