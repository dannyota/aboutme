<script setup lang="ts">
/**
 * Password recovery page.
 *
 * The server always answers `202` and only sends a reset email when a
 * password account exists. Every outcome — success or rejection — renders the
 * same fixed, account-neutral copy, so this page never reveals whether an
 * email is registered.
 */
import '~/assets/css/auth.css';
import { usePasswordAuth } from '../composables/usePasswordAuth';

const GENERIC_COPY
  = 'If an account exists for this email, we\'ve sent a password reset link.';

const email = ref('');
const pending = ref(false);
const errorMessage = ref<string | null>(null);
const success = ref(false);

async function onSubmit() {
  if (!email.value) {
    errorMessage.value = 'Enter your email address.';
    return;
  }
  pending.value = true;
  errorMessage.value = null;
  try {
    await usePasswordAuth().forgot(email.value);
    success.value = true;
  } catch {
    errorMessage.value = GENERIC_COPY;
  } finally {
    pending.value = false;
  }
}
</script>

<template>
  <main class="aboutme-app">
    <section class="app-page app-page--narrow">
      <div class="login">
        <h1>Forgot your password?</h1>
        <p class="auth-note">
          Enter your email and we'll send a reset link if an account exists.
        </p>

        <div
          v-if="errorMessage"
          data-testid="forgot-error"
          class="auth-error-summary"
          role="alert"
        >
          {{ errorMessage }}
        </div>

        <div
          v-if="success"
          data-testid="forgot-success"
          class="auth-success"
          role="status"
        >
          {{ GENERIC_COPY }}
        </div>

        <form
          v-else
          class="auth-form"
          novalidate
          @submit.prevent="onSubmit"
        >
          <div class="auth-field">
            <label for="forgot-email">Email</label>
            <input
              id="forgot-email"
              v-model="email"
              type="email"
              autocomplete="email"
            >
          </div>

          <button
            type="submit"
            class="auth-submit"
            :disabled="pending"
          >
            {{ pending ? 'Sending…' : 'Send reset link' }}
          </button>
        </form>

        <p class="auth-note">
          <NuxtLink to="/login">
            Back to sign in
          </NuxtLink>
        </p>
      </div>
    </section>
  </main>
</template>
