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
import StatusBanner from '@/components/app/StatusBanner.vue';
import { type AuthMessage, authCopy } from '@/i18n/auth';

useHead({
  meta: [{ name: 'referrer', content: 'no-referrer' }],
});

const { locale } = useLocale();
const copy = computed(() => authCopy[locale.value]);
const status = ref<'verifying' | 'success' | 'error'>('verifying');
const errorMessage = ref<AuthMessage | null>(null);

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
  <main
    class="mx-auto w-full max-w-[26rem] px-6 py-16"
    data-testid="verify-email-page"
  >
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
        class="text-primary underline-offset-4 hover:underline"
        to="/login"
      >
        {{ copy.signIn }}
      </NuxtLink>
    </nav>
  </main>
</template>
