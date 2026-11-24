<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { checkPassword, state } from '../composables/useFm'

const props = defineProps<{
  eyebrow: string
  title: string
  description: string
  confirm: string
  note?: string
  noteTone?: 'warning' | 'primary'
  typeToConfirm?: string
  hint?: string
}>()
const emit = defineEmits<{ confirm: [] }>()
const open = defineModel<boolean>('open', { default: false })

const typed = ref('')
// Over remote access, a destructive action asks for the password again (X-FM-Password header, v0.11.0).
const remote = computed(() => state.connection.mode === 'remote')
const password = ref('')
const wrong = ref(false)
watch(open, () => { typed.value = ''; password.value = ''; wrong.value = false })
const ok = computed(() => (!props.typeToConfirm || typed.value.trim().toUpperCase() === props.typeToConfirm) && (!remote.value || !!password.value))

function submit() {
  if (!ok.value) return
  if (remote.value && !checkPassword(password.value)) { wrong.value = true; return }
  emit('confirm')
  open.value = false
}
</script>

<template>
  <UModal v-model:open="open" :description="description" :ui="{ content: 'sm:max-w-[420px]', footer: 'justify-end gap-2' }">
    <template #title>
      <span class="block text-[10px] font-semibold tracking-wide text-muted uppercase">{{ eyebrow }}</span>
      <span class="mt-4 block text-lg">{{ title }}</span>
    </template>
    <template v-if="typeToConfirm || note || hint || remote" #body>
      <form class="flex flex-col gap-3" @submit.prevent="submit">
        <UFormField v-if="typeToConfirm" :label="`Saisissez ${typeToConfirm} pour confirmer`">
          <UInput v-model="typed" autofocus :placeholder="typeToConfirm" class="w-full" />
        </UFormField>
        <UFormField
          v-if="remote" label="Mot de passe du dashboard" :error="wrong ? 'Mot de passe erroné.' : undefined"
          help="Demandé pour chaque action destructive depuis l’accès distant." :ui="{ help: 'text-[11px]' }"
        >
          <UInput v-model="password" type="password" :autofocus="!typeToConfirm" class="w-full" @update:model-value="wrong = false" />
        </UFormField>
        <UAlert v-if="note" :color="noteTone ?? 'warning'" variant="soft" :description="note" :ui="{ description: 'text-xs' }" />
        <p v-if="hint" class="text-xs text-muted">{{ hint }}</p>
      </form>
    </template>
    <template #footer="{ close }">
      <UButton color="neutral" variant="outline" label="Annuler" @click="close" />
      <UButton color="error" :label="confirm" :disabled="!ok" @click="submit" />
    </template>
  </UModal>
</template>
