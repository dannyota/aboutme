<script setup lang="ts">
/**
 * `TotpSettings` — authenticator-app (TOTP) setup, replacement, and removal
 * for the account settings second-factor section
 * (`docs/design/totp-second-factor-contract.md`, ADR 0049).
 *
 * Mounted from `SecondFactorSettings.vue`, which owns the one shared
 * reauthentication flow for the whole second-factor section: a
 * `reauth-required` failure here is forwarded up (`reauth-required` event)
 * rather than handled locally, matching how a passkey or recovery-code
 * action already interrupts into that flow. `changed` carries the new
 * recovery-code set only when this completion created the account's first
 * active factor (never on replacement or when a passkey already enabled
 * enforcement) — see "Enrollment and replacement API".
 *
 * The provisioning secret, URI, and QR markup live only in this
 * component's memory and are cleared on dialog close, locale change while
 * open, completion, a setup-ending failure, and unmount ("Provisioning
 * data"): never a network request, URL, storage, log, or persisting
 * attribute.
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import ConfirmDialog from '../app/ConfirmDialog.vue';
import FormField from '../app/FormField.vue';
import StatusBanner from '../app/StatusBanner.vue';
import { Button } from '../ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog';
import { Input } from '../ui/input';
import {
  renderTotpQr,
  TotpSettingsFailure,
  useTotpSettingsActions,
  type TotpSettingsErrorKind,
} from '../../composables/totpSettings';
import { useCapabilities } from '../../composables/useCapabilities';
import { isWebAuthnSupported } from '../../utils/webauthn';
import { totpSettingsCopy } from '../../i18n/second-factor-settings';

const props = defineProps<{
  totpEnabled: boolean;
  /** Whether an active passkey also exists, for the removal warning. */
  hasOtherActiveFactor: boolean;
}>();

const emit = defineEmits<{
  changed: [recoveryCodes: readonly string[] | null];
  'reauth-required': [];
}>();

const actions = useTotpSettingsActions();
const { totpEnrollment } = useCapabilities();
const { locale } = useLocale();
const copy = computed(() => totpSettingsCopy[locale.value]);

const webAuthnSupported = ref(false);
onMounted(() => {
  webAuthnSupported.value = isWebAuthnSupported();
});

function failureKind(error: unknown): TotpSettingsErrorKind {
  return error instanceof TotpSettingsFailure ? error.kind : 'unavailable';
}

// --- setup (start, local QR, and proof) -----------------------------------

const setupOpen = ref(false);
const replacing = ref(false);
const enrollmentId = ref<string | null>(null);
const secret = ref<string | null>(null);
const provisioningUri = ref<string | null>(null);
const code = ref('');
const localFormatError = ref(false);
const secretCopied = ref(false);

const startPending = ref(false);
const startError = ref<TotpSettingsErrorKind | null>(null);
const completePending = ref(false);
const completeError = ref<TotpSettingsErrorKind | null>(null);

const qr = computed(() =>
  provisioningUri.value ? renderTotpQr(provisioningUri.value) : null);
const setupTitle = computed(() =>
  replacing.value ? copy.value.setupTitleReplace : copy.value.setupTitleNew);

const addedNotice = ref(false);
const replacedNotice = ref(false);
const removedNotice = ref(false);

/** The one place the secret, URI, QR markup, and code are discarded. */
function clearSetup(): void {
  enrollmentId.value = null;
  secret.value = null;
  provisioningUri.value = null;
  code.value = '';
  localFormatError.value = false;
  secretCopied.value = false;
  completeError.value = null;
  setupOpen.value = false;
}

onBeforeUnmount(clearSetup);

// A live locale change re-renders every string in the open dialog; treat
// it as a rebuild and drop the secret rather than let stale copy and live
// provisioning data mix.
watch(locale, () => {
  if (setupOpen.value) clearSetup();
});

async function startSetup(): Promise<void> {
  if (startPending.value || !totpEnrollment.value) return;
  startPending.value = true;
  startError.value = null;
  addedNotice.value = false;
  replacedNotice.value = false;
  try {
    const result = await actions.startEnrollment();
    replacing.value = props.totpEnabled;
    enrollmentId.value = result.enrollmentId;
    secret.value = result.secret;
    provisioningUri.value = result.provisioningUri;
    code.value = '';
    setupOpen.value = true;
  } catch (error) {
    const kind = failureKind(error);
    if (kind === 'reauth-required') {
      emit('reauth-required');
    } else {
      startError.value = kind;
    }
  } finally {
    startPending.value = false;
  }
}

