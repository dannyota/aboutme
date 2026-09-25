<script setup lang="ts">
/**
 * `SecondFactorSettings`: passkey, authenticator-app (`TotpSettings`), and
 * recovery-code controls for `/app/settings/sessions`
 * (`docs/design/second-factor-authentication.md#enrollment-and-management`).
 *
 * Presentational: it receives `enrollmentOpen`, `hasPassword`, and
 * `providers` as props, reads factor state through `useSecondFactorState`,
 * and performs every side effect through
 * `SecondFactorSettingsActionsKey` (see `composables/secondFactorSettings`).
 * Factor state (passkeys, recovery-code count) is read regardless of
 * `enrollmentOpen`; only starting a new registration is gated by it. Recovery
 * codes live in `revealedCodes` only between a successful enrollment or
 * regeneration and the reveal closing, navigating away, or this component
 * unmounting — never in browser storage.
 */
import { computed, inject, onBeforeUnmount, onMounted, ref } from 'vue';
import ConfirmDialog from '../app/ConfirmDialog.vue';
import EmptyState from '../app/EmptyState.vue';
import LoadingState from '../app/LoadingState.vue';
import StatusBanner from '../app/StatusBanner.vue';
import ReauthPrompt from './ReauthPrompt.vue';
import TotpSettings from './TotpSettings.vue';
import { Button } from '../ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog';
import type { AuthProvider } from '../../composables/useAuth';
import { PasswordSettingsFailure } from '../../composables/passwordSettings';
import {
  type SecondFactorSettingsFailure,
  createPasskeyCredential,
  downloadRecoveryCodes,
  mapPasskeyCompletionError,
  mapPasskeyOptionsError,
  mapPasskeyRemovalError,
  mapRecoveryRegenerationError,
  SecondFactorSettingsActionsKey,
  useSecondFactorState,
  type SecondFactorPasskey,
} from '../../composables/secondFactorSettings';
import { providerNames } from '../../composables/useCapabilities';
import {
  isWebAuthnCancellation,
  isWebAuthnSupported,
} from '../../utils/webauthn';
import {
  secondFactorSettingsCopy,
  type SecondFactorSettingsMessage,
} from '../../i18n/second-factor-settings';

const props = defineProps<{
  enrollmentOpen: boolean;
  hasPassword: boolean;
  providers: AuthProvider[];
  /** Page-level copy: factor changes end other sessions and agent access. */
  sessionsNotice: string;
}>();

const emit = defineEmits<{ changed: [] }>();

const actions = inject(SecondFactorSettingsActionsKey, null);
const { locale } = useLocale();
const copy = computed(() => secondFactorSettingsCopy[locale.value]);

const {
  enabled, passkeys, totpEnabled, recoveryCodesRemaining, resolved, refresh,
} = useSecondFactorState();

const webAuthnSupported = ref(false);
onMounted(() => {
  webAuthnSupported.value = isWebAuthnSupported();
});

type ReauthMode = 'password' | 'provider';

const reauthMode = ref<ReauthMode | null>(null);
const currentPassword = ref('');
const reauthPending = ref(false);
const reauthErrorKind = ref<
  'current-password-required' | 'reauth-failed' | 'rate-limited'
  | 'unavailable' | null
>(null);

const addPending = ref(false);
const addError = ref<SecondFactorSettingsMessage | null>(null);
const addedNotice = ref(false);

const removeTarget = ref<SecondFactorPasskey | null>(null);
const removePending = ref(false);
const removeError = ref<SecondFactorSettingsMessage | null>(null);
const removedNotice = ref(false);
// Matches the server's shared active-factor count (totp-second-factor
// -contract.md "Removal, recovery, and races"): passkeys plus TOTP.
const removeIsFinal = computed(
  () => removeTarget.value !== null && passkeys.value.length <= 1
    && !totpEnabled.value,
);

const regenerateConfirmOpen = ref(false);
const regeneratePending = ref(false);
const regenerateError = ref<SecondFactorSettingsMessage | null>(null);

const revealedCodes = ref<readonly string[] | null>(null);
const revealKind = ref<'enrolled' | 'regenerated'>('enrolled');
const revealTitle = computed(() =>
  revealKind.value === 'enrolled'
    ? copy.value.revealEnrolledTitle
    : copy.value.revealRegeneratedTitle);
const copiedNotice = ref(false);

function clearReveal(): void {
  revealedCodes.value = null;
  copiedNotice.value = false;
}

onBeforeUnmount(clearReveal);

/**
 * `reauth_required` interrupts the in-progress action: close any open
 * confirmation and switch to the shared reauth block. Every other kind maps
 * straight to fixed copy. Mirrors `PasswordSettings`/`PrivacySettings`: after
 * reauth the person retries the original action rather than an automatic
 * replay.
 */
