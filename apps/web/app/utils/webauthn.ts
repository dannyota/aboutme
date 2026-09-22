/**
 * WebAuthn assertion helpers for the pending second-factor page.
 *
 * Converts only the accepted canonical fields between the server's unpadded
 * base64url JSON (docs/design/passkey-second-factor-contract.md#webauthn-json)
 * and the browser's byte-array WebAuthn API. Every conversion validates its
 * input and throws `WebAuthnDataInvalid` on anything outside the accepted
 * shape, so a malformed or hostile options response never reaches
 * `navigator.credentials.get`. This module never handles registration: the
 * pending page only ever runs an assertion ceremony.
 */
import type { components } from '../api/generated/openapi';

export type AssertionCredentialJSON
  = components['schemas']['PasskeyAssertionCompletionRequest']['credential'];

const BASE64URL_PATTERN = /^[A-Za-z0-9_-]+$/u;
const KNOWN_TRANSPORTS: readonly string[] = [
  'usb',
  'nfc',
  'ble',
  'smart-card',
  'hybrid',
  'internal',
];

/** Thrown when server-supplied WebAuthn data is not the accepted shape. */
export class WebAuthnDataInvalid extends Error {
  constructor() {
    super('webauthn: malformed data');
    this.name = 'WebAuthnDataInvalid';
  }
}

/** True when this browser can attempt a WebAuthn ceremony at all. */
export function isWebAuthnSupported(): boolean {
  return typeof window !== 'undefined'
    && typeof window.PublicKeyCredential === 'function'
    && typeof navigator !== 'undefined'
    && typeof navigator.credentials?.get === 'function';
}

/** Encodes bytes as canonical unpadded base64url. */
export function encodeBase64Url(bytes: ArrayBuffer | Uint8Array): string {
  const view = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  let binary = '';
  for (const byte of view) binary += String.fromCharCode(byte);
  return btoa(binary)
    .replace(/\+/gu, '-')
    .replace(/\//gu, '_')
    .replace(/=+$/u, '');
}

/**
 * Decodes canonical unpadded base64url. Throws `WebAuthnDataInvalid` for
 * anything else — wrong type, empty, padded, or out-of-alphabet characters.
 */
export function decodeBase64Url(value: unknown): Uint8Array {
  if (typeof value !== 'string' || !BASE64URL_PATTERN.test(value)) {
    throw new WebAuthnDataInvalid();
  }
  const remainder = value.length % 4;
  const padded = (remainder === 0
    ? value
    : value + '='.repeat(4 - remainder))
    .replace(/-/gu, '+')
    .replace(/_/gu, '/');
  let binary: string;
  try {
    binary = atob(padded);
  } catch {
    throw new WebAuthnDataInvalid();
  }
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

function parseCredentialDescriptor(
  value: unknown,
): PublicKeyCredentialDescriptor {
  if (typeof value !== 'object' || value === null) {
    throw new WebAuthnDataInvalid();
  }
  const raw = value as { type?: unknown; id?: unknown; transports?: unknown };
  if (raw.type !== 'public-key') throw new WebAuthnDataInvalid();
  const id = decodeBase64Url(raw.id);
  if (raw.transports === undefined) {
    return { type: 'public-key', id };
  }
  if (
    !Array.isArray(raw.transports)
    || raw.transports.length === 0
    || !raw.transports.every(
      (transport) => typeof transport === 'string'
        && KNOWN_TRANSPORTS.includes(transport),
    )
  ) {
    throw new WebAuthnDataInvalid();
  }
  return {
    type: 'public-key',
    id,
    transports: raw.transports as AuthenticatorTransport[],
  };
}

/**
 * Validates and converts an accepted assertion `publicKey` object (the
 * server's `WebAuthnAssertionPublicKey`, received as `unknown` JSON) into
 * `PublicKeyCredentialRequestOptions`. Throws `WebAuthnDataInvalid` for a
 * malformed response instead of forwarding it to the browser.
 */
export function parseAssertionOptions(
  publicKey: unknown,
): PublicKeyCredentialRequestOptions {
  if (typeof publicKey !== 'object' || publicKey === null) {
    throw new WebAuthnDataInvalid();
  }
  const raw = publicKey as {
    challenge?: unknown;
    timeout?: unknown;
    rpId?: unknown;
    allowCredentials?: unknown;
    userVerification?: unknown;
  };
  const challenge = decodeBase64Url(raw.challenge);
  if (typeof raw.rpId !== 'string' || raw.rpId === '') {
    throw new WebAuthnDataInvalid();
  }
  if (
    !Array.isArray(raw.allowCredentials)
    || raw.allowCredentials.length === 0
  ) {
    throw new WebAuthnDataInvalid();
  }
  const allowCredentials = raw.allowCredentials.map(parseCredentialDescriptor);
  const userVerification: UserVerificationRequirement
    = raw.userVerification === 'required'
      || raw.userVerification === 'preferred'
      || raw.userVerification === 'discouraged'
      ? raw.userVerification
      : 'required';
  const options: PublicKeyCredentialRequestOptions = {
    challenge,
    rpId: raw.rpId,
    allowCredentials,
    userVerification,
  };
  if (typeof raw.timeout === 'number') options.timeout = raw.timeout;
  return options;
}

/**
 * Runs `navigator.credentials.get`. Rejects with the browser's own
 * `DOMException` on cancellation, timeout, or any other WebAuthn failure —
 * see `isWebAuthnCancellation` — and with a `NotAllowedError` when the
 * browser resolves with no credential.
 */
export async function requestAssertion(
  options: PublicKeyCredentialRequestOptions,
): Promise<PublicKeyCredential> {
  const credential = await navigator.credentials.get({ publicKey: options });
  if (credential === null) {
    throw new DOMException('no credential returned', 'NotAllowedError');
  }
  return credential as PublicKeyCredential;
}

/** True for a user-cancelled or timed-out WebAuthn ceremony. */
export function isWebAuthnCancellation(error: unknown): boolean {
  return error instanceof DOMException
    && (error.name === 'NotAllowedError' || error.name === 'AbortError');
}

/**
 * Converts a completed assertion into the exact accepted completion body.
 * `clientExtensionResults` is always sent as `{}`: v0.4.2 requests no
 * extensions and the server rejects anything else, regardless of what a
 * browser attaches to `getClientExtensionResults()`.
 */
export function assertionToJSON(
  credential: PublicKeyCredential,
): AssertionCredentialJSON {
  const response = credential.response as AuthenticatorAssertionResponse;
  const rawId = encodeBase64Url(credential.rawId);
  return {
    id: rawId,
    rawId,
    type: 'public-key',
    response: {
      clientDataJSON: encodeBase64Url(response.clientDataJSON),
      authenticatorData: encodeBase64Url(response.authenticatorData),
      signature: encodeBase64Url(response.signature),
      userHandle: response.userHandle
        ? encodeBase64Url(response.userHandle)
        : null,
    },
    clientExtensionResults: {},
  };
}
