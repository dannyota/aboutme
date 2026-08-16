<script setup lang="ts">
/**
 * Login page: email/password form + static OAuth provider links.
 *
 * Provider buttons are plain `<a href>` elements, never fetch/JS-driven
 * navigation — `/api/v1/auth/{provider}/start` sets a cookie and issues a
 * redirect, which requires a real top-level browser navigation.
 *
 * `?error=` is a closed vocabulary produced by the callback landing
 * redirect: `auth_failed`, `email_not_verified`, `cancelled`, and
 * `email_already_registered`. Copy here is intentionally minimal — P5B
 * owns wording polish.
 *
 * The password form sends closed copy for every failure and never retains
 * the password after a successful login.
 */
import '~/assets/css/auth.css';
import PasswordField from '../components/auth/PasswordField.vue';
import {
  type PasswordAuthFailure,
  usePasswordAuth,
} from '../composables/usePasswordAuth';

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
  // `errorMessages[code]` alone would resolve inherited properties too
  // (`?error=constructor` renders `Object`'s constructor function,
  // `?error=__proto__` renders `{}`) rather than falling back — restrict
  // the lookup to the map's own keys, the actual closed vocabulary.
  if (Object.hasOwn(errorMessages, errorCode.value)) {
    return errorMessages[errorCode.value];
  }
  return errorMessages.auth_failed;
});

const email = ref('');
const password = ref('');
const pending = ref(false);
const formError = ref<string | null>(null);

function copyFor(failure: PasswordAuthFailure): string {
  switch (failure.kind) {
    case 'authentication-failed':
      return 'Invalid email or password.';
    case 'rate-limited':
      return 'Too many attempts. Try again later.';
    case 'unavailable':
      return 'Something went wrong. Please try again.';
    default:
      return 'Check your email and password and try again.';
  }
}

async function onSubmit() {
  if (!email.value || !password.value) {
    formError.value = 'Enter your email and password.';
    return;
  }
  pending.value = true;
  formError.value = null;
  try {
    await usePasswordAuth().login({
      email: email.value,
      password: password.value,
    });
    password.value = '';
    await navigateTo('/app/resumes');
  } catch (failure) {
    formError.value = copyFor(failure as PasswordAuthFailure);
  } finally {
    pending.value = false;
  }
}
</script>

<template>
  <main class="app-page app-page--narrow">
    <section class="login">
      <h1>Sign in</h1>

      <p
        v-if="errorMessage"
        data-testid="login-error"
        role="alert"
      >
        {{ errorMessage }}
      </p>

      <div
        v-if="formError"
        data-testid="login-form-error"
        class="auth-error-summary"
        role="alert"
      >
        {{ formError }}
      </div>

      <form
        class="auth-form"
        novalidate
        @submit.prevent="onSubmit"
      >
        <div class="auth-field">
          <label for="login-email">Email</label>
          <input
            id="login-email"
            v-model="email"
            type="email"
            autocomplete="email"
          >
        </div>

        <PasswordField
          id="login-password"
          v-model="password"
          label="Password"
          autocomplete="current-password"
        />

        <button
          type="submit"
          class="auth-submit"
          :disabled="pending"
        >
          {{ pending ? 'Signing in…' : 'Sign in' }}
        </button>
      </form>

      <div class="auth-links">
        <NuxtLink to="/forgot-password">
          Forgot password?
        </NuxtLink>
        <NuxtLink to="/register">
          Create account
        </NuxtLink>
      </div>

      <div class="auth-divider">
        or
      </div>

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
  </main>
</template>
