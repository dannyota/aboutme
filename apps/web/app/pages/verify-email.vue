<script setup lang="ts">
/**
 * Email verification landing page.
 *
 * The single-use token arrives in the URL fragment (the email link points at
 * `#token=...`). Reading the fragment, stripping it, and firing the request
 * happen synchronously during browser setup — before any await or telemetry —
 * so a refresh or replay of the address never re-sends the token, and the
 * token never appears in route query, history, state, or the DOM. A malformed
 * or missing fragment fails locally with no request at all.
 */
import '~/assets/css/auth.css';
import {
  type PasswordAuthFailure,
  usePasswordAuth,
} from '../composables/usePasswordAuth';

useHead({
  meta: [{ name: 'referrer', content: 'no-referrer' }],
});

const status = ref<'verifying' | 'success' | 'error'>('verifying');
const errorMessage = ref<string | null>(null);

let token = '';

if (import.meta.client) {
  const { hash } = window.location;
  const params = new URLSearchParams(hash.replace(/^#/, ''));
  const values = params.getAll('token').filter((value) => value !== '');
  if (values.length === 1) {
    token = values[0] as string;
    // Strip the fragment before the request so no trace carries the token.
    history.replaceState(
      null,
      '',
      window.location.pathname + window.location.search,
    );
  } else {
    status.value = 'error';
    errorMessage.value = 'This verification link is invalid or incomplete.';
  }
}

if (token !== '') {
  usePasswordAuth().verify(token)
    .then(() => {
      status.value = 'success';
    })
    .catch((failure: PasswordAuthFailure) => {
      status.value = 'error';
      errorMessage.value = copyFor(failure);
    })
    .finally(() => {
      token = '';
    });
}

function copyFor(failure: PasswordAuthFailure): string {
  switch (failure.kind) {
    case 'invalid-token':
      return 'This verification link is invalid or has expired.';
    case 'rate-limited':
      return 'Too many attempts. Try again later.';
    case 'unavailable':
      return 'Something went wrong. Please try again.';
    default:
      return 'This verification link is invalid or incomplete.';
  }
}
</script>

<template>
  <main class="aboutme-app">
    <section class="app-page app-page--narrow">
      <div class="login">
        <h1>Verify your email</h1>

        <div
          v-if="status === 'error'"
          data-testid="verify-error"
          class="auth-error-summary"
          role="alert"
        >
          {{ errorMessage }}
        </div>

        <div
          v-else-if="status === 'success'"
          data-testid="verify-success"
          class="auth-success"
          role="status"
        >
          Email verified. Sign in.
        </div>

        <p
          v-else
          class="auth-note"
        >
          Verifying your email address…
        </p>

        <p class="auth-note">
          <NuxtLink to="/login">
            Sign in
          </NuxtLink>
        </p>
      </div>
    </section>
  </main>
</template>
