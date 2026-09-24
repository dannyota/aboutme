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
 *
 * This page opens in a new tab with no query of its own, so the sign-in link
 * below can only recover register.vue's validated `?next=` from storage. That
 * read happens on the client in onMounted, so the server render and first
 * client render agree on plain `/login` and the link updates after.
 */
import {
  type PasswordAuthFailure,
  usePasswordAuth,
} from '../composables/usePasswordAuth';
import StatusBanner from '@/components/app/StatusBanner.vue';
import AuthLayout from '@/components/auth/AuthLayout.vue';
import ProviderButtons from '@/components/auth/ProviderButtons.vue';
import { useCapabilities } from '@/composables/useCapabilities';
import { type AuthMessage, authCopy } from '@/i18n/auth';
import { pageTitle } from '@/i18n/meta';
import { take } from '@/utils/pendingReturnPath';

useHead({
  meta: [{ name: 'referrer', content: 'no-referrer' }],
});

const { locale } = useLocale();
const copy = computed(() => authCopy[locale.value]);
useHead(computed(() => ({ title: pageTitle(copy.value.verify.title) })));
const { loginProviders } = useCapabilities();
const status = ref<'verifying' | 'success' | 'error'>('verifying');
const errorMessage = ref<AuthMessage | null>(null);
const pendingNext = ref<string | null>(null);
const signInLink = computed(() => (pendingNext.value
  ? `/login?next=${encodeURIComponent(pendingNext.value)}`
  : '/login'));

onMounted(() => {
  pendingNext.value = take();
});

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
    errorMessage.value = 'verifyLinkIncomplete';
  }
}

if (token !== '') {
  usePasswordAuth()
    .verify(token)
    .then(() => {
      status.value = 'success';
    })
    .catch((failure: PasswordAuthFailure) => {
      status.value = 'error';
      errorMessage.value = messageFor(failure);
    })
    .finally(() => {
      token = '';
    });
}

// A dead or broken link can be sidestepped by signing in with Google, which
// needs no verification email.
const offerGoogle = computed(() =>
  loginProviders.value.includes('google')
  && (errorMessage.value === 'verifyLinkExpired'
    || errorMessage.value === 'verifyLinkIncomplete'));

function messageFor(failure: PasswordAuthFailure): AuthMessage {
  switch (failure.kind) {
    case 'invalid-token':
      return 'verifyLinkExpired';
    case 'rate-limited':
      return 'rateLimited';
    case 'unavailable':
      return 'unavailable';
    default:
      return 'verifyLinkIncomplete';
  }
}
</script>

<template>
  <AuthLayout testid="verify-email-page">
    <h1
      class="border-b pb-4 text-xl font-semibold"
      data-page-title
    >
      {{ copy.verify.title }}
    </h1>
    <p class="mt-4 text-base text-muted-foreground">
      {{ copy.verify.lead }}
    </p>
    <StatusBanner
      v-if="status === 'error'"
      class="mt-6"
      kind="error"
      testid="verify-error"
    >
      {{ errorMessage ? copy.messages[errorMessage] : '' }}
    </StatusBanner>
    <div
      v-if="status === 'error' && offerGoogle"
      class="mt-6 grid gap-3 text-sm text-muted-foreground"
      data-testid="verify-google"
    >
      <p>{{ copy.verify.useGoogle }}</p>
      <ProviderButtons
        :locale="locale"
        :providers="['google']"
      />
    </div>
    <StatusBanner
      v-else-if="status === 'success'"
      class="mt-6"
      kind="success"
      testid="verify-success"
    >
      {{ copy.verify.success }}
    </StatusBanner>
    <p
      v-else
      class="mt-8 text-base text-muted-foreground"
    >
      {{ copy.verify.pending }}
    </p>
    <nav class="mt-6 flex justify-between gap-3 text-sm">
      <NuxtLink
        class="text-link underline-offset-4 hover:underline"
        data-testid="verify-sign-in"
        :to="signInLink"
      >
        {{ copy.signIn }}
      </NuxtLink>
    </nav>
  </AuthLayout>
</template>
