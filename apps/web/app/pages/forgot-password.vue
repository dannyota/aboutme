<script setup lang="ts">
/**
 * Password recovery page.
 *
 * The server always answers `202` and only sends a reset email when a
 * password account exists. Every outcome — success or rejection — renders the
 * same fixed, account-neutral copy, so this page never reveals whether an
 * email is registered.
 */
import { usePasswordAuth } from '../composables/usePasswordAuth';
import AuthCard from '@/components/auth/AuthCard.vue';
import FormField from '@/components/app/FormField.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

const GENERIC_COPY
  = 'If an account exists for this email, we\'ve sent a password reset link.';

const email = ref('');
const pending = ref(false);
const errorMessage = ref<string | null>(null);
const success = ref(false);

async function onSubmit() {
  if (!email.value) {
    errorMessage.value = 'Enter your email address.';
    return;
  }
  pending.value = true;
  errorMessage.value = null;
  try {
    await usePasswordAuth().forgot(email.value);
    success.value = true;
  } catch {
    errorMessage.value = GENERIC_COPY;
  } finally {
    pending.value = false;
  }
}
</script>

<template>
  <AuthCard
    description="Enter your email to receive a reset link if an account exists."
    title="Forgot password"
  >
    <StatusBanner
      v-if="errorMessage"
      kind="error"
      testid="forgot-error"
    >
      {{ errorMessage }}
    </StatusBanner>
    <StatusBanner
      v-if="success"
      kind="success"
      testid="forgot-success"
    >
      {{ GENERIC_COPY }}
    </StatusBanner>
    <form
      v-else
      class="grid gap-4"
      data-testid="forgot-form"
      novalidate
      @submit.prevent="onSubmit"
    >
      <FormField
        id="forgot-email"
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
      <Button
        class="mt-1 w-full"
        :disabled="pending"
        type="submit"
      >
        {{ pending ? "Sending…" : "Send reset link" }}
      </Button>
    </form>
    <template #footer>
      <NuxtLink
        class="text-primary underline-offset-4 hover:underline"
        to="/login"
      >
        Back to sign in
      </NuxtLink>
    </template>
  </AuthCard>
</template>
