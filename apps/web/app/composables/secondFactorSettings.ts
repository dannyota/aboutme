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

export class SecondFactorSettingsFailure extends Error {
  readonly kind: SecondFactorSettingsErrorKind;

  constructor(kind: SecondFactorSettingsErrorKind) {
    super(`second-factor settings failed: ${kind}`);
    this.name = 'SecondFactorSettingsFailure';
    this.kind = kind;
  }
}

export type RegistrationPublicKeyInput
  = components['schemas']['WebAuthnRegistrationPublicKey'];

export type PasskeyRegistrationCredential
  = components['schemas']['PasskeyRegistrationCompletionRequest']['credential'];

export type RegistrationOptionsResult
  = components['schemas']['SecondFactorRegistrationOptionsResponse']['data'];

export type RegistrationCompletionResult
  = components['schemas']['SecondFactorRegistrationResponse']['data'];

export interface SecondFactorSettingsActions {
  reauthenticate(password: string): Promise<void>;
  startProviderReauth(provider: AuthProvider): Promise<void>;
  registrationOptions(): Promise<RegistrationOptionsResult>;
  completeRegistration(
    ceremonyId: string,
    credential: PasskeyRegistrationCredential,
  ): Promise<RegistrationCompletionResult>;
  removePasskey(id: string): Promise<void>;
  regenerateRecoveryCodes(): Promise<readonly string[]>;
}

export const SecondFactorSettingsActionsKey: InjectionKey<
  SecondFactorSettingsActions
> = Symbol('aboutme-second-factor-settings-actions');

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

const RECOVERY_CODES_FILENAME = 'aboutme-recovery-codes.txt';

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
