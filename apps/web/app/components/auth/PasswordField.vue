<script setup lang="ts">
/**
 * `PasswordField` — a labelled password input with a show/hide toggle and an
 * optional, component-local confirmation field.
 *
 * The caller supplies `autocomplete` (`current-password` or `new-password`)
 * so password managers see the right intent. The show/hide toggle reports
 * `aria-pressed` and a labelled icon so its state is accessible. Pasting and
 * autofill are native — no `@paste.prevent`, no re-typing shims. The model is
 * bound with Vue's native `v-model`, which already defers updates across IME
 * composition, so there is no composition rewrite here.
 *
 * Confirmation is entirely local to this component: `confirmValue` is exposed
 * (for a page's submit-time check) but never submitted by any parent, and no
 * strength score or character-class hint is ever rendered.
 */
import { Eye, EyeOff } from '@lucide/vue';
import IconButton from '@/components/app/IconButton.vue';
import FormField from '@/components/app/FormField.vue';
import { Input } from '@/components/ui/input';

const props = withDefaults(
  defineProps<{
    id: string;
    label: string;
    autocomplete: 'current-password' | 'new-password';
    confirm?: boolean;
    confirmLabel?: string;
  }>(),
  {
    confirm: false,
    confirmLabel: 'Confirm password',
  },
);

const model = defineModel<string>({ default: '' });

const confirmValue = ref('');
const visible = ref(false);

const inputType = computed(() => (visible.value ? 'text' : 'password'));
const visibilityLabel = computed(
  () => `${visible.value ? 'Hide' : 'Show'} ${props.label.toLowerCase()}`,
);

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
        :error="confirmMismatch ? 'Passwords do not match.' : undefined"
        :label="confirmLabel"
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
