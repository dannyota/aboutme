<script setup lang="ts">
/**
 * Password reset landing page.
 *
 * Like verify-email, the single-use token arrives in the URL fragment and is
 * stripped synchronously during browser setup. The token lives in
 * component-local memory only until the reset POST completes, then is
 * replaced with an empty string. A malformed or missing fragment renders a
 * local error and no form.
 */
import AuthCard from '@/components/auth/AuthCard.vue';
import PasswordField from '@/components/auth/PasswordField.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import {
  type PasswordAuthFailure,
  type PasswordIssue,
  usePasswordAuth,
} from '../composables/usePasswordAuth';

useHead({
  meta: [{ name: 'referrer', content: 'no-referrer' }],
});

const password = ref('');
const passwordField = ref<InstanceType<typeof PasswordField> | null>(null);
const pending = ref(false);
const errorMessage = ref<string | null>(null);
const success = ref(false);
const tokenError = ref<string | null>(null);

let token = '';

if (import.meta.client) {
  const { hash } = window.location;
  const params = new URLSearchParams(hash.replace(/^#/, ''));
  const values = params.getAll('token').filter((value) => value !== '');
  if (values.length === 1) {
    token = values[0] as string;
    // Strip the fragment before any request or telemetry.
    history.replaceState(
      null,
      '',
      window.location.pathname + window.location.search,
    );
  } else {
    tokenError.value = 'This reset link is invalid or incomplete.';
  }
}

const PASSWORD_ISSUE_COPY: Record<PasswordIssue, string> = {
  length: 'Password must be at least 12 characters.',
  common: 'That password is too common. Choose a different one.',
  breached:
    'That password was exposed in a data breach. Choose a different one.',
};

function copyFor(failure: PasswordAuthFailure): string {
  switch (failure.kind) {
    case 'invalid-token':
      return 'This reset link is invalid or has expired.';
    case 'password-invalid':
      return failure.issue
        ? PASSWORD_ISSUE_COPY[failure.issue]
        : 'Password does not meet our requirements.';
    case 'rate-limited':
      return 'Too many attempts. Try again later.';
    case 'unavailable':
      return 'Something went wrong. Please try again.';
    default:
      return 'Check your details and try again.';
  }
}

async function onSubmit() {
  if (passwordField.value?.confirmMismatch) {
    errorMessage.value = 'Passwords do not match.';
    return;
  }
  if (!password.value) {
    errorMessage.value = 'Enter a new password.';
    return;
  }
  pending.value = true;
  errorMessage.value = null;
  const resetToken = token;
  try {
    await usePasswordAuth().reset({
      token: resetToken,
      password: password.value,
    });
    success.value = true;
    password.value = '';
  } catch (failure) {
    errorMessage.value = copyFor(failure as PasswordAuthFailure);
  } finally {
    pending.value = false;
    token = '';
  }
}
</script>

<template>
  <AuthCard
    description="Choose a new password for your account."
    title="Reset password"
  >
    <StatusBanner
      v-if="tokenError || errorMessage"
      kind="error"
      testid="reset-error"
    >
      {{ tokenError ?? errorMessage }}
    </StatusBanner>
    <StatusBanner
      v-if="success"
      kind="success"
      testid="reset-success"
    >
      Password reset. Sign in.
    </StatusBanner>
    <form
      v-else-if="!tokenError"
      class="grid gap-4"
      data-testid="reset-form"
      novalidate
      @submit.prevent="onSubmit"
    >
      <PasswordField
        id="reset-password"
        ref="passwordField"
        v-model="password"
        autocomplete="new-password"
        confirm
        label="New password"
      />
      <Button
        class="mt-1 w-full"
        :disabled="pending"
        type="submit"
      >
        {{ pending ? "Resetting…" : "Reset password" }}
      </Button>
    </form>
    <template #footer>
      <NuxtLink
        v-if="success"
        class="text-primary underline-offset-4 hover:underline"
        to="/login"
      >
        Sign in
      </NuxtLink>
    </template>
  </AuthCard>
</template>
