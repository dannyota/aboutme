<script setup lang="ts">
/**
 * Registration page: name/email/password with component-local confirmation.
 *
 * The server is authoritative for password policy; the client checks only
 * required fields and local confirmation, then maps the closed policy issues
 * to fixed copy. The 202 success copy is fixed and reveals nothing about
 * email ownership.
 */
import AuthCard from '@/components/auth/AuthCard.vue';
import FormField from '@/components/app/FormField.vue';
import PasswordField from '@/components/auth/PasswordField.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  type PasswordAuthFailure,
  type PasswordIssue,
  usePasswordAuth,
} from '../composables/usePasswordAuth';

const name = ref('');
const email = ref('');
const password = ref('');
const passwordField = ref<InstanceType<typeof PasswordField> | null>(null);
const pending = ref(false);
const errorMessage = ref<string | null>(null);
const success = ref(false);
const errorSummary = ref<HTMLElement | null>(null);

const PASSWORD_ISSUE_COPY: Record<PasswordIssue, string> = {
  length: 'Password must be at least 12 characters.',
  common: 'That password is too common. Choose a different one.',
  breached:
    'That password was exposed in a data breach. Choose a different one.',
};

function copyFor(failure: PasswordAuthFailure): string {
  switch (failure.kind) {
    case 'password-invalid':
      return failure.issue
        ? PASSWORD_ISSUE_COPY[failure.issue]
        : 'Password does not meet our requirements.';
    case 'rate-limited':
      return 'Too many attempts. Try again later.';
    case 'unavailable':
      return 'Something went wrong. Please try again.';
    default:
      return 'Check your details and try again.';
  }
}

async function onSubmit() {
  if (!name.value || !email.value || !password.value) {
    errorMessage.value = 'Please fill in all fields.';
    return;
  }
  if (passwordField.value?.confirmMismatch) {
    errorMessage.value = 'Passwords do not match.';
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
    errorMessage.value = copyFor(failure as PasswordAuthFailure);
    await nextTick();
    errorSummary.value?.focus();
  } finally {
    pending.value = false;
  }
}
</script>

<template>
  <AuthCard
    description="Create an account to build and publish your resumes."
    title="Create account"
  >
    <StatusBanner
      v-if="errorMessage"
      ref="errorSummary"
      :focus-on-mount="true"
      kind="error"
      testid="register-error"
    >
      {{ errorMessage }}
    </StatusBanner>
    <StatusBanner
      v-if="success"
      kind="success"
      testid="register-success"
    >
      Check your email to verify your address.
    </StatusBanner>
    <form
      class="grid gap-4"
      data-testid="register-form"
      novalidate
      @submit.prevent="onSubmit"
    >
      <FormField
        id="register-name"
        v-slot="{ id, describedBy, invalid }"
        label="Name"
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
        id="register-password"
        ref="passwordField"
        v-model="password"
        autocomplete="new-password"
        confirm
        label="Password"
      />
      <Button
        class="mt-1 w-full"
        :disabled="pending"
        type="submit"
      >
        {{ pending ? "Creating account…" : "Create account" }}
      </Button>
    </form>
    <template #footer>
      <span>Already have an account?</span>
      <NuxtLink
        class="text-primary underline-offset-4 hover:underline"
        to="/login"
      >
        Sign in
      </NuxtLink>
    </template>
  </AuthCard>
</template>
