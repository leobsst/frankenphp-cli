<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { actions, active } from '../composables/useFm'

const props = defineProps<{ target?: string }>()
const open = defineModel<boolean>('open', { default: false })

// Possible targets: PHP projects and proxy hosts.
const targets = computed(() => active.value
  .filter((d) => d.kind === 'php' || d.kind === 'proxy')
  .map((d) => ({ label: d.kind === 'proxy' ? `${d.name} · proxy vers ${d.target}` : `${d.name} · PHP ${d.php}`, value: d.name })))
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
  <UModal v-model:open="open" description="Un alias sert le même site que sa cible : sa version PHP, ou l’upstream d’un proxy host." :ui="{ content: 'sm:max-w-[420px]', footer: 'justify-end gap-2' }">
    <template #title>
      <span class="block text-[10px] font-semibold tracking-wide text-muted uppercase">Formulaire</span>
      <span class="mt-4 block text-lg">Ajouter un alias</span>
    </template>
    <template #body>
      <form id="alias-form" class="flex flex-col gap-4" @submit.prevent="submit">
        <UFormField label="Domaine cible">
          <USelect v-model="target" :items="targets" class="w-full" />
        </UFormField>
        <UFormField label="Nouvel alias" :error="error ?? undefined">
          <UInput v-model="alias" autofocus :placeholder="`www.${target}`" class="w-full" @update:model-value="error = null" />
        </UFormField>
      </form>
    </template>
    <template #footer="{ close }">
      <UButton color="neutral" variant="outline" label="Annuler" @click="close" />
      <UButton type="submit" form="alias-form" label="Créer l’alias" :disabled="!alias.trim() || !!error" />
    </template>
  </UModal>
</template>
