<script setup lang="ts">
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

const offerProviders = computed(() =>
  loginProviders.value.length > 0
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
      v-if="status === 'error' && offerProviders"
      class="mt-6 grid gap-3 text-sm text-muted-foreground"
      data-testid="verify-providers"
    >
      <p>{{ copy.verify.useProviders }}</p>
      <ProviderButtons
        :locale="locale"
        :providers="loginProviders"
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
