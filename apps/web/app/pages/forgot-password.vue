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
import FormField from '@/components/app/FormField.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { type AuthMessage, authCopy } from '@/i18n/auth';
import { pageTitle } from '@/i18n/meta';

const { locale } = useLocale();
const copy = computed(() => authCopy[locale.value]);
useHead(computed(() => ({ title: pageTitle(copy.value.forgot.title) })));

const email = ref('');
const pending = ref(false);
const errorMessage = ref<AuthMessage | null>(null);
const success = ref(false);

async function onSubmit() {
  if (!email.value) {
    errorMessage.value = 'enterEmail';
    return;
  }
  pending.value = true;
  errorMessage.value = null;
  try {
    await usePasswordAuth().forgot(email.value);
    success.value = true;
  } catch {
    errorMessage.value = 'resetRequested';
  } finally {
    pending.value = false;
  }
}
</script>

<template>
  <main
    class="mx-auto w-full max-w-[26rem] px-6 py-16"
    data-testid="forgot-password-page"
  >
    <h1
      class="border-b pb-4 text-xl font-semibold"
      data-page-title
    >
      {{ copy.forgot.title }}
    </h1>
    <p class="mt-4 text-base text-muted-foreground">
      {{ copy.forgot.lead }}
    </p>
    <StatusBanner
      v-if="errorMessage"
      class="mt-6"
      kind="error"
      testid="forgot-error"
    >
      {{ copy.messages[errorMessage] }}
    </StatusBanner>
    <StatusBanner
      v-if="success"
      class="mt-6"
      kind="success"
      testid="forgot-success"
    >
      {{ copy.messages.resetRequested }}
    </StatusBanner>
    <form
      v-else
      class="mt-8 grid gap-6"
      data-testid="forgot-form"
      novalidate
      @submit.prevent="onSubmit"
    >
      <FormField
        id="forgot-email"
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
      <Button
        class="h-9 w-full"
        :disabled="pending"
        type="submit"
      >
        {{ pending ? copy.forgot.pending : copy.forgot.submit }}
      </Button>
    </form>
    <nav class="mt-6 flex justify-between gap-3 text-sm">
      <NuxtLink
        class="text-primary underline-offset-4 hover:underline"
        to="/login"
      >
        {{ copy.forgot.backToSignIn }}
      </NuxtLink>
    </nav>
  </main>
</template>
