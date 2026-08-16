<script setup lang="ts">
/**
 * `PasswordField` — a labelled password input with a show/hide toggle and an
 * optional, component-local confirmation field.
 *
 * The caller supplies `autocomplete` (`current-password` or `new-password`)
 * so password managers see the right intent. The show/hide toggle reports
 * `aria-pressed` and a labelled text so its state is accessible. Pasting and
 * autofill are native — no `@paste.prevent`, no re-typing shims. The model is
 * bound with Vue's native `v-model`, which already defers updates across IME
 * composition, so there is no composition rewrite here.
 *
 * Confirmation is entirely local to this component: `confirmValue` is exposed
 * (for a page's submit-time check) but never submitted by any parent, and no
 * strength score or character-class hint is ever rendered.
 */
const props = withDefaults(defineProps<{
  id: string;
  label: string;
  autocomplete: 'current-password' | 'new-password';
  confirm?: boolean;
  confirmLabel?: string;
}>(), {
  confirm: false,
  confirmLabel: 'Confirm password',
});

const model = defineModel<string>({ default: '' });

const confirmValue = ref('');
const visible = ref(false);

const inputType = computed(() => (visible.value ? 'text' : 'password'));

const confirmMismatch = computed(
  () => props.confirm
    && confirmValue.value !== '' && confirmValue.value !== model.value,
);

defineExpose({ confirmValue, confirmMismatch });
</script>

<template>
  <div class="auth-field">
    <label :for="id">{{ label }}</label>
    <div class="auth-field__input">
      <input
        :id="id"
        v-model="model"
        :type="inputType"
        :autocomplete="autocomplete"
      >
      <button
        type="button"
        class="auth-password-toggle"
        :aria-pressed="visible"
        :aria-label="visible ? `Hide ${label}` : `Show ${label}`"
      >
        {{ visible ? 'Hide' : 'Show' }}
      </button>
    </div>

    <template v-if="confirm">
      <label :for="`${id}-confirm`">{{ confirmLabel }}</label>
      <div class="auth-field__input auth-field__input--confirm">
        <input
          :id="`${id}-confirm`"
          v-model="confirmValue"
          :type="inputType"
          autocomplete="new-password"
        >
      </div>
      <p
        v-if="confirmMismatch"
        class="auth-field-error"
      >
        Passwords do not match.
      </p>
    </template>
  </div>
</template>
