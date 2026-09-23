<script setup lang="ts">
/**
 * Pending second-factor page. Completes a password or provider login, or a
 * settings reauthentication, for an account enrolled in a second factor.
 *
 * State comes only from the `__Host-auth-pending` cookie, read through
 * `GET /api/v1/auth/second-factor`. This page never calls `/me` and never
 * treats the pending cookie as a session — a provider redirect lands here
 * with no query string, and none of its text ever becomes copy, markup, or
 * a request target.
 *
 * Methods render in the server's fixed order (passkey, then recovery). A
 * method value this build does not recognize renders a refresh prompt
 * instead of a route call — beside the known methods, or alone when none
 * remain — and never touches the pending cookie itself.
 */
import { onBeforeUnmount, onMounted, ref } from 'vue';
import FormField from '@/components/app/FormField.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { pageTitle } from '@/i18n/meta';
import {
  type SecondFactorMessage,
  secondFactorCopy,
} from '@/i18n/second-factor';
import {
  DEFAULT_RETURN_PATH,
  isAppRoute,
  validateReturnPath,
} from '@/utils/returnPath';
import {
  assertionToJSON,
  isWebAuthnCancellation,
  isWebAuthnSupported,
  parseAssertionOptions,
  requestAssertion,
} from '../../utils/webauthn';
import {
  isKnownPendingMethod,
  type SecondFactorPendingError,
  SecondFactorPendingFailure,
  type SecondFactorPendingMethod,
  type SecondFactorPendingStatus,
  useSecondFactorPending,
} from '../../composables/secondFactorPending';

const { locale } = useLocale();
const copy = computed(() => secondFactorCopy[locale.value]);
useHead(computed(() => ({ title: pageTitle(copy.value.title) })));

const pending = useSecondFactorPending();
// Real WebAuthn support never changes mid-session; SSR (where `window` is
// absent) never reaches the branch that reads this, since `status` only
// leaves "loading" from `onMounted`.
const passkeySupported = isWebAuthnSupported();

type PageState = 'loading' | 'ready' | 'expired' | 'unavailable';
const state = ref<PageState>('loading');
const status = ref<SecondFactorPendingStatus | null>(null);
// Remembered only to pick the right sign-in-again target if the pending row
// later turns out to be gone; never used to call `/me` or treat the cookie
// as a session.
const purpose = ref<'login' | 'reauth'>('login');

const passkeyBusy = ref(false);
const passkeyMessage = ref<SecondFactorMessage | null>(null);
const recoveryCode = ref('');
const recoveryBusy = ref(false);
const recoveryMessage = ref<SecondFactorMessage | null>(null);

const KNOWN_ORDER: readonly SecondFactorPendingMethod[] = [
  'passkey',
  'recovery',
];

const knownMethods = computed<SecondFactorPendingMethod[]>(() => {
  const methods = status.value?.methods ?? [];
  return KNOWN_ORDER.filter((method) => methods.includes(method));
});
const hasUnknownMethod = computed(() => (status.value?.methods ?? []).some(
  (method) => !isKnownPendingMethod(method),
));

const expiredTarget = computed(() => (purpose.value === 'reauth'
  ? '/app/settings/sessions?error=authentication_required'
  : '/login?error=authentication_required'));

/** Maps a retryable pending failure to its generic display copy. Callers
 * handle `authentication-required` separately by moving to "expired". */
function messageFor(kind: SecondFactorPendingError): SecondFactorMessage {
  switch (kind) {
    case 'verification-failed':
    case 'challenge-invalid':
      return 'verificationFailed';
    case 'rate-limited':
      return 'rateLimited';
    default:
      return 'tryAgain';
  }
}

async function load(): Promise<void> {
  state.value = 'loading';
  try {
    const data = await pending.status();
    status.value = data;
    purpose.value = data.purpose;
    state.value = 'ready';
  } catch (error) {
    const failure = error as SecondFactorPendingFailure;
    state.value = failure.kind === 'authentication-required'
      ? 'expired'
      : 'unavailable';
  }
}

onMounted(load);

/** Navigates to the pending row's own validated return path, re-validated
 * client-side the same way login.vue treats `?next=` — defense in depth,
 * never trusting a stored string as a safe navigation target outright. A
 * destination outside this app's own pages (for example `/oauth/authorize`)
 * needs a real browser navigation, matching login.vue's own rule. */
async function afterSuccess(): Promise<void> {
  const target = validateReturnPath(status.value?.returnPath)
    ?? DEFAULT_RETURN_PATH;
  if (isAppRoute(target)) {
    await navigateTo(target);
  } else {
    await navigateTo(target, { external: true });
  }
}

function refreshPage(): void {
  try {
    window.location.reload();
  } catch {
    // Best effort only; not every test/browser environment implements it.
  }
}

async function usePasskey(): Promise<void> {
  if (!status.value || passkeyBusy.value || !passkeySupported) return;
  const csrfToken = status.value.csrfToken;
  passkeyMessage.value = null;
  passkeyBusy.value = true;
  try {
    const options = await pending.passkeyOptions(csrfToken);
    const requestOptions = parseAssertionOptions(options.publicKey);
    const credential = await requestAssertion(requestOptions);
    const credentialJSON = assertionToJSON(credential);
    await pending.verifyPasskey(csrfToken, options.ceremonyId, credentialJSON);
    await afterSuccess();
  } catch (error) {
    if (isWebAuthnCancellation(error)) {
      passkeyMessage.value = 'passkeyCancelled';
    } else if (error instanceof SecondFactorPendingFailure
      && error.kind === 'authentication-required') {
      state.value = 'expired';
    } else if (error instanceof SecondFactorPendingFailure) {
      passkeyMessage.value = messageFor(error.kind);
    } else {
      // A malformed options response (`WebAuthnDataInvalid`) or any other
      // unexpected WebAuthn failure degrades to the same generic retry copy.
      passkeyMessage.value = 'tryAgain';
    }
  } finally {
    passkeyBusy.value = false;
  }
}

