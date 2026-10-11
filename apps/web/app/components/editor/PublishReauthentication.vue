<script setup lang="ts">
import TextField from '@/components/app/TextField.vue';
import { Button } from '@/components/ui/button';
import type { PublishControllerState } from '../../editor/publishController';
import type { PublishCopy } from '../../i18n/publish';

defineProps<{
  readonly state: PublishControllerState;
  readonly copy: PublishCopy;
  readonly busy: boolean;
  readonly passwordReauth: boolean;
  readonly password: string;
  readonly providerLinkActivated: boolean;
}>();
const emit = defineEmits<{
  'update:password': [value: string];
  'update:providerLinkActivated': [value: boolean];
  'submit-password': [];
  'start-provider': [];
  'retry-provider': [];
}>();
</script>

<template>
  <div
    v-if="passwordReauth"
    class="grid gap-3"
  >
    <TextField
      :model-value="password"
      :label="copy.currentPassword"
      type="password"
      autocomplete="current-password"
      :disabled="busy"
      :control-attrs="{ 'data-action': 'publish-password' }"
      @update:model-value="(value) => emit('update:password', value)"
    />
    <Button
      type="button"
      data-action="publish-password-reauth"
      :disabled="busy || password === ''"
      @click="emit('submit-password')"
    >
      {{ copy.passwordAction }}
    </Button>
  </div>

  <div
    v-else-if="
      (state.kind === 'reauth-required'
        || state.kind === 'reauth-rate-limited')
        && state.method === 'provider'
    "
    class="grid gap-3"
  >
    <p>{{ copy.providerContinue }}</p>
    <Button
      type="button"
      data-action="publish-provider-start"
      :disabled="busy"
      @click="emit('start-provider')"
    >
      {{ copy.providerStart }}
    </Button>
  </div>

  <div
    v-else-if="state.kind === 'provider-started'"
    class="grid gap-3"
  >
    <a
      :href="state.authorizeUrl"
      class="text-link underline underline-offset-4"
      data-action="publish-provider-link"
      target="_blank"
      rel="noopener noreferrer"
      @click="emit('update:providerLinkActivated', true)"
    >
      {{ copy.providerLink }}
    </a>
    <p v-if="providerLinkActivated">
      {{ copy.providerReturn }}
    </p>
    <Button
      v-if="providerLinkActivated"
      type="button"
      data-action="publish-provider-retry"
      :disabled="busy"
      @click="emit('retry-provider')"
    >
      {{ copy.providerRetry }}
    </Button>
  </div>

  <p
    v-if="state.kind === 'reauth-wrong-password'"
    role="alert"
  >
    {{ copy.wrongPassword }}
  </p>
  <p
    v-if="
      state.kind === 'reauth-rate-limited'
        || state.kind === 'provider-started-rate-limited'
    "
    role="alert"
  >
    {{ copy.reauthRateLimited }}
  </p>
  <p
    v-if="
      state.kind === 'reauth-unavailable'
        || state.kind === 'provider-start-invalid'
    "
    role="alert"
  >
    {{ copy.reauthUnavailable }}
  </p>
</template>
