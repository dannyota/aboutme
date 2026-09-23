import {
  computed,
  ref,
  watch,
  type ComputedRef,
  type InjectionKey,
} from 'vue';

import type { AuthProvider } from './useAuth';
import type { components } from '../api/generated/openapi';
import type { Locale } from '@/i18n/locale';
import { decodeBase64Url, encodeBase64Url } from '../utils/webauthn';

/**
 * `secondFactorSettings` — the closed contract behind the account-settings
 * passkey and recovery-code controls
 * (`docs/design/passkey-second-factor-contract.md`).
 *
 * `SecondFactorSettings` is presentational: it receives `enrollmentOpen`,
 * `hasPassword`, and `providers` as props, reads factor state through
 * `useSecondFactorState`, and performs every side effect through
 * `SecondFactorSettingsActionsKey`. Every rejection is a
 * `SecondFactorSettingsFailure` so the component maps to fixed copy without
 * reading a raw server body.
 */

export type SecondFactorPasskey = components['schemas']['SecondFactorPasskey'];

export type SecondFactorSettingsErrorKind
  = | 'reauth-required'
    | 'enrollment-closed'
    | 'limit-reached'
    | 'challenge-invalid'
    | 'verification-failed'
    | 'not-found'
    | 'rate-limited'
    | 'unavailable';

/** Rejection value for every second-factor management action. */
export class SecondFactorSettingsFailure extends Error {
  readonly kind: SecondFactorSettingsErrorKind;

  constructor(kind: SecondFactorSettingsErrorKind) {
    super(`second-factor settings failed: ${kind}`);
    this.name = 'SecondFactorSettingsFailure';
    this.kind = kind;
  }
}

// Aliased from the generated client (not hand-written) so a wire-shape
// change fails typecheck here instead of silently drifting: the request
// body sent to `navigator.credentials.create` and to
// `POST /me/second-factor/passkeys` must stay exactly what the server
// accepts, the same reason `utils/webauthn.ts` aliases its own assertion
// credential type from `PasskeyAssertionCompletionRequest`.
export type RegistrationPublicKeyInput
  = components['schemas']['WebAuthnRegistrationPublicKey'];

export type PasskeyRegistrationCredential
  = components['schemas']['PasskeyRegistrationCompletionRequest']['credential'];

export type RegistrationOptionsResult
  = components['schemas']['SecondFactorRegistrationOptionsResponse']['data'];

export type RegistrationCompletionResult
  = components['schemas']['SecondFactorRegistrationResponse']['data'];

export interface SecondFactorSettingsActions {
  /** Refresh the current session's recent-reauthentication time. */
  reauthenticate(password: string): Promise<void>;
  /** Begin the provider OAuth reauthentication round trip. */
  startProviderReauth(provider: AuthProvider): Promise<void>;
  /** Start a passkey registration ceremony. */
  registrationOptions(): Promise<RegistrationOptionsResult>;
  /** Verify and store the completed passkey ceremony. */
  completeRegistration(
    ceremonyId: string,
    credential: PasskeyRegistrationCredential,
  ): Promise<RegistrationCompletionResult>;
  /** Remove one owned passkey by its internal id. */
  removePasskey(id: string): Promise<void>;
  /** Replace every recovery code and return the new set once. */
  regenerateRecoveryCodes(): Promise<readonly string[]>;
}

export const SecondFactorSettingsActionsKey: InjectionKey<
  SecondFactorSettingsActions
> = Symbol('aboutme-second-factor-settings-actions');

// --- Factor state read -------------------------------------------------

interface SecondFactorStateEnvelope {
  data?: unknown;
}

function isIsoDateString(value: unknown): value is string {
  return typeof value === 'string'
    && value !== ''
    && Number.isFinite(Date.parse(value));
}

function isPasskey(value: unknown): value is SecondFactorPasskey {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as {
    id?: unknown;
    createdAt?: unknown;
    lastUsedAt?: unknown;
  };
  return typeof candidate.id === 'string'
    && candidate.id !== ''
    && isIsoDateString(candidate.createdAt)
    && (candidate.lastUsedAt === null || isIsoDateString(candidate.lastUsedAt));
}

export interface UseSecondFactorStateReturn {
  enabled: ComputedRef<boolean>;
  passkeys: ComputedRef<readonly SecondFactorPasskey[]>;
  totpEnabled: ComputedRef<boolean>;
  recoveryCodesRemaining: ComputedRef<number>;
  resolved: ComputedRef<boolean>;
  refresh: () => Promise<void>;
}

/**
 * `useSecondFactorState` — account factor state from
 * `GET /api/v1/me/second-factor`, read regardless of the passkey-enrollment
 * capability (enrollment can be closed while verification, removal,
 * recovery, and state stay available). Anything but an exact well-formed
 * response degrades to the unenrolled shape, matching `useCapabilities`.
 */