function onSetupOpenChange(open: boolean): void {
  if (!open && !completePending.value) clearSetup();
}

function cancelSetup(): void {
  if (completePending.value) return;
  clearSetup();
}

async function submitCode(): Promise<void> {
  if (completePending.value) return;
  // A local const, not `enrollmentId.value` again below: TypeScript cannot
  // carry a null check on a mutable ref property across the intervening
  // calls and the `await`.
  const currentEnrollmentId = enrollmentId.value;
  if (currentEnrollmentId === null) return;
  if (!/^\d{6}$/.test(code.value)) {
    localFormatError.value = true;
    return;
  }
  localFormatError.value = false;
  completePending.value = true;
  completeError.value = null;
  try {
    const result = await actions.completeEnrollment(
      currentEnrollmentId,
      code.value,
    );
    const wasReplacing = replacing.value;
    clearSetup();
    if (wasReplacing) {
      replacedNotice.value = true;
    } else {
      addedNotice.value = true;
    }
    emit('changed', result.recoveryCodes ?? null);
  } catch (error) {
    const kind = failureKind(error);
    if (kind === 'reauth-required') {
      clearSetup();
      emit('reauth-required');
    } else if (kind === 'closed' || kind === 'expired') {
      // The proposed secret can no longer be proven; only a fresh start
      // can recover, so the whole dialog ends rather than staying open on
      // a QR that no longer matches any enrollment row.
      clearSetup();
      startError.value = kind;
    } else {
      completeError.value = kind;
    }
  } finally {
    completePending.value = false;
  }
}

function copySecret(): void {
  const currentSecret = secret.value;
  if (currentSecret === null || !navigator.clipboard) return;
  secretCopied.value = false;
  navigator.clipboard.writeText(currentSecret)
    .then(() => {
      secretCopied.value = true;
    })
    .catch(() => undefined);
}

// --- removal ---------------------------------------------------------------

const removeConfirmOpen = ref(false);
const removePending = ref(false);
const removeError = ref<TotpSettingsErrorKind | null>(null);

function requestRemove(): void {
  removeError.value = null;
  removedNotice.value = false;
  removeConfirmOpen.value = true;
}

function cancelRemove(): void {
  if (removePending.value) return;
  removeConfirmOpen.value = false;
}

async function confirmRemove(): Promise<void> {
  if (removePending.value) return;
  removePending.value = true;
  try {
    await actions.removeTotp();
    removeConfirmOpen.value = false;
    removedNotice.value = true;
    emit('changed', null);
  } catch (error) {
    const kind = failureKind(error);
    removeConfirmOpen.value = false;
    if (kind === 'reauth-required') {
      emit('reauth-required');
    } else {
      removeError.value = kind;
    }
  } finally {
    removePending.value = false;
  }
}
</script>

