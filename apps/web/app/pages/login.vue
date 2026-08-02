<script setup lang="ts">
/**
 * Login page: static provider links + a query-param-driven error banner.
 *
 * Provider buttons are plain `<a href>` elements, never fetch/JS-driven
 * navigation — `/api/v1/auth/{provider}/start` sets a cookie and issues a
 * redirect, which requires a real top-level browser navigation.
 *
 * `?error=` is a closed vocabulary produced by the callback landing
 * redirect: `auth_failed`, `email_not_verified`, `cancelled`, and
 * `email_already_registered`. Copy here is intentionally minimal — P5B
 * owns wording polish.
 */
const route = useRoute();

const providers = [
  { id: 'google', label: 'Continue with Google' },
  { id: 'github', label: 'Continue with GitHub' },
  { id: 'linkedin', label: 'Continue with LinkedIn' },
] as const;

const errorMessages: Record<string, string> = {
  auth_failed: 'Something went wrong while signing you in. Please try '
    + 'again.',
  email_not_verified: 'Your email address must be verified with your '
    + 'provider before you can sign in.',
  cancelled: 'Sign-in was cancelled.',
  // Deliberately does not name the existing provider — naming it hands an
  // attacker a targeted-phishing hint (spec: OAuth email-collision rule).
  email_already_registered: 'An account with this email already '
    + 'exists. Sign in with the provider you used originally.',
};

const errorCode = computed(() => {
  const value = route.query.error;
  return typeof value === 'string' ? value : null;
});

const errorMessage = computed(() => {
  if (!errorCode.value) return null;
  return errorMessages[errorCode.value] ?? errorMessages.auth_failed;
});
</script>

<template>
  <section class="login">
    <h1>Sign in</h1>

    <p
      v-if="errorMessage"
      data-testid="login-error"
      role="alert"
    >
      {{ errorMessage }}
    </p>

    <ul class="login-providers">
      <li
        v-for="provider in providers"
        :key="provider.id"
      >
        <a :href="`/api/v1/auth/${provider.id}/start`">
          {{ provider.label }}
        </a>
      </li>
    </ul>
  </section>
</template>