export function useSecondFactorState(): UseSecondFactorStateReturn {
  const { data, status, error, refresh: refreshState }
    = useFetch<SecondFactorStateEnvelope>('/api/v1/me/second-factor', {
      credentials: 'include',
      server: false,
      cache: 'no-store',
    });

  const envelope = computed(() => {
    const value = data.value?.data;
    return typeof value === 'object' && value !== null
      ? (value as {
          enabled?: unknown;
          passkeys?: unknown;
          totpEnabled?: unknown;
          recoveryCodesRemaining?: unknown;
        })
      : null;
  });
  const enabled = computed(() => envelope.value?.enabled === true);
  const passkeys = computed(() => {
    const list = envelope.value?.passkeys;
    return Array.isArray(list) ? list.filter(isPasskey) : [];
  });
  // Read regardless of `totpEnrollment` (enrollment can be closed while
  // verification, removal, and state stay available); absent or malformed
  // is false, matching mixed-version compatibility (totp-second-factor
  // -contract.md "Migration, mixed versions, and loss").
  const totpEnabled = computed(() => envelope.value?.totpEnabled === true);
  const recoveryCodesRemaining = computed(() => {
    const value = envelope.value?.recoveryCodesRemaining;
    return typeof value === 'number' && Number.isInteger(value) && value >= 0
      ? value
      : 0;
  });
  // Sticky: `refresh()` (called after every successful mutation) sends
  // `status` back through `pending` while the replacement read is in
  // flight. A plain `status`-derived flag would flip back to unresolved
  // then, swapping the whole section back to the loading skeleton and
  // hiding the success banner and passkey list a person is looking at.
  // Once genuinely resolved once, stay resolved.
  const resolvedOnce = ref(
    status.value === 'success'
    || status.value === 'error'
    || (error.value ?? null) !== null,
  );
  watch([status, error], () => {
    if (
      status.value === 'success'
      || status.value === 'error'
      // Nuxt 4 leaves `error` undefined, not null, until a request fails.
      || (error.value ?? null) !== null
    ) {
      resolvedOnce.value = true;
    }
  });
  const resolved = computed(() => resolvedOnce.value);

  async function refresh(): Promise<void> {
    await refreshState();
  }

  return {
    enabled,
    passkeys,
    totpEnabled,
    recoveryCodesRemaining,
    resolved,
    refresh,
  };
}

// --- Error mapping -------------------------------------------------------

function failureStatus(error: unknown): number | null {
  const candidate = error as { statusCode?: unknown; status?: unknown } | null;
  for (const key of ['statusCode', 'status'] as const) {
    const value = candidate?.[key];
    if (typeof value === 'number') return value;
  }
  return null;
}

function failureCode(error: unknown): unknown {
  return (error as { data?: { error?: { code?: unknown } } } | null)?.data
    ?.error?.code;
}

/**
 * Map a `POST .../passkeys/options` rejection. `404` covers enrollment being
 * turned off (the uniform not-found response, matching an unregistered
 * route) and never reaches this function for any other reason.
 */
export function mapPasskeyOptionsError(
  error: unknown,
): SecondFactorSettingsFailure {
  const status = failureStatus(error);
  const code = failureCode(error);
  if (status === 403 && code === 'reauth_required') {
    return new SecondFactorSettingsFailure('reauth-required');
  }
  if (status === 404) {
    return new SecondFactorSettingsFailure('enrollment-closed');
  }
  if (status === 409 && code === 'passkey_limit_reached') {
    return new SecondFactorSettingsFailure('limit-reached');
  }
  if (status === 429 && code === 'rate_limited') {
    return new SecondFactorSettingsFailure('rate-limited');
  }
  return new SecondFactorSettingsFailure('unavailable');
}

/**
 * Map a `POST .../passkeys` (registration completion) rejection. Adds
 * `challenge-invalid` (an expired or foreign ceremony) and
 * `verification-failed` (a failed WebAuthn verification or duplicate
 * credential) to the options vocabulary.
 */
export function mapPasskeyCompletionError(
  error: unknown,
): SecondFactorSettingsFailure {
  const status = failureStatus(error);
  const code = failureCode(error);
  if (status === 403 && code === 'reauth_required') {
    return new SecondFactorSettingsFailure('reauth-required');
  }
  if (status === 404) {
    return new SecondFactorSettingsFailure('enrollment-closed');
  }
  if (status === 409 && code === 'passkey_limit_reached') {
    return new SecondFactorSettingsFailure('limit-reached');
  }
  if (status === 400 && code === 'challenge_invalid') {
    return new SecondFactorSettingsFailure('challenge-invalid');
  }
  if (status === 400 && code === 'verification_failed') {
    return new SecondFactorSettingsFailure('verification-failed');
  }
  if (status === 429 && code === 'rate_limited') {
    return new SecondFactorSettingsFailure('rate-limited');
  }
  return new SecondFactorSettingsFailure('unavailable');
}

/** Map a `DELETE .../passkeys/{id}` rejection. */
export function mapPasskeyRemovalError(
  error: unknown,
): SecondFactorSettingsFailure {
  const status = failureStatus(error);
  const code = failureCode(error);
  if (status === 403 && code === 'reauth_required') {
    return new SecondFactorSettingsFailure('reauth-required');
  }
  if (status === 404) return new SecondFactorSettingsFailure('not-found');
  if (status === 429 && code === 'rate_limited') {
    return new SecondFactorSettingsFailure('rate-limited');
  }
  return new SecondFactorSettingsFailure('unavailable');
}

