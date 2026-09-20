<script setup lang="ts">
import ConfirmDialog from '../app/ConfirmDialog.vue';
import LocaleToggle from '../app/LocaleToggle.vue';
import StatusBanner from '../app/StatusBanner.vue';
import PasswordField from '../auth/PasswordField.vue';
import { Button } from '../ui/button';
import type { AuthProvider } from '../../composables/useAuth';
import {
  type AccountExportErrorKind,
  PrivacySettingsActionsKey,
  PrivacySettingsFailure,
} from '../../composables/privacySettings';
import {
  privacySettingsCopy,
  type PrivacySettingsReauthErrorKind,
} from '../../i18n/privacy-settings';
import { providerNames } from '../../composables/useCapabilities';

const props = defineProps<{
  hasPassword: boolean;
  providers: AuthProvider[];
}>();

const emit = defineEmits<{ deleted: [] }>();
const actions = inject(PrivacySettingsActionsKey, null);
const locale = useRouteLocale();
const copy = computed(() => privacySettingsCopy[locale.value]);

type DeleteMode = 'idle' | 'confirm' | 'reauth-password' | 'reauth-provider';

const deleteMode = ref<DeleteMode>('idle');
const deletePending = ref(false);
const exportPending = ref(false);
const errorKind = ref<PrivacySettingsReauthErrorKind | null>(null);
const exportErrorKind = ref<AccountExportErrorKind | null>(null);
const currentPassword = ref('');

function openDelete(): void {
  errorKind.value = null;
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
  exportErrorKind.value = null;
  try {
    const result = await actions.exportAccount();
    if (result.kind === 'error') exportErrorKind.value = result.error;
  } catch {
    exportErrorKind.value = 'unavailable';
  } finally {
    exportPending.value = false;
  }
}

async function confirmDelete(): Promise<void> {
  if (!actions || deletePending.value) return;
  deletePending.value = true;
  errorKind.value = null;
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
    errorKind.value = failure.kind;
  } finally {
    deletePending.value = false;
  }
}

async function submitPasswordReauth(): Promise<void> {
  if (!actions || deletePending.value) return;
  if (!currentPassword.value) {
    errorKind.value = 'current-password-required';
    return;
  }
  deletePending.value = true;
  errorKind.value = null;
  try {
    await actions.reauthenticate(currentPassword.value);
    currentPassword.value = '';
    deleteMode.value = 'confirm';
  } catch (error) {
    errorKind.value = asPrivacyFailure(error).kind;
  } finally {
    deletePending.value = false;
  }
}

async function startProviderReauth(provider: AuthProvider): Promise<void> {
  if (!actions || deletePending.value) return;
  deletePending.value = true;
  errorKind.value = null;
  try {
    await actions.startProviderReauth(provider);
  } catch (error) {
    errorKind.value = asPrivacyFailure(error).kind;
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
      {{ copy.title }}
    </h2>
    <div class="grid gap-2">
      <h3 class="font-medium">
        {{ copy.exportTitle }}
      </h3>
      <p class="text-sm text-muted-foreground">
        {{ copy.exportDescription }}
      </p>
      <StatusBanner
        v-if="exportErrorKind"
        kind="error"
        testid="account-export-error"
      >
        {{ copy.exportError(exportErrorKind) }}
      </StatusBanner>
      <Button
        data-testid="account-export-action"
        :disabled="exportPending || !actions"
        type="button"
        variant="outline"
        @click="exportAccount"
      >
        {{ exportPending ? copy.exportPending : copy.exportAction }}
      </Button>
    </div>

    <div class="grid gap-2 border-t pt-6">
      <h3 class="font-medium text-destructive">
        {{ copy.deleteTitle }}
      </h3>
      <p class="text-sm text-muted-foreground">
        {{ copy.deleteDescription }}
      </p>
      <StatusBanner
        v-if="errorKind"
        kind="error"
        testid="account-delete-error"
        focus-on-mount
      >
        {{
          deleteMode === 'idle'
            ? copy.deleteError(errorKind)
            : copy.reauthError(errorKind)
        }}
      </StatusBanner>
      <Button
        data-testid="account-delete-action"
        :disabled="!actions"
        type="button"
        variant="destructive"
        @click="openDelete"
      >
        {{ copy.deleteAction }}
      </Button>
    </div>

    <form
      v-if="deleteMode === 'reauth-password'"
      data-testid="account-delete-reauth-password"
      class="grid gap-4"
      novalidate
      @submit.prevent="submitPasswordReauth"
    >
      <p>{{ copy.passwordReauthDescription }}</p>
      <PasswordField
        id="account-delete-current-password"
        v-model="currentPassword"
        :label="copy.currentPassword"
        :locale="locale"
        autocomplete="current-password"
      />
      <div class="flex gap-2">
        <Button
          :disabled="deletePending"
          type="submit"
          variant="secondary"
        >
          {{ deletePending ? copy.checking : copy.continue }}
        </Button>
        <Button
          :disabled="deletePending"
          type="button"
          variant="ghost"
          @click="cancelDelete"
        >
          {{ copy.cancel }}
        </Button>
      </div>
    </form>

    <div
      v-else-if="deleteMode === 'reauth-provider'"
      class="grid gap-3"
      data-testid="account-delete-reauth-provider"
    >
      <p>{{ copy.providerReauthDescription }}</p>
      <div class="flex flex-wrap gap-2">
        <Button
          v-for="provider in providers"
          :key="provider"
          :disabled="deletePending"
          type="button"
          @click="startProviderReauth(provider)"
        >
          {{ copy.providerContinue(providerNames[provider]) }}
        </Button>
        <Button
          :disabled="deletePending"
          type="button"
          variant="ghost"
          @click="cancelDelete"
        >
          {{ copy.cancel }}
        </Button>
      </div>
    </div>

    <ConfirmDialog
      :busy="deletePending"
      cancel-action="cancel-account-delete"
      :cancel-label="copy.cancel"
      class="max-w-md"
      confirm-action="confirm-account-delete"
      :confirm-label="copy.deleteTitle"
      confirm-text="DELETE"
      :confirm-input-label="copy.deleteConfirmInput"
      destructive
      :open="deleteMode === 'confirm'"
      :title="copy.deleteDialogTitle"
      :description="copy.deletionDisclosure"
      @cancel="cancelDelete"
      @confirm="confirmDelete"
    >
      <template #header-actions>
        <LocaleToggle
          :label="copy.localeLabel"
          @pointerdown.prevent
        />
      </template>
    </ConfirmDialog>
  </section>
</template>
