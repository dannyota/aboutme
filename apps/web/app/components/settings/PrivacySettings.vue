<script setup lang="ts">
import ConfirmDialog from '../app/ConfirmDialog.vue';
import StatusBanner from '../app/StatusBanner.vue';
import PasswordField from '../auth/PasswordField.vue';
import { Button } from '../ui/button';
import type { AuthProvider } from '../../composables/useAuth';
import {
  PrivacySettingsActionsKey,
  PrivacySettingsFailure,
} from '../../composables/privacySettings';

const props = defineProps<{
  hasPassword: boolean;
  providers: AuthProvider[];
}>();

const emit = defineEmits<{ deleted: [] }>();
const actions = inject(PrivacySettingsActionsKey, null);

type DeleteMode = 'idle' | 'confirm' | 'reauth-password' | 'reauth-provider';

const deleteMode = ref<DeleteMode>('idle');
const deletePending = ref(false);
const exportPending = ref(false);
const errorMessage = ref<string | null>(null);
const exportError = ref<string | null>(null);
const currentPassword = ref('');
const deletionDisclosure = [
  'Access ends immediately.',
  'Private-media removal targets 24 hours.',
  'Backup copies expire on the 30-day schedule.',
].join(' ');

function openDelete(): void {
  errorMessage.value = null;
  deleteMode.value = 'confirm';
}

function cancelDelete(): void {
  if (deletePending.value) return;
  deleteMode.value = 'idle';
  currentPassword.value = '';
}

async function exportAccount(): Promise<void> {
  if (!actions || exportPending.value) return;
  exportPending.value = true;
  exportError.value = null;
  try {
    const result = await actions.exportAccount();
    if (result.kind === 'error') exportError.value = result.message;
  } catch {
    exportError.value = 'Could not export your data. Try again.';
  } finally {
    exportPending.value = false;
  }
}

async function confirmDelete(): Promise<void> {
  if (!actions || deletePending.value) return;
  deletePending.value = true;
  errorMessage.value = null;
  try {
    await actions.deleteAccount();
    deleteMode.value = 'idle';
    emit('deleted');
  } catch (error) {
    const failure = asPrivacyFailure(error);
    if (failure.kind === 'reauth-required') {
      currentPassword.value = '';
      deleteMode.value = props.hasPassword
        ? 'reauth-password'
        : 'reauth-provider';
      return;
    }
    deleteMode.value = 'idle';
    errorMessage.value = deletionCopy(failure);
  } finally {
    deletePending.value = false;
  }
}

async function submitPasswordReauth(): Promise<void> {
  if (!actions || deletePending.value) return;
  if (!currentPassword.value) {
    errorMessage.value = 'Enter your current password.';
    return;
  }
  deletePending.value = true;
  errorMessage.value = null;
  try {
    await actions.reauthenticate(currentPassword.value);
    currentPassword.value = '';
    deleteMode.value = 'confirm';
  } catch (error) {
    errorMessage.value = reauthCopy(asPrivacyFailure(error));
  } finally {
    deletePending.value = false;
  }
}

async function startProviderReauth(provider: AuthProvider): Promise<void> {
  if (!actions || deletePending.value) return;
  deletePending.value = true;
  errorMessage.value = null;
  try {
    await actions.startProviderReauth(provider);
  } catch (error) {
    errorMessage.value = reauthCopy(asPrivacyFailure(error));
  } finally {
    deletePending.value = false;
  }
}

function asPrivacyFailure(error: unknown): PrivacySettingsFailure {
  if (error instanceof PrivacySettingsFailure) return error;
  const kind = (error as { kind?: unknown } | null)?.kind;
  if (kind === 'reauth-failed' || kind === 'rate-limited') {
    return new PrivacySettingsFailure(kind);
  }
  return new PrivacySettingsFailure('unavailable');
}

function deletionCopy(failure: PrivacySettingsFailure): string {
  switch (failure.kind) {
    case 'session-required':
      return 'Your session ended. Sign in again.';
    case 'account-changed':
      return 'Your account changed. Review it and try again.';
    case 'rate-limited':
      return 'Too many attempts. Try again later.';
    case 'reauth-required':
    case 'reauth-failed':
    case 'unavailable':
      return 'Account deletion is temporarily unavailable. Try again.';
  }
}

