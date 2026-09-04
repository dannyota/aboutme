<script setup lang="ts">
/**
 * Login page: email/password form + optional static OAuth provider links.
 *
 * Provider buttons render only when the capabilities read reports
 * `providerLogin`. They are plain `<a href>` elements, never fetch/JS-driven
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
import FormField from '@/components/app/FormField.vue';
import PasswordField from '@/components/auth/PasswordField.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Separator } from '@/components/ui/separator';
import {
  type PasswordAuthFailure,
  usePasswordAuth,
} from '../composables/usePasswordAuth';
import { useCapabilities } from '../composables/useCapabilities';

const route = useRoute();
const { providerLogin } = useCapabilities();

const FALLBACK_NEXT = '/app/resumes';

/** Accept only a bounded, same-origin relative return path. */
function validateLoginNext(value: unknown): string | null {
  if (typeof value !== 'string' || value === '' || !value.startsWith('/')) {
    return null;
  }
  if (value.startsWith('//') || /[\\\r\n]/.test(value)) return null;
  if (/^[A-Za-z][A-Za-z0-9+.-]*:/.test(value)) return null;
  if (/%(?![0-9A-Fa-f]{2})/.test(value)) return null;
  if (new TextEncoder().encode(value).byteLength > 2048) return null;
  try {
    const parsed = new URL(value, 'https://aboutme.invalid');
    if (parsed.origin !== 'https://aboutme.invalid') return null;
  } catch {
    return null;
  }
  return value;
}

const explicitNext = computed(() => validateLoginNext(route.query.next));
const loginDestination = computed(() => explicitNext.value ?? FALLBACK_NEXT);

const providers = [
  { id: 'google', label: 'Continue with Google' },
  { id: 'github', label: 'Continue with GitHub' },
  { id: 'linkedin', label: 'Continue with LinkedIn' },
] as const;

const errorMessages: Record<string, string> = {
  auth_failed:
    'Something went wrong while signing you in. Please try ' + 'again.',
  email_not_verified:
    'Your email address must be verified with your '
    + 'provider before you can sign in.',
  cancelled: 'Sign-in was cancelled.',
  // Deliberately does not name the existing provider — naming it hands an
  // attacker a targeted-phishing hint (spec: OAuth email-collision rule).
  email_already_registered:
    'An account with this email already '
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
    await navigateTo(loginDestination.value);
  } catch (failure) {
    formError.value = copyFor(failure as PasswordAuthFailure);
  } finally {
    pending.value = false;
  }
}
</script>

<template>
  <main
    class="mx-auto w-full max-w-[26rem] px-6 py-16"
    data-testid="login-page"
  >
    <h1
      class="border-b pb-4 text-xl font-semibold"
      data-page-title
    >
      Sign in
    </h1>
    <p class="mt-4 text-base text-muted-foreground">
      Use the email and password for your account.
    </p>
    <StatusBanner
      v-if="errorMessage"
      class="mt-6"
      kind="error"
      testid="login-error"
    >
      {{ errorMessage }}
    </StatusBanner>
    <StatusBanner
      v-if="formError"
      class="mt-6"
      kind="error"
      testid="login-form-error"
    >
      {{ formError }}
    </StatusBanner>
    <form
      class="mt-8 grid gap-6"
      data-testid="login-form"
      novalidate
      @submit.prevent="onSubmit"
    >
      <FormField
        id="login-email"
        v-slot="{ id, describedBy, invalid }"
        label="Email"
      >
        <Input
          :id="id"
          v-model="email"
          :aria-describedby="describedBy"
          :aria-invalid="invalid"
          autocomplete="email"
          type="email"
        />
      </FormField>
      <PasswordField
        id="login-password"
        v-model="password"
        autocomplete="current-password"
        label="Password"
      />
      <Button
        class="h-9 w-full"
        :disabled="pending"
        type="submit"
      >
        {{ pending ? 'Signing in…' : 'Sign in' }}
      </Button>
    </form>
    <template v-if="providerLogin">
      <div
        class="mt-8 flex items-center gap-3 text-xs text-muted-foreground"
        data-testid="login-divider"
      >
        <Separator class="flex-1" />
        or
        <Separator class="flex-1" />
      </div>
      <ul class="mt-4 grid gap-2">
        <li
          v-for="provider in providers"
          :key="provider.id"
        >
          <Button
            as="a"
            class="w-full"
            :href="
              explicitNext
                ? `/api/v1/auth/${provider.id}/start?next=${encodeURIComponent(
                  explicitNext,
                )}`
                : `/api/v1/auth/${provider.id}/start`
            "
            variant="outline"
          >
            {{ provider.label }}
          </Button>
        </li>
      </ul>
    </template>
    <nav class="mt-6 flex justify-between gap-3 text-sm">
      <NuxtLink
        class="text-primary underline-offset-4 hover:underline"
        to="/forgot-password"
      >
        Forgot password?
      </NuxtLink>
      <NuxtLink
        class="text-primary underline-offset-4 hover:underline"
        to="/register"
      >
        Create account
      </NuxtLink>
    </nav>
  </main>
</template>
