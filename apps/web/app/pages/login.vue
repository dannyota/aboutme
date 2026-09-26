<script setup lang="ts">
/**
 * Login page: email/password form + optional static OAuth provider links.
 *
 * Provider buttons render only for the providers the capabilities read
 * enables (`ProviderButtons` explains why they are plain links).
 *
 * `?error=` is a closed vocabulary produced by the callback landing
 * redirect: `auth_failed`, `email_not_verified`, `cancelled`, and
 * `email_already_registered`. Copy for both languages lives in
 * `app/i18n/auth.ts`. `email_already_registered` also carries `?provider=`,
 * the provider just used; only a value from the closed provider allowlist
 * names it, and it is never rendered raw.
 *
 * The password form sends closed copy for every failure and never retains
 * the password after a successful login. An account enrolled in a second
 * factor gets no session from this form: it navigates to the fixed
 * `/login/second-factor` path instead, which finishes the sign-in.
 */
import FormField from '@/components/app/FormField.vue';
import AuthLayout from '@/components/auth/AuthLayout.vue';
import PasswordField from '@/components/auth/PasswordField.vue';
import LegalAgreement from '@/components/legal/LegalAgreement.vue';
import ProviderButtons from '@/components/auth/ProviderButtons.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Separator } from '@/components/ui/separator';
import { type AuthMessage, authCopy } from '@/i18n/auth';
import {
  type PasswordAuthFailure,
  usePasswordAuth,
} from '../composables/usePasswordAuth';
import { providerNames, useCapabilities } from '../composables/useCapabilities';
import { pageTitle } from '@/i18n/meta';
import {
  DEFAULT_RETURN_PATH,
  isAppRoute,
  validateReturnPath,
} from '@/utils/returnPath';

const route = useRoute();
// `app/pages/login/second-factor.vue` makes this page the parent route of
// `/login/second-factor`, so the child has nowhere to render unless this page
// renders it. The sign-in form and its head title belong to `/login` alone.
const onChildRoute = computed(() => route.matched.length > 1);
const { loginProviders, resolved } = useCapabilities();
const { locale } = useLocale();
const copy = computed(() => authCopy[locale.value]);
useHead(computed(() => (onChildRoute.value
  ? {}
  : { title: pageTitle(copy.value.signIn) })));

const explicitNext = computed(() => validateReturnPath(route.query.next));
const loginDestination = computed(
  () => explicitNext.value ?? DEFAULT_RETURN_PATH,
);
const registerLink = computed(() => (explicitNext.value
  ? `/register?next=${encodeURIComponent(explicitNext.value)}`
  : '/register'));

const errorMessages: Record<string, AuthMessage> = {
  auth_failed: 'providerFailed',
  email_not_verified: 'providerEmailNotVerified',
  cancelled: 'providerCancelled',
};

const errorCode = computed(() => {
  const value = route.query.error;
  return typeof value === 'string' ? value : null;
});

const providerDisplayName = computed(() => {
  const value = route.query.provider;
  // Only a value from the closed allowlist names a provider; anything else,
  // including a prototype property name, gets the copy's neutral wording and
  // is never rendered (docs/design/linkedin-sign-in.md "Web").
  return typeof value === 'string' && Object.hasOwn(providerNames, value)
    ? providerNames[value as keyof typeof providerNames]
    : null;
});

const errorMessage = computed(() => {
  if (!errorCode.value) return null;
  if (errorCode.value === 'email_already_registered') {
    return copy.value.providerEmailRegistered(providerDisplayName.value);
  }
  // `errorMessages[code]` alone would resolve inherited properties too
  // (`?error=constructor` renders `Object`'s constructor function,
  // `?error=__proto__` renders `{}`) rather than falling back — restrict
  // the lookup to the map's own keys, the actual closed vocabulary.
  const key = Object.hasOwn(errorMessages, errorCode.value)
    ? errorMessages[errorCode.value] as AuthMessage
    : 'providerFailed';
  return copy.value.messages[key];
});