<template>
  <div
    class="grid gap-3 border-t pt-4"
    data-testid="totp-settings"
  >
    <h3 class="font-medium">
      {{ copy.title }}
    </h3>
    <p class="text-muted-foreground text-sm">
      {{ copy.description }}
    </p>
    <p
      v-if="webAuthnSupported"
      class="text-muted-foreground text-sm"
      data-testid="totp-passkey-recommendation"
    >
      {{ copy.passkeyRecommendation }}
    </p>

    <p
      class="text-sm"
      data-testid="totp-status"
    >
      {{ totpEnabled ? copy.statusEnabled : copy.statusNotSetUp }}
    </p>

    <div class="flex flex-wrap gap-2">
      <Button
        v-if="totpEnrollment && !totpEnabled"
        data-testid="totp-setup-start"
        :disabled="startPending"
        type="button"
        @click="startSetup"
      >
        {{ startPending ? copy.starting : copy.setUpButton }}
      </Button>
      <Button
        v-if="totpEnrollment && totpEnabled"
        data-testid="totp-setup-replace"
        :disabled="startPending"
        type="button"
        variant="outline"
        @click="startSetup"
      >
        {{ startPending ? copy.starting : copy.replaceButton }}
      </Button>
      <Button
        v-if="totpEnabled"
        data-testid="totp-remove"
        :disabled="removePending"
        type="button"
        variant="secondary"
        @click="requestRemove"
      >
        {{ copy.removeButton }}
      </Button>
    </div>

    <StatusBanner
      v-if="addedNotice"
      kind="success"
      testid="totp-added-success"
    >
      {{ copy.addedNotice }}
    </StatusBanner>
    <StatusBanner
      v-if="replacedNotice"
      kind="success"
      testid="totp-replaced-success"
    >
      {{ copy.replacedNotice }}
    </StatusBanner>
    <StatusBanner
      v-if="removedNotice"
      kind="success"
      testid="totp-removed-success"
    >
      {{ copy.removedNotice }}
    </StatusBanner>
    <StatusBanner
      v-if="startError"
      kind="error"
      testid="totp-start-error"
      focus-on-mount
    >
      {{ copy.errors[startError] }}
    </StatusBanner>
    <StatusBanner
      v-if="removeError"
      kind="error"
      testid="totp-remove-error"
      focus-on-mount
    >
      {{ copy.errors[removeError] }}
    </StatusBanner>

    <Dialog
      :open="setupOpen"
      @update:open="onSetupOpenChange"
    >
      <DialogContent
        data-testid="totp-setup-dialog"
        :close-label="copy.close"
      >
        <DialogHeader>
          <DialogTitle>{{ setupTitle }}</DialogTitle>
          <DialogDescription>{{ copy.setupDescription }}</DialogDescription>
        </DialogHeader>
        <p
          v-if="replacing"
          class="text-sm text-muted-foreground"
          data-testid="totp-replace-notice"
        >
          {{ copy.replaceNotice }}
        </p>
        <svg
          v-if="qr"
          :aria-label="copy.qrAlt"
          :viewBox="`0 0 ${qr.size} ${qr.size}`"
          class="size-48 justify-self-center"
          data-testid="totp-qr"
          role="img"
        >
          <path
            :d="qr.path"
            fill="currentColor"
          />
        </svg>
        <div class="grid gap-1.5">
          <p class="text-sm font-medium">
            {{ copy.secretLabel }}
          </p>
          <p
            class="rounded-md border p-2 font-mono text-sm"
            data-testid="totp-secret"
          >{{ secret ?? '' }}</p>
          <Button
            class="justify-self-start"
            data-testid="totp-secret-copy"
            type="button"
            variant="outline"
            @click="copySecret"
          >
            {{ copy.copySecret }}
          </Button>
          <StatusBanner
            v-if="secretCopied"
            kind="success"
            testid="totp-secret-copied"
          >
            {{ copy.secretCopied }}
          </StatusBanner>
        </div>
        <form
          data-testid="totp-code-form"
          novalidate
          @submit.prevent="submitCode"
        >
          <FormField
            id="totp-code"
            v-slot="{ id: fieldId, describedBy, invalid }"
            :error="localFormatError ? copy.invalidFormat : undefined"
            :label="copy.codeLabel"
          >
            <Input
              :id="fieldId"
              v-model="code"
              :aria-describedby="describedBy"
              :aria-invalid="invalid"
              autocomplete="one-time-code"
              data-testid="totp-code-input"
              inputmode="numeric"
              maxlength="6"
              pattern="[0-9]*"
            />
          </FormField>
          <StatusBanner
            v-if="completeError"
            kind="error"
            testid="totp-setup-error"
            focus-on-mount
          >
            {{ copy.errors[completeError] }}
          </StatusBanner>
          <DialogFooter>
            <Button
              data-testid="totp-setup-cancel"
              :disabled="completePending"
              type="button"
              variant="ghost"
              @click="cancelSetup"
            >
              {{ copy.cancel }}
            </Button>
            <Button
              data-testid="totp-code-submit"
              :disabled="completePending"
              type="submit"
            >
              {{ completePending ? copy.verifying : copy.verifyButton }}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

    <ConfirmDialog
      :open="removeConfirmOpen"
      :title="copy.removeTitle"
      :description="hasOtherActiveFactor ? copy.removeDescription
        : copy.removeDescriptionFinal"
      :confirm-label="copy.removeConfirm"
      :cancel-label="copy.cancel"
      destructive
      :busy="removePending"
      confirm-action="totp-remove-confirm"
      cancel-action="totp-remove-cancel"
      @confirm="confirmRemove"
      @cancel="cancelRemove"
    />
  </div>
</template>
