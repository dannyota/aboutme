<script setup lang="ts">
/**
 * Password reset landing page.
 *
 * Like verify-email, the single-use token arrives in the URL fragment and is
 * stripped synchronously during browser setup. The token lives in
 * component-local memory only until the reset POST completes, then is
 * replaced with an empty string. A malformed or missing fragment renders a
 * local error and no form.
 */
import PasswordField from '@/components/auth/PasswordField.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import { type AuthMessage, authCopy } from '@/i18n/auth';
import {
  type PasswordAuthFailure,
  type PasswordIssue,
  usePasswordAuth,
} from '../composables/usePasswordAuth';

useHead({
  meta: [{ name: 'referrer', content: 'no-referrer' }],
});

const { locale } = useLocale();
const copy = computed(() => authCopy[locale.value]);
const password = ref('');
const passwordField = ref<InstanceType<typeof PasswordField> | null>(null);
const pending = ref(false);
const errorMessage = ref<AuthMessage | null>(null);
const success = ref(false);
const tokenError = ref<AuthMessage | null>(null);

let token = '';

if (import.meta.client) {
  const { hash } = window.location;
  const params = new URLSearchParams(hash.replace(/^#/, ''));
  const values = params.getAll('token').filter((value) => value !== '');
  if (values.length === 1) {
    token = values[0] as string;
    // Strip the fragment before any request or telemetry.
    history.replaceState(
      null,
      '',
      window.location.pathname + window.location.search,
    );
  } else {
    tokenError.value = 'resetLinkIncomplete';
  }
}

const PASSWORD_ISSUE_MESSAGE: Record<PasswordIssue, AuthMessage> = {
  length: 'passwordLength',
  common: 'passwordCommon',
  breached: 'passwordBreached',
};

function messageFor(failure: PasswordAuthFailure): AuthMessage {
  switch (failure.kind) {
    case 'invalid-token':
      return 'resetLinkExpired';
    case 'password-invalid':
      return failure.issue
        ? PASSWORD_ISSUE_MESSAGE[failure.issue]
        : 'passwordInvalid';
    case 'rate-limited':
      return 'rateLimited';
    case 'unavailable':
      return 'unavailable';
    default:
      return 'checkDetails';
  }
}

async function onSubmit() {
  if (passwordField.value?.confirmMismatch) {
    errorMessage.value = 'passwordsDoNotMatch';
    return;
  }
  if (!password.value) {
    errorMessage.value = 'enterNewPassword';
    return;
  }
  pending.value = true;
  errorMessage.value = null;
  const resetToken = token;
  try {
    await usePasswordAuth().reset({
      token: resetToken,
      password: password.value,
    });
    success.value = true;
    password.value = '';
  } catch (failure) {
    errorMessage.value = messageFor(failure as PasswordAuthFailure);
  } finally {
    pending.value = false;
    token = '';
  }
}
</script>

<template>
  <main
    class="mx-auto w-full max-w-[26rem] px-6 py-16"
    data-testid="reset-password-page"
  >
    <h1
      class="border-b pb-4 text-xl font-semibold"
      data-page-title
    >
      {{ copy.reset.title }}
    </h1>
    <p class="mt-4 text-base text-muted-foreground">
      {{ copy.reset.lead }}
    </p>
    <StatusBanner
      v-if="tokenError || errorMessage"
      class="mt-6"
      kind="error"
      testid="reset-error"
    >
      {{ copy.messages[tokenError ?? errorMessage ?? 'unavailable'] }}
    </StatusBanner>
    <StatusBanner
      v-if="success"
      class="mt-6"
      kind="success"
      testid="reset-success"
    >
      {{ copy.reset.success }}
    </StatusBanner>
    <form
      v-else-if="!tokenError"
      class="mt-8 grid gap-6"
      data-testid="reset-form"
      novalidate
      @submit.prevent="onSubmit"
    >
      <PasswordField
        id="reset-password"
        ref="passwordField"
        v-model="password"
        autocomplete="new-password"
        confirm
        :label="copy.newPassword"
        :locale="locale"
      />
      <Button
        class="h-9 w-full"
        :disabled="pending"
        type="submit"
      >
        {{ pending ? copy.reset.pending : copy.reset.submit }}
      </Button>
    </form>
    <nav class="mt-6 flex justify-between gap-3 text-sm">
      <NuxtLink
        v-if="success"
        class="text-primary underline-offset-4 hover:underline"
        to="/login"
      >
        {{ copy.signIn }}
      </NuxtLink>
    </nav>
  </main>
</template>
