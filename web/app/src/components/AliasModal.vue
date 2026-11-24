<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { actions, active } from '../composables/useFm'

const props = defineProps<{ target?: string }>()
const { t } = useI18n()
const open = defineModel<boolean>('open', { default: false })

// Possible targets: PHP projects and proxy hosts.
const targets = computed(() => active.value
  .filter((d) => d.kind === 'php' || d.kind === 'proxy')
  .map((d) => ({ label: d.kind === 'proxy' ? t('aliasModal.targetProxy', { name: d.name, target: d.target }) : `${d.name} · PHP ${d.php}`, value: d.name })))
const target = ref('')
const alias = ref('')
const error = ref<string | null>(null)

watch(open, (o) => {
  if (!o) return
  target.value = props.target ?? targets.value[0]?.value ?? ''
  alias.value = ''
  error.value = null
})

async function submit() {
  error.value = await actions.addAlias(target.value, alias.value.trim().toLowerCase())
  if (!error.value) open.value = false
}
</script>

<template>
  <UModal v-model:open="open" :description="t('aliasModal.description')" :ui="{ content: 'sm:max-w-[420px]', footer: 'justify-end gap-2' }">
    <template #title>
      <span class="block text-[10px] font-semibold tracking-wide text-muted uppercase">{{ t('aliasModal.eyebrow') }}</span>
      <span class="mt-4 block text-lg">{{ t('aliasModal.title') }}</span>
    </template>
    <template #body>
      <form id="alias-form" class="flex flex-col gap-4" @submit.prevent="submit">
        <UFormField :label="t('aliasModal.target')">
          <USelect v-model="target" :items="targets" class="w-full" />
        </UFormField>
        <UFormField :label="t('aliasModal.alias')" :error="error ?? undefined">
          <UInput v-model="alias" autofocus :placeholder="`www.${target}`" class="w-full" @update:model-value="error = null" />
        </UFormField>
      </form>
    </template>
    <template #footer="{ close }">
      <UButton color="neutral" variant="outline" :label="t('common.cancel')" @click="close" />
      <UButton type="submit" form="alias-form" :label="t('aliasModal.submit')" :disabled="!alias.trim() || !!error" />
    </template>
  </UModal>
</template>