function handleFailure(
  failure: SecondFactorSettingsFailure,
): SecondFactorSettingsMessage | null {
  if (failure.kind === 'reauth-required') {
    removeTarget.value = null;
    regenerateConfirmOpen.value = false;
    currentPassword.value = '';
    reauthErrorKind.value = null;
    reauthMode.value = props.hasPassword ? 'password' : 'provider';
    return null;
  }
  return failure.kind;
}

/** Best effort: the mutation already succeeded, so a stale refresh here must
 * never undo the completed change or block the one-time reveal. */
async function afterMutationSuccess(): Promise<void> {
  try {
    await refresh();
  } catch {
    // Ignored; the next visit re-reads current state.
  }
  emit('changed');
}

function totpReauthRequired(): void {
  reauthMode.value = props.hasPassword ? 'password' : 'provider';
}

async function onTotpChanged(codes: readonly string[] | null): Promise<void> {
  if (codes) revealKind.value = 'enrolled';
  if (codes) revealedCodes.value = codes;
  await afterMutationSuccess();
}

async function startAddPasskey(): Promise<void> {
  if (!actions || addPending.value) return;
  addPending.value = true;
  addError.value = null;
  addedNotice.value = false;
  let options;
  try {
    options = await actions.registrationOptions();
  } catch (error) {
    addError.value = handleFailure(mapPasskeyOptionsError(error));
    addPending.value = false;
    return;
  }
  let credential;
  try {
    credential = await createPasskeyCredential(options.publicKey);
  } catch (ceremonyError) {
    addPending.value = false;
    addError.value = isWebAuthnCancellation(ceremonyError)
      ? 'cancelled'
      : 'unavailable';
    return;
  }
  try {
    const result = await actions.completeRegistration(
      options.ceremonyId,
      credential,
    );
    if (result.recoveryCodes) {
      revealKind.value = 'enrolled';
      revealedCodes.value = result.recoveryCodes;
    } else {
      addedNotice.value = true;
    }
    await afterMutationSuccess();
  } catch (error) {
    addError.value = handleFailure(mapPasskeyCompletionError(error));
  } finally {
    addPending.value = false;
  }
}

function requestRemove(passkey: SecondFactorPasskey): void {
  removeError.value = null;
  removedNotice.value = false;
  removeTarget.value = passkey;
}

function cancelRemove(): void {
  if (removePending.value) return;
  removeTarget.value = null;
}

async function confirmRemove(): Promise<void> {
  if (!actions || removePending.value || removeTarget.value === null) return;
  removePending.value = true;
  const target = removeTarget.value;
  try {
    await actions.removePasskey(target.id);
    removeTarget.value = null;
    clearReveal();
    removedNotice.value = true;
    await afterMutationSuccess();
  } catch (error) {
    removeError.value = handleFailure(mapPasskeyRemovalError(error));
    removeTarget.value = null;
  } finally {
    removePending.value = false;
  }
}

function openRegenerate(): void {
  regenerateError.value = null;
  regenerateConfirmOpen.value = true;
}

function cancelRegenerate(): void {
  if (regeneratePending.value) return;
  regenerateConfirmOpen.value = false;
}

async function confirmRegenerate(): Promise<void> {
  if (!actions || regeneratePending.value) return;
  regeneratePending.value = true;
  try {
    const codes = await actions.regenerateRecoveryCodes();
    regenerateConfirmOpen.value = false;
    revealKind.value = 'regenerated';
    revealedCodes.value = codes;
    await afterMutationSuccess();
  } catch (error) {
    regenerateError.value = handleFailure(mapRecoveryRegenerationError(error));
    regenerateConfirmOpen.value = false;
  } finally {
    regeneratePending.value = false;
  }
}

function onRevealOpenChange(open: boolean): void {
  if (!open) clearReveal();
}

function closeReveal(): void {
  clearReveal();
}

function copyCodes(): void {
  const codes = revealedCodes.value;
  if (codes === null || !navigator.clipboard) return;
  copiedNotice.value = false;
  navigator.clipboard.writeText(codes.join('\n'))
    .then(() => {
      copiedNotice.value = true;
    })
    .catch(() => undefined);
}

function downloadCodes(): void {
  if (revealedCodes.value === null) return;
  downloadRecoveryCodes(revealedCodes.value, locale.value);
}

function reauthFailureKind(
  error: unknown,
): 'reauth-failed' | 'rate-limited' | 'unavailable' {
  if (error instanceof PasswordSettingsFailure) {
    if (error.kind === 'reauth-failed') return 'reauth-failed';
    if (error.kind === 'rate-limited') return 'rate-limited';
  }
  return 'unavailable';
}

function cancelReauth(): void {
  if (reauthPending.value) return;
  reauthMode.value = null;
  currentPassword.value = '';
  reauthErrorKind.value = null;
}