/** Map a `POST .../recovery-codes` (regeneration) rejection. */
export function mapRecoveryRegenerationError(
  error: unknown,
): SecondFactorSettingsFailure {
  const status = failureStatus(error);
  const code = failureCode(error);
  if (status === 403 && code === 'reauth_required') {
    return new SecondFactorSettingsFailure('reauth-required');
  }
  if (status === 404) return new SecondFactorSettingsFailure('not-found');
  if (status === 429 && code === 'rate_limited') {
    return new SecondFactorSettingsFailure('rate-limited');
  }
  return new SecondFactorSettingsFailure('unavailable');
}

// --- Browser WebAuthn ceremony -------------------------------------------
//
// `isWebAuthnSupported`, `isWebAuthnCancellation`, and the base64url codec
// are shared with `utils/webauthn.ts` (the pending-login page's assertion
// helpers) and live there; import them from that module rather than from
// here, so Nuxt's auto-import has exactly one binding per name. This module
// keeps only what genuinely differs: `createPasskeyCredential` runs
// `navigator.credentials.create` with `PublicKeyCredentialCreationOptions`,
// the registration ceremony `utils/webauthn.ts` explicitly never handles.

/**
 * Run the browser registration ceremony from accepted canonical options and
 * serialize the result back to the canonical wire shape
 * (`docs/design/passkey-second-factor-contract.md#webauthn-json`). Throws
 * the raw browser error (see `isWebAuthnCancellation`) on cancellation or an
 * unsupported browser; never falls back to a differently-shaped credential.
 */
export async function createPasskeyCredential(
  publicKey: RegistrationPublicKeyInput,
): Promise<PasskeyRegistrationCredential> {
  const created = await navigator.credentials.create({
    publicKey: {
      challenge: decodeBase64Url(publicKey.challenge),
      rp: publicKey.rp,
      user: {
        id: decodeBase64Url(publicKey.user.id),
        name: publicKey.user.name,
        displayName: publicKey.user.displayName,
      },
      pubKeyCredParams:
        publicKey.pubKeyCredParams as PublicKeyCredentialParameters[],
      timeout: publicKey.timeout,
      excludeCredentials: publicKey.excludeCredentials.map((credential) => ({
        type: 'public-key' as const,
        id: decodeBase64Url(credential.id),
        transports: credential.transports as AuthenticatorTransport[]
        | undefined,
      })),
      authenticatorSelection: {
        residentKey: publicKey.authenticatorSelection
          .residentKey as ResidentKeyRequirement,
        requireResidentKey:
          publicKey.authenticatorSelection.requireResidentKey,
        userVerification: publicKey.authenticatorSelection
          .userVerification as UserVerificationRequirement,
      },
      attestation: publicKey.attestation as AttestationConveyancePreference,
    },
  });
  if (!(created instanceof PublicKeyCredential)) {
    throw new Error('the browser returned an unusable passkey credential');
  }
  const response = created.response;
  if (!(response instanceof AuthenticatorAttestationResponse)) {
    throw new Error('the browser returned an unexpected passkey response');
  }
  const transports = typeof response.getTransports === 'function'
    ? response.getTransports()
    : [];
  return {
    id: created.id,
    rawId: encodeBase64Url(created.rawId),
    type: 'public-key',
    response: {
      clientDataJSON: encodeBase64Url(response.clientDataJSON),
      attestationObject: encodeBase64Url(response.attestationObject),
      transports,
    },
    clientExtensionResults: {},
  };
}

// --- Recovery-code download -----------------------------------------------

export const RECOVERY_CODES_FILENAME = 'aboutme-recovery-codes.txt';

/**
 * The exact downloaded byte contract: UTF-8, no byte-order mark, LF line
 * endings, one trailing LF, and a locale-selected heading. See
 * "Management responses and recovery download" in
 * `docs/design/passkey-second-factor-contract.md`.
 */
export function recoveryCodesDownloadText(
  codes: readonly string[],
  locale: Locale,
): string {
  const heading = locale === 'vi'
    ? 'Mã khôi phục aboutme'
    : 'aboutme recovery codes';
  return `${heading}\n\n${codes.join('\n')}\n`;
}

export interface DownloadRecoveryCodesDeps {
  readonly createObjectURL?: (blob: Blob) => string;
  readonly revokeObjectURL?: (url: string) => void;
}

/** Downloads the codes as a local file; never issues a second request. */
export function downloadRecoveryCodes(
  codes: readonly string[],
  locale: Locale,
  deps: DownloadRecoveryCodesDeps = {},
): void {
  const createObjectURL = deps.createObjectURL ?? URL.createObjectURL;
  const revokeObjectURL = deps.revokeObjectURL ?? URL.revokeObjectURL;
  const blob = new Blob([recoveryCodesDownloadText(codes, locale)], {
    type: 'text/plain;charset=utf-8',
  });
  const url = createObjectURL(blob);
  try {
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = RECOVERY_CODES_FILENAME;
    anchor.click();
  } finally {
    revokeObjectURL(url);
  }
}
