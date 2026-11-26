<script setup lang="ts">
import PasswordInput from './PasswordInput.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
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
const { t } = useI18n()

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
        <UFormField v-if="typeToConfirm" :label="t('confirmModal.typeToConfirm', { value: typeToConfirm })">
          <UInput v-model="typed" autofocus :placeholder="typeToConfirm" class="w-full" />
        </UFormField>
        <UFormField
          v-if="remote" :label="t('confirmModal.password')" :error="wrong ? t('confirmModal.wrongPassword') : undefined"
          :help="t('confirmModal.passwordHelp')" :ui="{ help: 'text-[11px]' }"
        >
          <PasswordInput v-model="password" :autofocus="!typeToConfirm" class="w-full" @update:model-value="wrong = false" />
        </UFormField>
        <UAlert v-if="note" :color="noteTone ?? 'warning'" variant="soft" :description="note" :ui="{ description: 'text-xs' }" />
        <p v-if="hint" class="text-xs text-muted">{{ hint }}</p>
      </form>
    </template>
    <template #footer="{ close }">
      <UButton color="neutral" variant="outline" :label="t('common.cancel')" @click="close" />
      <UButton color="error" :label="confirm" :disabled="!ok" @click="submit" />
    </template>
  </UModal>
</template>
