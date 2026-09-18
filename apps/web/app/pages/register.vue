<script setup lang="ts">
/**
 * Registration page: name/email/password with component-local confirmation.
 *
 * The server is authoritative for password policy; the client checks only
 * required fields and local confirmation, then maps the closed policy issues
 * to fixed copy. The 202 success copy is fixed and reveals nothing about
 * email ownership. The server answers 202 before any mail is sent, so the
 * missing-email hint is shown to everyone and never claims a send failed.
 */
import FormField from '@/components/app/FormField.vue';
import PasswordField from '@/components/auth/PasswordField.vue';
import ProviderButtons from '@/components/auth/ProviderButtons.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Separator } from '@/components/ui/separator';
import { useCapabilities } from '@/composables/useCapabilities';
import { type AuthMessage, authCopy } from '@/i18n/auth';
import {
  type PasswordAuthFailure,
  type PasswordIssue,
  usePasswordAuth,
} from '../composables/usePasswordAuth';

const { locale } = useLocale();
const copy = computed(() => authCopy[locale.value]);
const { loginProviders } = useCapabilities();
const googleEnabled = computed(() => loginProviders.value.includes('google'));
const name = ref('');
const email = ref('');
const password = ref('');
const passwordField = ref<InstanceType<typeof PasswordField> | null>(null);
const pending = ref(false);
const errorMessage = ref<AuthMessage | null>(null);
const success = ref(false);
const errorSummary = ref<HTMLElement | null>(null);

const PASSWORD_ISSUE_MESSAGE: Record<PasswordIssue, AuthMessage> = {
  length: 'passwordLength',
  common: 'passwordCommon',
  breached: 'passwordBreached',
};

function messageFor(failure: PasswordAuthFailure): AuthMessage {
  switch (failure.kind) {
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
  if (!name.value || !email.value || !password.value) {
    errorMessage.value = 'fillAllFields';
    return;
  }
  if (passwordField.value?.confirmMismatch) {
    errorMessage.value = 'passwordsDoNotMatch';
    return;
  }
  pending.value = true;
  errorMessage.value = null;
  try {
    await usePasswordAuth().register({
      name: name.value,
      email: email.value,
      password: password.value,
    });
    success.value = true;
    password.value = '';
  } catch (failure) {
    errorMessage.value = messageFor(failure as PasswordAuthFailure);
    await nextTick();
    errorSummary.value?.focus();
  } finally {
    pending.value = false;
  }
}
</script>

<template>
  <main
    class="mx-auto w-full max-w-[26rem] px-6 py-16"
    data-testid="register-page"
  >
    <h1
      class="border-b pb-4 text-xl font-semibold"
      data-page-title
    >
      {{ copy.createAccount }}
    </h1>
    <p class="mt-4 text-base text-muted-foreground">
      {{ copy.register.lead }}
    </p>
    <StatusBanner
      v-if="errorMessage"
      ref="errorSummary"
      :focus-on-mount="true"
      class="mt-6"
      kind="error"
      testid="register-error"
    >
      {{ copy.messages[errorMessage] }}
    </StatusBanner>
    <StatusBanner
      v-if="success"
      :focus-on-mount="true"
      class="mt-6"
      kind="success"
      testid="register-success"
    >
      {{ copy.register.success }}
    </StatusBanner>
    <p
      v-if="success"
      class="mt-6 text-sm"
    >
      <NuxtLink
        class="text-primary underline-offset-4 hover:underline"
        data-testid="register-success-sign-in"
        to="/login"
      >
        {{ copy.signIn }}
      </NuxtLink>
      {{ copy.register.afterVerify }}
    </p>
    <div
      v-if="success"
      class="mt-6 grid gap-3 border-t border-border pt-6 text-sm
        text-muted-foreground"
      data-testid="register-no-email"
    >
      <p>{{ copy.register.noEmail }}</p>
      <template v-if="googleEnabled">
        <p data-testid="register-no-email-google">
          {{ copy.register.noEmailGoogle }}
        </p>
        <ProviderButtons
          :locale="locale"
          :providers="['google']"
        />
      </template>
    </div>
    <form
      v-if="!success"
      class="mt-8 grid gap-6"
      data-testid="register-form"
      novalidate
      @submit.prevent="onSubmit"
    >
      <FormField
        id="register-name"
        v-slot="{ id, describedBy, invalid }"
        :label="copy.name"
      >
        <Input
          :id="id"
          v-model="name"
          :aria-describedby="describedBy"
          :aria-invalid="invalid"
          autocomplete="name"
          type="text"
        />
      </FormField>
      <FormField
        id="register-email"
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
        id="register-password"
        ref="passwordField"
        v-model="password"
        autocomplete="new-password"
        confirm
        :label="copy.password"
        :locale="locale"
      />
      <Button
        class="h-9 w-full"
        :disabled="pending"
        type="submit"
      >
        {{ pending ? copy.register.pending : copy.createAccount }}
      </Button>
    </form>
    <template v-if="!success && loginProviders.length > 0">
      <div
        class="mt-8 flex items-center gap-3 text-xs text-muted-foreground"
        data-testid="register-divider"
      >
        <Separator class="flex-1" />
        {{ copy.or }}
        <Separator class="flex-1" />
      </div>
      <ProviderButtons
        class="mt-4"
        :locale="locale"
        :providers="loginProviders"
      />
    </template>
    <nav
      v-if="!success"
      class="mt-6 flex justify-between gap-3 text-sm"
    >
      <span>{{ copy.register.haveAccount }}</span>
      <NuxtLink
        class="text-primary underline-offset-4 hover:underline"
        to="/login"
      >
        {{ copy.signIn }}
      </NuxtLink>
    </nav>
  </main>
</template>