function clearRecoveryCode(): void {
  recoveryCode.value = '';
}

async function submitRecovery(): Promise<void> {
  if (!status.value || recoveryBusy.value) return;
  const code = recoveryCode.value.trim();
  recoveryMessage.value = null;
  if (!code) {
    recoveryMessage.value = 'enterRecoveryCode';
    return;
  }
  recoveryBusy.value = true;
  const csrfToken = status.value.csrfToken;
  try {
    await pending.verifyRecovery(csrfToken, code);
    await afterSuccess();
  } catch (error) {
    if (error instanceof SecondFactorPendingFailure
      && error.kind === 'authentication-required') {
      state.value = 'expired';
    } else if (error instanceof SecondFactorPendingFailure) {
      recoveryMessage.value = messageFor(error.kind);
    } else {
      recoveryMessage.value = 'tryAgain';
    }
  } finally {
    clearRecoveryCode();
    recoveryBusy.value = false;
  }
}

// Never keep a recovery code around longer than the attempt that used it.
onBeforeUnmount(clearRecoveryCode);
</script>

<template>
  <main
    class="mx-auto w-full max-w-[26rem] px-6 py-16"
    data-testid="second-factor-page"
  >
    <h1
      class="border-b pb-4 text-xl font-semibold"
      data-page-title
    >
      {{ copy.title }}
    </h1>
    <p
      v-if="state === 'ready'"
      class="mt-4 text-base text-muted-foreground"
    >
      {{ copy.lead[purpose] }}
    </p>

    <StatusBanner
      v-if="state === 'loading'"
      class="mt-6"
      kind="info"
      testid="second-factor-loading"
    >
      {{ copy.messages.loading }}
    </StatusBanner>

    <StatusBanner
      v-else-if="state === 'unavailable'"
      class="mt-6"
      focus-on-mount
      kind="error"
      testid="second-factor-unavailable"
    >
      {{ copy.messages.unavailable }}
    </StatusBanner>

    <template v-else-if="state === 'expired'">
      <StatusBanner
        class="mt-6"
        focus-on-mount
        kind="error"
        testid="second-factor-expired"
      >
        {{ copy.messages.expired }}
      </StatusBanner>
      <nav class="mt-6">
        <NuxtLink
          class="text-primary underline-offset-4 hover:underline"
          data-testid="second-factor-sign-in-again"
          :to="expiredTarget"
        >
          {{ copy.messages.signInAgain }}
        </NuxtLink>
      </nav>
    </template>

    <template v-else-if="state === 'ready'">
      <section
        v-if="knownMethods.includes('passkey')"
        class="mt-8"
        data-testid="second-factor-passkey"
      >
        <h2 class="text-sm font-medium">
          {{ copy.passkey.heading }}
        </h2>
        <p class="mt-1 text-sm text-muted-foreground">
          {{ copy.passkey.description }}
        </p>
        <template v-if="passkeySupported">
          <StatusBanner
            v-if="passkeyMessage"
            class="mt-3"
            kind="error"
            testid="second-factor-passkey-error"
          >
            {{ copy.messages[passkeyMessage] }}
          </StatusBanner>
          <Button
            class="mt-3 h-9 w-full"
            data-testid="second-factor-passkey-button"
            :disabled="passkeyBusy"
            type="button"
            @click="usePasskey"
          >
            {{ passkeyBusy ? copy.passkey.pending : copy.passkey.button }}
          </Button>
        </template>
        <StatusBanner
          v-else
          class="mt-3"
          kind="error"
          testid="second-factor-passkey-unsupported"
        >
          {{ copy.messages.passkeyUnsupported }}
        </StatusBanner>
      </section>

      <section
        v-if="knownMethods.includes('recovery')"
        class="mt-8"
        data-testid="second-factor-recovery"
      >
        <h2 class="text-sm font-medium">
          {{ copy.recovery.heading }}
        </h2>
        <p class="mt-1 text-sm text-muted-foreground">
          {{ copy.recovery.description }}
        </p>
        <StatusBanner
          v-if="recoveryMessage"
          class="mt-3"
          kind="error"
          testid="second-factor-recovery-error"
        >
          {{ copy.messages[recoveryMessage] }}
        </StatusBanner>
        <form
          class="mt-3 grid gap-3"
          data-testid="second-factor-recovery-form"
          novalidate
          @submit.prevent="submitRecovery"
        >
          <FormField
            id="second-factor-recovery-code"
            v-slot="{ id, describedBy, invalid }"
            :label="copy.recovery.label"
          >
            <Input
              :id="id"
              v-model="recoveryCode"
              :aria-describedby="describedBy"
              :aria-invalid="invalid"
              autocapitalize="off"
              autocomplete="one-time-code"
              autocorrect="off"
              spellcheck="false"
              type="text"
            />
          </FormField>
          <Button
            class="h-9 w-full"
            :disabled="recoveryBusy"
            type="submit"
          >
            {{ recoveryBusy ? copy.recovery.pending : copy.recovery.button }}
          </Button>
        </form>
      </section>

      <StatusBanner
        v-if="hasUnknownMethod"
        class="mt-8"
        kind="info"
        testid="second-factor-refresh-prompt"
      >
        {{ copy.messages.unknownMethod }}
        <Button
          class="mt-3 h-9 w-full"
          data-testid="second-factor-refresh-button"
          type="button"
          @click="refreshPage"
        >
          {{ copy.messages.refresh }}
        </Button>
      </StatusBanner>
    </template>
  </main>
</template>
