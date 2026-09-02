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
import {
  type PasswordAuthFailure,
  usePasswordAuth,
} from '../composables/usePasswordAuth';
import AuthCard from '@/components/auth/AuthCard.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';

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
  <AuthCard
    description="Follow the link in your email to verify your address."
    title="Verify email"
  >
    <StatusBanner
      v-if="status === 'error'"
      kind="error"
      testid="verify-error"
    >
      {{ errorMessage }}
    </StatusBanner>
    <StatusBanner
      v-else-if="status === 'success'"
      kind="success"
      testid="verify-success"
    >
      Email verified. Sign in.
    </StatusBanner>
    <p
      v-else
      class="text-sm text-muted-foreground"
    >
      Verifying your email address…
    </p>
    <template #footer>
      <NuxtLink
        class="text-primary underline-offset-4 hover:underline"
        to="/login"
      >
        Sign in
      </NuxtLink>
    </template>
  </AuthCard>
</template>
