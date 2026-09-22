import { computed, type ComputedRef, type InjectionKey } from 'vue';

import type { AuthProvider } from './useAuth';
import type { Locale } from '@/i18n/locale';

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

export interface SecondFactorPasskey {
  readonly id: string;
  readonly createdAt: string;
  readonly lastUsedAt: string | null;
}

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

export interface RegistrationPublicKeyInput {
  readonly challenge: string;
  readonly rp: { readonly name: string; readonly id: string };
  readonly user: {
    readonly id: string;
    readonly name: string;
    readonly displayName: string;
  };
  readonly pubKeyCredParams: ReadonlyArray<{
    readonly type: 'public-key';
    readonly alg: number;
  }>;
  readonly timeout: number;
  readonly excludeCredentials: ReadonlyArray<{
    readonly type: 'public-key';
    readonly id: string;
    readonly transports?: readonly string[];
  }>;
  readonly authenticatorSelection: {
    readonly residentKey: string;
    readonly requireResidentKey: boolean;
    readonly userVerification: string;
  };
  readonly attestation: string;
}

export interface PasskeyRegistrationCredential {
  readonly id: string;
  readonly rawId: string;
  readonly type: 'public-key';
  readonly response: {
    readonly clientDataJSON: string;
    readonly attestationObject: string;
    readonly transports: readonly string[];
  };
  readonly clientExtensionResults: Record<string, never>;
}

export interface RegistrationOptionsResult {
  readonly ceremonyId: string;
  readonly publicKey: RegistrationPublicKeyInput;
}

export interface RegistrationCompletionResult {
  readonly passkey: SecondFactorPasskey;
  readonly recoveryCodes?: readonly string[];
}

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
          recoveryCodesRemaining?: unknown;
        })
      : null;
  });
  const enabled = computed(() => envelope.value?.enabled === true);
  const passkeys = computed(() => {
    const list = envelope.value?.passkeys;
    return Array.isArray(list) ? list.filter(isPasskey) : [];
  });
  const recoveryCodesRemaining = computed(() => {
    const value = envelope.value?.recoveryCodesRemaining;
    return typeof value === 'number' && Number.isInteger(value) && value >= 0
      ? value
      : 0;
  });
  const resolved = computed(
    () => status.value === 'success'
      || status.value === 'error'
      // Nuxt 4 leaves `error` undefined, not null, until a request fails.
      || (error.value ?? null) !== null,
  );

  async function refresh(): Promise<void> {
    await refreshState();
  }

  return { enabled, passkeys, recoveryCodesRemaining, resolved, refresh };
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

/** True only when this browser can attempt a passkey registration. */
export function isWebAuthnSupported(): boolean {
  return typeof window !== 'undefined'
    && typeof window.PublicKeyCredential !== 'undefined'
    && typeof navigator !== 'undefined'
    && typeof navigator.credentials?.create === 'function';
}

/** True for the browser cancelling or aborting the platform ceremony. */
export function isPasskeyCancellation(error: unknown): boolean {
  return error instanceof DOMException
    && (error.name === 'NotAllowedError' || error.name === 'AbortError');
}

function base64UrlToBytes(value: string): Uint8Array {
  const converted = value.replace(/-/g, '+').replace(/_/g, '/');
  const pad = converted.length % 4 === 0 ? 0 : 4 - (converted.length % 4);
  const binary = atob(converted + '='.repeat(pad));
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

function bytesToBase64Url(bytes: ArrayBuffer | Uint8Array): string {
  const array = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  let binary = '';
  for (let i = 0; i < array.length; i += 1) {
    binary += String.fromCharCode(array[i]!);
  }
  return btoa(binary)
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
}

/**
 * Run the browser registration ceremony from accepted canonical options and
 * serialize the result back to the canonical wire shape
 * (`docs/design/passkey-second-factor-contract.md#webauthn-json`). Throws
 * the raw browser error (see `isPasskeyCancellation`) on cancellation or an
 * unsupported browser; never falls back to a differently-shaped credential.
 */
export async function createPasskeyCredential(
  publicKey: RegistrationPublicKeyInput,
): Promise<PasskeyRegistrationCredential> {
  const created = await navigator.credentials.create({
    publicKey: {
      challenge: base64UrlToBytes(publicKey.challenge),
      rp: publicKey.rp,
      user: {
        id: base64UrlToBytes(publicKey.user.id),
        name: publicKey.user.name,
        displayName: publicKey.user.displayName,
      },
      pubKeyCredParams:
        publicKey.pubKeyCredParams as PublicKeyCredentialParameters[],
      timeout: publicKey.timeout,
      excludeCredentials: publicKey.excludeCredentials.map((credential) => ({
        type: 'public-key' as const,
        id: base64UrlToBytes(credential.id),
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
    rawId: bytesToBase64Url(created.rawId),
    type: 'public-key',
    response: {
      clientDataJSON: bytesToBase64Url(response.clientDataJSON),
      attestationObject: bytesToBase64Url(response.attestationObject),
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
