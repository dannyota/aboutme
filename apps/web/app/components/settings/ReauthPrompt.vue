<script setup lang="ts">
import PasswordField from '../auth/PasswordField.vue';
import { Button } from '../ui/button';
import type { AuthProvider } from '../../composables/useAuth';
import type { Locale } from '../../i18n/locale';

withDefaults(defineProps<{
  mode: 'password' | 'provider';
  providers: readonly AuthProvider[];
  providerLabel: (provider: AuthProvider) => string;
  pending: boolean;
  locale: Locale;
  passwordId: string;
  labels: {
    readonly currentPassword: string;
    readonly continue: string;
    readonly checking: string;
    readonly cancel: string;
  };
  passwordDescription?: string;
  providerDescription?: string;
  lockCancel?: boolean;
  formTestid?: string;
  providerTestid?: string;
  submitTestid?: string;
  cancelTestid?: string;
  providerButtonTestid?: string;
}>(), {
  passwordDescription: undefined,
  providerDescription: undefined,
  lockCancel: false,
  formTestid: undefined,
  providerTestid: undefined,
  submitTestid: undefined,
  cancelTestid: undefined,
  providerButtonTestid: undefined,
});
const emit = defineEmits<{
  submitPassword: [];
  provider: [provider: AuthProvider];
  cancel: [];
}>();
const currentPassword = defineModel<string>({ required: true });
</script>

<template>
  <form
    v-if="mode === 'password'"
    :data-testid="formTestid"
    class="grid gap-4"
    novalidate
    @submit.prevent="emit('submitPassword')"
  >
    <p v-if="passwordDescription !== undefined">
      {{ passwordDescription }}
    </p>
    <slot />
    <PasswordField
      :id="passwordId"
      v-model="currentPassword"
      autocomplete="current-password"
      :label="labels.currentPassword"
      :locale="locale"
    />
    <div class="flex gap-2">
      <Button
        :data-testid="submitTestid"
        :disabled="pending"
        type="submit"
        variant="secondary"
      >
        {{ pending ? labels.checking : labels.continue }}
      </Button>
      <Button
        :data-testid="cancelTestid"
        :disabled="lockCancel && pending"
        type="button"
        variant="ghost"
        @click="emit('cancel')"
      >
        {{ labels.cancel }}
      </Button>
    </div>
  </form>
  <div
    v-else
    class="grid gap-3"
    :data-testid="providerTestid"
  >
    <p v-if="providerDescription !== undefined">
      {{ providerDescription }}
    </p>
    <slot />
    <div class="flex flex-wrap gap-2">
      <Button
        v-for="provider in providers"
        :key="provider"
        :data-testid="providerButtonTestid === undefined
          ? undefined
          : `${providerButtonTestid}${provider}`"
        :disabled="pending"
        type="button"
        @click="emit('provider', provider)"
      >
        {{ providerLabel(provider) }}
      </Button>
      <Button
        :data-testid="cancelTestid"
        :disabled="lockCancel && pending"
        type="button"
        variant="ghost"
        @click="emit('cancel')"
      >
        {{ labels.cancel }}
      </Button>
    </div>
  </div>
</template>
