<script setup lang="ts">
/**
 * `PasswordSettings` — add/change a password from account settings.
 *
 * Presentational: it receives the current `hasPassword` status and the
 * linked providers as props, and it performs exactly three side effects
 * through `PasswordSettingsActionsKey` (see `composables/passwordSettings`).
 * It never reads ambient auth state, and it never renders a provider email.
 *
 * Provider-only users add a password optimistically and fall back to a
 * provider reauth round trip only when the server answers `reauth_required`.
 * Password users reauthenticate with their current password before entering
 * a new one. The confirmation field is component-local (via `PasswordField`)
 * and is never submitted; `setPassword` receives exactly one password.
 */
import PasswordField from './PasswordField.vue';
import StatusBanner from '../app/StatusBanner.vue';
import { Button } from '../ui/button';
import { Card, CardContent, CardHeader } from '../ui/card';
import {
  PasswordSettingsActionsKey,
  type PasswordSettingsFailure,
} from '../../composables/passwordSettings';
import type { AuthProvider } from '../../composables/useAuth';
import type { PasswordIssue } from '../../composables/usePasswordAuth';

const props = defineProps<{
  hasPassword: boolean;
  providers: AuthProvider[];
}>();

const emit = defineEmits<{
  updated: [];
}>();

const actions = inject(PasswordSettingsActionsKey, null);

const PROVIDER_LABELS: Record<AuthProvider, string> = {
  google: 'Google',
  github: 'GitHub',
  linkedin: 'LinkedIn',
};

const ISSUE_COPY: Record<PasswordIssue, string> = {
  length: 'Password must be at least 12 characters.',
  common: 'That password is too common. Choose a different one.',
  breached:
    'That password was exposed in a data breach. Choose a different one.',
};

type Mode = 'idle' | 'set' | 'reauth-password' | 'reauth-provider';

const mode = ref<Mode>('idle');
const currentPassword = ref('');
const newPassword = ref('');
const pending = ref(false);
const errorMessage = ref<string | null>(null);
const successMessage = ref<string | null>(null);
const passwordField = ref<InstanceType<typeof PasswordField> | null>(null);

function copyFor(failure: PasswordSettingsFailure): string {
  switch (failure.kind) {
    case 'reauth-failed':
      return 'Incorrect password.';
    case 'reauth-required':
      return 'Sign in again to confirm it\'s you before continuing.';
    case 'password-invalid':
      return failure.issue
        ? ISSUE_COPY[failure.issue]
        : 'Password does not meet our requirements.';
    case 'rate-limited':
      return 'Too many attempts. Try again later.';
    case 'unavailable':
      return 'Something went wrong. Please try again.';
  }
}

function start(): void {
  errorMessage.value = null;
  successMessage.value = null;
  currentPassword.value = '';
  newPassword.value = '';
  mode.value = props.hasPassword ? 'reauth-password' : 'set';
}

function cancel(): void {
  mode.value = 'idle';
  errorMessage.value = null;
  currentPassword.value = '';
  newPassword.value = '';
  pending.value = false;
}

async function submitReauth(): Promise<void> {
  if (pending.value || !actions) return;
  if (!currentPassword.value) {
    errorMessage.value = 'Enter your current password.';
    return;
  }
  pending.value = true;
  errorMessage.value = null;
  try {
    await actions.reauthenticate(currentPassword.value);
    currentPassword.value = '';
    newPassword.value = '';
    mode.value = 'set';
  } catch (failure) {
    errorMessage.value = copyFor(failure as PasswordSettingsFailure);
  } finally {
    pending.value = false;
  }
}

