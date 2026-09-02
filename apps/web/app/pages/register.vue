<script setup lang="ts">
/**
 * Registration page: name/email/password with component-local confirmation.
 *
 * The server is authoritative for password policy; the client checks only
 * required fields and local confirmation, then maps the closed policy issues
 * to fixed copy. The 202 success copy is fixed and reveals nothing about
 * email ownership.
 */
import PasswordField from '../components/auth/PasswordField.vue';
import {
  type PasswordAuthFailure,
  type PasswordIssue,
  usePasswordAuth,
} from '../composables/usePasswordAuth';

const name = ref('');
const email = ref('');
const password = ref('');
const passwordField = ref<InstanceType<typeof PasswordField> | null>(null);
const pending = ref(false);
const errorMessage = ref<string | null>(null);
const success = ref(false);
const errorSummary = ref<HTMLElement | null>(null);

const PASSWORD_ISSUE_COPY: Record<PasswordIssue, string> = {
  length: 'Password must be at least 12 characters.',
  common: 'That password is too common. Choose a different one.',
  breached:
    'That password was exposed in a data breach. Choose a different one.',
};

function copyFor(failure: PasswordAuthFailure): string {
  switch (failure.kind) {
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
  if (!name.value || !email.value || !password.value) {
    errorMessage.value = 'Please fill in all fields.';
    return;
  }
  if (passwordField.value?.confirmMismatch) {
    errorMessage.value = 'Passwords do not match.';
    return;
  }
  pending.value = true;
  errorMessage.value = null;
  try {
    await usePasswordAuth().register({
      name: name.value,
      email: email.value,
      password: password.value,
    });
    success.value = true;
    password.value = '';
  } catch (failure) {
    errorMessage.value = copyFor(failure as PasswordAuthFailure);
    await nextTick();
    errorSummary.value?.focus();
  } finally {
    pending.value = false;
  }
}
</script>

<template>
  <main class="aboutme-app">
    <section class="app-page app-page--narrow">
      <div class="login">
        <h1>Create account</h1>
        <p class="auth-note">
          Already have an account?
          <NuxtLink to="/login">
            Sign in
          </NuxtLink>
        </p>

        <div
          v-if="errorMessage"
          ref="errorSummary"
          data-testid="register-error"
          class="auth-error-summary"
          role="alert"
          tabindex="-1"
        >
          {{ errorMessage }}
        </div>

        <div
          v-if="success"
          data-testid="register-success"
          class="auth-success"
          role="status"
        >
          Check your email to verify your address.
        </div>

        <form
          class="auth-form"
          novalidate
          @submit.prevent="onSubmit"
        >
          <div class="auth-field">
            <label for="register-name">Name</label>
            <input
              id="register-name"
              v-model="name"
              type="text"
              autocomplete="name"
            >
          </div>
          <div class="auth-field">
            <label for="register-email">Email</label>
            <input
              id="register-email"
              v-model="email"
              type="email"
              autocomplete="email"
            >
          </div>

          <PasswordField
            id="register-password"
            ref="passwordField"
            v-model="password"
            label="Password"
            autocomplete="new-password"
            confirm
          />

          <button
            type="submit"
            class="auth-submit"
            :disabled="pending"
          >
            {{ pending ? 'Creating account…' : 'Create account' }}
          </button>
        </form>
      </div>
    </section>
  </main>
</template>