async function submitReauthPassword(): Promise<void> {
  if (!actions || reauthPending.value) return;
  if (!currentPassword.value) {
    reauthErrorKind.value = 'current-password-required';
    return;
  }
  reauthPending.value = true;
  reauthErrorKind.value = null;
  try {
    await actions.reauthenticate(currentPassword.value);
    currentPassword.value = '';
    reauthMode.value = null;
  } catch (error) {
    reauthErrorKind.value = reauthFailureKind(error);
  } finally {
    reauthPending.value = false;
  }
}

async function submitReauthProvider(provider: AuthProvider): Promise<void> {
  if (!actions || reauthPending.value) return;
  reauthPending.value = true;
  reauthErrorKind.value = null;
  try {
    await actions.startProviderReauth(provider);
  } catch (error) {
    reauthErrorKind.value = reauthFailureKind(error);
  } finally {
    reauthPending.value = false;
  }
}

function formatTimestamp(value: string): string {
  return new Intl.DateTimeFormat(locale.value === 'vi' ? 'vi-VN' : 'en-US', {
    timeZone: 'UTC',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  }).format(new Date(value));
}
</script>

<template>
  <div
    data-testid="second-factor-settings"
    class="grid gap-4"
  >
    <h2
      id="second-factor-title"
      class="text-lg font-semibold"
    >
      {{ copy.title }}
    </h2>
    <p class="text-muted-foreground text-sm">
      {{ copy.description }}
    </p>
    <p
      class="text-muted-foreground text-sm"
      data-testid="second-factor-sessions-notice"
    >
      {{ sessionsNotice }}
    </p>

    <LoadingState
      v-if="!resolved"
      :label="copy.title"
      testid="second-factor-loading"
    />

    <ReauthPrompt
      v-else-if="reauthMode !== null"
      v-model="currentPassword"
      :mode="reauthMode"
      :providers="providers"
      :provider-label="(provider) =>
        copy.continueWithProvider(providerNames[provider])"
      :pending="reauthPending"
      :locale="locale"
      password-id="second-factor-reauth-password"
      :labels="{
        currentPassword: copy.currentPassword,
        continue: copy.continueLabel,
        checking: copy.checking,
        cancel: copy.cancel,
      }"
      :password-description="copy.reauthPasswordDescription"
      :provider-description="copy.reauthProviderDescription"
      form-testid="second-factor-reauth-password"
      provider-testid="second-factor-reauth-provider"
      submit-testid="second-factor-reauth-submit"
      cancel-testid="second-factor-reauth-cancel"
      provider-button-testid="second-factor-reauth-provider-"
      @submit-password="submitReauthPassword"
      @provider="submitReauthProvider"
      @cancel="cancelReauth"
    >
      <StatusBanner
        v-if="reauthErrorKind"
        kind="error"
        testid="second-factor-reauth-error"
        focus-on-mount
      >
        {{ copy.errors[reauthErrorKind] }}
      </StatusBanner>
    </ReauthPrompt>

    <template v-else>
      <EmptyState
        v-if="passkeys.length === 0"
        data-testid="second-factor-empty"
        :title="copy.emptyTitle"
        :description="copy.emptyDescription"
      >
        <template #action>
          <Button
            v-if="enrollmentOpen && webAuthnSupported"
            data-testid="passkey-add"
            :disabled="!actions || addPending"
            type="button"
            @click="startAddPasskey"
          >
            {{ addPending ? copy.adding : copy.addPasskey }}
          </Button>
          <p
            v-else-if="enrollmentOpen"
            data-testid="second-factor-unsupported"
            class="text-muted-foreground text-sm"
          >
            {{ copy.unsupported }}
          </p>
        </template>
      </EmptyState>

      <div
        v-else
        class="grid gap-3"
      >
        <div
          v-for="passkey in passkeys"
          :key="passkey.id"
          :data-testid="`passkey-row-${passkey.id}`"
          :class="[
            'grid grid-cols-[1fr_auto] items-center gap-3 border-b py-3',
            'last:border-b-0',
          ]"
        >
          <div>
            <p class="text-sm">
              {{ copy.created }}
              <time :datetime="passkey.createdAt">{{
                formatTimestamp(passkey.createdAt)
              }}</time>
            </p>
            <p class="text-muted-foreground text-sm">
              <time
                v-if="passkey.lastUsedAt !== null"
                :datetime="passkey.lastUsedAt"
              >{{ copy.lastUsed }}
                {{ formatTimestamp(passkey.lastUsedAt) }}</time>
              <span v-else>{{ copy.neverUsed }}</span>
            </p>
          </div>
          <Button
            :data-testid="`passkey-remove-${passkey.id}`"
            :disabled="!actions || removePending"
            type="button"
            variant="secondary"
            @click="requestRemove(passkey)"
          >
            {{ copy.removePasskey }}
          </Button>
        </div>

        <Button
          v-if="enrollmentOpen && webAuthnSupported"
          data-testid="passkey-add"
          class="justify-self-start"
          :disabled="!actions || addPending"
          type="button"
          variant="outline"
          @click="startAddPasskey"
        >
          {{ addPending ? copy.adding : copy.addPasskey }}
        </Button>
        <p
          v-else-if="enrollmentOpen"
          data-testid="second-factor-unsupported"
          class="text-muted-foreground text-sm"
        >
          {{ copy.unsupported }}
        </p>
      </div>

      <StatusBanner
        v-if="addedNotice"
        kind="success"
        testid="passkey-added-success"
      >
        {{ copy.passkeyAdded }}
      </StatusBanner>
      <StatusBanner
        v-if="addError"
        kind="error"
        testid="passkey-add-error"
        focus-on-mount
      >
        {{ copy.errors[addError] }}
      </StatusBanner>
      <StatusBanner
        v-if="removedNotice"
        kind="success"
        testid="passkey-removed-success"
      >
        {{ copy.passkeyRemoved }}
      </StatusBanner>
      <StatusBanner
        v-if="removeError"
        kind="error"
        testid="passkey-remove-error"
        focus-on-mount
      >
        {{ copy.errors[removeError] }}
      </StatusBanner>
      <TotpSettings
        :has-other-active-factor="passkeys.length > 0"
        :totp-enabled="totpEnabled"
        @changed="onTotpChanged"
        @reauth-required="totpReauthRequired"
      />
      <div
        v-if="enabled"
        class="grid gap-2 border-t pt-4"
        data-testid="recovery-codes-section"
      >
        <h3 class="font-medium">
          {{ copy.recoveryTitle }}
        </h3>
        <p
          class="text-sm"
          data-testid="recovery-codes-remaining"
        >
          {{ copy.recoveryRemaining(recoveryCodesRemaining) }}
        </p>
        <StatusBanner
          v-if="regenerateError"
          kind="error"
          testid="recovery-regenerate-error"
          focus-on-mount
        >
          {{ copy.errors[regenerateError] }}
        </StatusBanner>
        <Button
          class="justify-self-start"
          data-testid="recovery-regenerate"
          :disabled="!actions || regeneratePending"
          type="button"
          variant="outline"
          @click="openRegenerate"
        >
          {{ copy.regenerate }}
        </Button>
      </div>
    </template>

    <ConfirmDialog
      :open="removeTarget !== null"
      :title="copy.removeTitle"
      :description="removeIsFinal ? copy.removeDescriptionFinal
        : copy.removeDescription"
      :confirm-label="copy.removeConfirm"
      :cancel-label="copy.cancel"
      destructive
      :busy="removePending"
      confirm-action="passkey-remove-confirm"
      cancel-action="passkey-remove-cancel"
      @confirm="confirmRemove"
      @cancel="cancelRemove"
    />

    <ConfirmDialog
      :open="regenerateConfirmOpen"
      :title="copy.regenerateTitle"
      :description="copy.regenerateDescription"
      :confirm-label="copy.regenerateConfirm"
      :cancel-label="copy.cancel"
      :busy="regeneratePending"
      confirm-action="recovery-regenerate-confirm"
      cancel-action="recovery-regenerate-cancel"
      @confirm="confirmRegenerate"
      @cancel="cancelRegenerate"
    />

    <Dialog
      :open="revealedCodes !== null"
      @update:open="onRevealOpenChange"
    >
      <DialogContent
        data-testid="recovery-reveal"
        :close-label="copy.close"
      >
        <DialogHeader>
          <DialogTitle>{{ revealTitle }}</DialogTitle>
          <DialogDescription>{{ copy.revealDescription }}</DialogDescription>
        </DialogHeader>
        <ol
          v-if="revealedCodes"
          class="grid gap-1 rounded-md border p-3 font-mono text-sm"
          data-testid="recovery-codes-list"
        >
          <li
            v-for="code in revealedCodes"
            :key="code"
          >
            {{ code }}
          </li>
        </ol>
        <StatusBanner
          v-if="copiedNotice"
          kind="success"
          testid="recovery-copied"
        >
          {{ copy.copied }}
        </StatusBanner>
        <DialogFooter>
          <Button
            data-testid="recovery-copy"
            type="button"
            variant="outline"
            @click="copyCodes"
          >
            {{ copy.copyCodes }}
          </Button>
          <Button
            data-testid="recovery-download"
            type="button"
            variant="outline"
            @click="downloadCodes"
          >
            {{ copy.downloadCodes }}
          </Button>
          <Button
            data-testid="recovery-reveal-close"
            type="button"
            @click="closeReveal"
          >
            {{ copy.closeReveal }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
