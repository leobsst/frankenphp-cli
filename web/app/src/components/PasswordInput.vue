<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

/**
 * Password field with a button that shows or hides what was typed, to check it before sending. The other attributes
 * (size, autocomplete, autofocus, placeholder, id…) go to UInput.
 */
defineOptions({ inheritAttrs: false })
const model = defineModel<string>({ default: '' })
const { t } = useI18n()
const shown = ref(false)
</script>

<template>
  <UInput v-model="model" v-bind="$attrs" :type="shown ? 'text' : 'password'" :ui="{ trailing: 'pe-1' }">
    <template #trailing>
      <UButton
        color="neutral" variant="link" size="sm" :icon="shown ? 'i-lucide-eye-off' : 'i-lucide-eye'"
        :aria-label="shown ? t('common.hidePassword') : t('common.showPassword')" :aria-pressed="shown"
        @click="shown = !shown"
      />
    </template>
  </UInput>
</template>