const email = ref('');
const password = ref('');
const pending = ref(false);
const formError = ref<AuthMessage | null>(null);

function messageFor(failure: PasswordAuthFailure): AuthMessage {
  switch (failure.kind) {
    case 'authentication-failed':
      return 'invalidCredentials';
    case 'rate-limited':
      return 'rateLimited';
    case 'unavailable':
      return 'unavailable';
    default:
      return 'checkEmailAndPassword';
  }
}

// A destination outside this app's own pages (for example
// `/oauth/authorize`, served by the Go backend for a connected-agent
// consent link) needs a real browser navigation: the client router has no
// route for it and would otherwise strand the user on a dead page.
async function goTo(path: string): Promise<void> {
  if (isAppRoute(path)) {
    await navigateTo(path);
  } else {
    await navigateTo(path, { external: true });
  }
}

async function onSubmit() {
  if (!email.value || !password.value) {
    formError.value = 'enterEmailAndPassword';
    return;
  }
  pending.value = true;
  formError.value = null;
  try {
    const result = await usePasswordAuth().login({
      email: email.value,
      password: password.value,
      next: explicitNext.value ?? undefined,
    });
    password.value = '';
    // An enrolled account gets no session yet: the fixed pending path, never
    // a computed one, since the server carries the return path instead.
    await goTo(
      result.secondFactorRequired
        ? '/login/second-factor'
        : loginDestination.value,
    );
  } catch (failure) {
    formError.value = messageFor(failure as PasswordAuthFailure);
  } finally {
    pending.value = false;
  }
}
</script>

<template>
  <NuxtPage v-if="onChildRoute" />
  <AuthLayout
    v-else
    testid="login-page"
  >
    <h1
      class="border-b pb-4 text-xl font-semibold"
      data-page-title
    >
      {{ copy.signIn }}
    </h1>
    <p class="mt-4 text-base text-muted-foreground">
      {{ copy.login.lead }}
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
      {{ copy.messages[formError] }}
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
        :label="copy.email"
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
        :label="copy.password"
        :locale="locale"
      />
      <Button
        class="h-9 w-full"
        :disabled="pending"
        type="submit"
      >
        {{ pending ? copy.login.pending : copy.signIn }}
      </Button>
    </form>
    <!-- The capabilities read is client-only, so hold the space one provider
         button takes until it resolves; no link renders before then. -->
    <div
      v-if="!resolved"
      aria-hidden="true"
      class="invisible"
      data-testid="provider-placeholder"
    >
      <div class="mt-8 flex items-center gap-3 text-xs">
        <Separator class="flex-1" />
        {{ copy.or }}
        <Separator class="flex-1" />
      </div>
      <div class="mt-4 h-10" />
      <LegalAgreement
        class="mt-3"
        :locale="locale"
        testid="login-agreement-placeholder"
      />
    </div>
    <template v-else-if="loginProviders.length > 0">
      <div
        class="mt-8 flex items-center gap-3 text-xs text-muted-foreground"
        data-testid="login-divider"
      >
        <Separator class="flex-1" />
        {{ copy.or }}
        <Separator class="flex-1" />
      </div>
      <ProviderButtons
        class="mt-4"
        :locale="locale"
        :next="explicitNext"
        :providers="loginProviders"
      />
      <!-- A first provider sign-in creates the account. -->
      <LegalAgreement
        class="mt-3"
        :locale="locale"
        testid="login-agreement"
      />
    </template>
    <nav class="mt-6 flex justify-between gap-3 text-sm">
      <NuxtLink
        class="text-link underline-offset-4 hover:underline"
        to="/forgot-password"
      >
        {{ copy.login.forgotPassword }}
      </NuxtLink>
      <NuxtLink
        class="text-link underline-offset-4 hover:underline"
        data-testid="login-create-account"
        :to="registerLink"
      >
        {{ copy.createAccount }}
      </NuxtLink>
    </nav>
  </AuthLayout>
</template>