function reauthCopy(failure: PrivacySettingsFailure): string {
  switch (failure.kind) {
    case 'reauth-failed':
      return 'Incorrect password.';
    case 'rate-limited':
      return 'Too many attempts. Try again later.';
    case 'session-required':
      return 'Your session ended. Sign in again.';
    case 'reauth-required':
    case 'account-changed':
    case 'unavailable':
      return 'Something went wrong. Please try again.';
  }
}
</script>

<template>
  <section
    aria-labelledby="privacy-title"
    class="grid gap-4"
    data-testid="privacy-settings"
  >
    <h2
      id="privacy-title"
      class="text-lg font-semibold"
    >
      Privacy
    </h2>
    <div class="grid gap-2">
      <h3 class="font-medium">
        Download your data
      </h3>
      <p class="text-sm text-muted-foreground">
        Download a JSON copy of your account and resumes.
      </p>
      <StatusBanner
        v-if="exportError"
        kind="error"
        testid="account-export-error"
      >
        {{ exportError }}
      </StatusBanner>
      <Button
        data-testid="account-export-action"
        :disabled="exportPending || !actions"
        type="button"
        variant="outline"
        @click="exportAccount"
      >
        {{ exportPending ? "Preparing download…" : "Download your data" }}
      </Button>
    </div>

    <div class="grid gap-2 border-t pt-6">
      <h3 class="font-medium text-destructive">
        Delete account
      </h3>
      <p class="text-sm text-muted-foreground">
        This permanently deletes your account and resumes.
      </p>
      <StatusBanner
        v-if="errorMessage"
        kind="error"
        testid="account-delete-error"
        focus-on-mount
      >
        {{ errorMessage }}
      </StatusBanner>
      <Button
        data-testid="account-delete-action"
        :disabled="!actions"
        type="button"
        variant="destructive"
        @click="openDelete"
      >
        Delete my account
      </Button>
    </div>

    <form
      v-if="deleteMode === 'reauth-password'"
      data-testid="account-delete-reauth-password"
      class="grid gap-4"
      novalidate
      @submit.prevent="submitPasswordReauth"
    >
      <p>Sign in again to confirm it’s you before deleting your account.</p>
      <PasswordField
        id="account-delete-current-password"
        v-model="currentPassword"
        label="Current password"
        autocomplete="current-password"
      />
      <div class="flex gap-2">
        <Button
          :disabled="deletePending"
          type="submit"
          variant="secondary"
        >
          {{ deletePending ? "Checking…" : "Continue" }}
        </Button>
        <Button
          :disabled="deletePending"
          type="button"
          variant="ghost"
          @click="cancelDelete"
        >
          Cancel
        </Button>
      </div>
    </form>

    <div
      v-else-if="deleteMode === 'reauth-provider'"
      class="grid gap-3"
      data-testid="account-delete-reauth-provider"
    >
      <p>Sign in again with your provider, then confirm deletion again.</p>
      <div class="flex flex-wrap gap-2">
        <Button
          v-for="provider in providers"
          :key="provider"
          :disabled="deletePending"
          type="button"
          @click="startProviderReauth(provider)"
        >
          Continue with {{ provider }}
        </Button>
        <Button
          :disabled="deletePending"
          type="button"
          variant="ghost"
          @click="cancelDelete"
        >
          Cancel
        </Button>
      </div>
    </div>

    <ConfirmDialog
      :busy="deletePending"
      cancel-action="cancel-account-delete"
      cancel-label="Cancel"
      class="max-w-md"
      confirm-action="confirm-account-delete"
      confirm-label="Delete account"
      confirm-text="DELETE"
      confirm-input-label="Type DELETE to permanently delete your account"
      destructive
      :open="deleteMode === 'confirm'"
      title="Delete your account?"
      :description="deletionDisclosure"
      @cancel="cancelDelete"
      @confirm="confirmDelete"
    />
  </section>
</template>
