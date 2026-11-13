<script setup lang="ts">
import { Eye, EyeOff } from '@lucide/vue';
import IconButton from '@/components/app/IconButton.vue';
import FormField from '@/components/app/FormField.vue';
import { Input } from '@/components/ui/input';
import { authCopy } from '@/i18n/auth';
import type { Locale } from '@/i18n/locale';

const props = withDefaults(
  defineProps<{
    id: string;
    label: string;
    autocomplete: 'current-password' | 'new-password';
    confirm?: boolean;
    confirmLabel?: string;
    locale?: Locale;
  }>(),
  {
    confirm: false,
    confirmLabel: undefined,
    locale: 'en',
  },
);
const copy = computed(() => authCopy[props.locale]);

const model = defineModel<string>({ default: '' });

const confirmValue = ref('');
const visible = ref(false);

const inputType = computed(() => (visible.value ? 'text' : 'password'));
const visibilityLabel = computed(() => visible.value
  ? copy.value.hidePassword(props.label)
  : copy.value.showPassword(props.label));

const confirmMismatch = computed(
  () =>
    props.confirm
    && confirmValue.value !== ''
    && confirmValue.value !== model.value,
);

defineExpose({ confirmValue, confirmMismatch });

function toggleVisibility(): void {
  visible.value = !visible.value;
}
</script>

<template>
  <FormField
    :id="id"
    v-slot="{ id: fieldId, describedBy, invalid }"
    :label="label"
  >
    <div class="relative">
      <Input
        :id="fieldId"
        v-model="model"
        :aria-describedby="describedBy"
        :aria-invalid="invalid"
        :autocomplete="autocomplete"
        class="pr-10"
        :type="inputType"
      />
      <IconButton
        class="absolute right-0 top-1/2 -translate-y-1/2"
        :label="visibilityLabel"
        :pressed="visible"
        size="icon"
        variant="ghost"
        @click="toggleVisibility"
      >
        <EyeOff
          v-if="visible"
          aria-hidden="true"
        />
        <Eye
          v-else
          aria-hidden="true"
        />
      </IconButton>
    </div>

    <template v-if="confirm">
      <FormField
        :id="`${id}-confirm`"
        v-slot="{
          id: confirmId,
          describedBy: confirmDescribedBy,
          invalid: confirmInvalid,
        }"
        :error="
          confirmMismatch ? copy.messages.passwordsDoNotMatch : undefined
        "
        :label="confirmLabel ?? copy.confirmPassword"
      >
        <Input
          :id="confirmId"
          v-model="confirmValue"
          :aria-describedby="confirmDescribedBy"
          :aria-invalid="confirmInvalid"
          autocomplete="new-password"
          :type="inputType"
        />
      </FormField>
    </template>
  </FormField>
</template>