async function submitSet(): Promise<void> {
  if (pending.value || !actions) return;
  if (passwordField.value?.confirmMismatch) {
    errorMessage.value = 'Passwords do not match.';
    return;
  }
  if (!newPassword.value) {
    errorMessage.value = 'Enter a new password.';
    return;
  }
  pending.value = true;
  errorMessage.value = null;
  try {
    await actions.setPassword(newPassword.value);
    currentPassword.value = '';
    newPassword.value = '';
    successMessage.value = props.hasPassword
      ? 'Password changed.'
      : 'Password added.';
    mode.value = 'idle';
    emit('updated');
  } catch (failure) {
    const kind = (failure as PasswordSettingsFailure).kind;
    if (kind === 'reauth-required') {
      currentPassword.value = '';
      newPassword.value = '';
      mode.value = props.hasPassword ? 'reauth-password' : 'reauth-provider';
    }
    errorMessage.value = copyFor(failure as PasswordSettingsFailure);
  } finally {
    pending.value = false;
  }
}

async function submitProviderReauth(provider: AuthProvider): Promise<void> {
  if (pending.value || !actions) return;
  pending.value = true;
  errorMessage.value = null;
  try {
    await actions.startProviderReauth(provider);
  } catch (failure) {
    errorMessage.value = copyFor(failure as PasswordSettingsFailure);
  } finally {
    pending.value = false;
  }
}
</script>

<template>
  <Card data-testid="password-settings">
    <CardHeader>
      <h2 class="leading-none font-semibold">
        Password
      </h2>
    </CardHeader>
    <CardContent class="grid gap-4">
      <p data-testid="password-status">
        {{ hasPassword ? "You have a password." : "No password set." }}
      </p>

      <StatusBanner
        v-if="successMessage"
        kind="success"
        testid="password-success"
      >
        {{ successMessage }}
      </StatusBanner>
      <StatusBanner
        v-if="errorMessage"
        kind="error"
        testid="password-error"
        focus-on-mount
      >
        {{ errorMessage }}
      </StatusBanner>

      <Button
        v-if="mode === 'idle'"
        data-testid="password-action"
        type="button"
        @click="start"
      >
        {{ hasPassword ? "Change password" : "Add a password" }}
      </Button>

      <form
        v-else-if="mode === 'set'"
        data-testid="password-form"
        class="grid gap-4"
        novalidate
        @submit.prevent="submitSet"
      >
        <PasswordField
          id="password-new"
          ref="passwordField"
          v-model="newPassword"
          label="New password"
          autocomplete="new-password"
          confirm
        />
        <div class="flex gap-2">
          <Button
            data-testid="password-set-submit"
            :disabled="pending"
            type="submit"
          >
            {{ pending ? "Saving…" : "Save password" }}
          </Button>
          <Button
            data-testid="password-cancel"
            type="button"
            variant="ghost"
            @click="cancel"
          >
            Cancel
          </Button>
        </div>
      </form>

      <form
        v-else-if="mode === 'reauth-password'"
        data-testid="password-form"
        class="grid gap-4"
        novalidate
        @submit.prevent="submitReauth"
      >
        <PasswordField
          id="password-current"
          v-model="currentPassword"
          label="Current password"
          autocomplete="current-password"
        />
        <div class="flex gap-2">
          <Button
            data-testid="password-reauth-submit"
            :disabled="pending"
            type="submit"
          >
            {{ pending ? "Checking…" : "Continue" }}
          </Button>
          <Button
            data-testid="password-cancel"
            type="button"
            variant="ghost"
            @click="cancel"
          >
            Cancel
          </Button>
        </div>
      </form>

      <div
        v-else-if="mode === 'reauth-provider'"
        class="grid gap-3"
      >
        <p>Sign in again with your provider to continue.</p>
        <div class="flex flex-wrap gap-2">
          <Button
            v-for="provider in providers"
            :key="provider"
            :data-testid="`password-provider-reauth-${provider}`"
            :disabled="pending"
            type="button"
            @click="submitProviderReauth(provider)"
          >
            {{ `Continue with ${PROVIDER_LABELS[provider]}` }}
          </Button>
          <Button
            data-testid="password-cancel"
            type="button"
            variant="ghost"
            @click="cancel"
          >
            Cancel
          </Button>
        </div>
      </div>
    </CardContent>
  </Card>
</template>
