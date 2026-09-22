import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  assertionToJSON,
  decodeBase64Url,
  encodeBase64Url,
  isWebAuthnCancellation,
  isWebAuthnSupported,
  parseAssertionOptions,
  requestAssertion,
  WebAuthnDataInvalid,
} from '../app/utils/webauthn';

function bytesOf(...values: number[]): Uint8Array {
  return new Uint8Array(values);
}

describe('encodeBase64Url / decodeBase64Url', () => {
  it('round-trips arbitrary bytes with no padding', () => {
    const original = bytesOf(0, 1, 2, 253, 254, 255, 16, 32, 48);
    const encoded = encodeBase64Url(original);
    expect(encoded).not.toContain('=');
    expect(encoded).not.toContain('+');
    expect(encoded).not.toContain('/');
    expect(Array.from(decodeBase64Url(encoded))).toEqual(
      Array.from(original),
    );
  });

  it('accepts an ArrayBuffer the same way as a Uint8Array', () => {
    const bytes = bytesOf(9, 8, 7, 6);
    expect(encodeBase64Url(bytes.buffer)).toBe(encodeBase64Url(bytes));
  });

  it('rejects a non-string value', () => {
    expect(() => decodeBase64Url(42)).toThrow(WebAuthnDataInvalid);
    expect(() => decodeBase64Url(null)).toThrow(WebAuthnDataInvalid);
    expect(() => decodeBase64Url(undefined)).toThrow(WebAuthnDataInvalid);
  });

  it('rejects an empty string', () => {
    expect(() => decodeBase64Url('')).toThrow(WebAuthnDataInvalid);
  });

  it('rejects padded base64url', () => {
    expect(() => decodeBase64Url('AAAA=')).toThrow(WebAuthnDataInvalid);
  });

  it('rejects standard base64 characters (+ and /)', () => {
    expect(() => decodeBase64Url('a+b')).toThrow(WebAuthnDataInvalid);
    expect(() => decodeBase64Url('a/b')).toThrow(WebAuthnDataInvalid);
  });
});

describe('isWebAuthnSupported', () => {
  afterEach(() => {
    Reflect.deleteProperty(window, 'PublicKeyCredential');
    Reflect.deleteProperty(navigator, 'credentials');
  });

  it('is false when the browser has no WebAuthn support', () => {
    Reflect.deleteProperty(window, 'PublicKeyCredential');
    expect(isWebAuthnSupported()).toBe(false);
  });

  it('is true when both PublicKeyCredential and credentials.get exist',
    () => {
      Object.defineProperty(window, 'PublicKeyCredential', {
        value: function PublicKeyCredentialStub() {},
        configurable: true,
      });
      Object.defineProperty(navigator, 'credentials', {
        value: { get: vi.fn() },
        configurable: true,
      });
      expect(isWebAuthnSupported()).toBe(true);
    });
});

const VALID_PUBLIC_KEY = {
  challenge: encodeBase64Url(bytesOf(1, 2, 3, 4)),
  timeout: 300000,
  rpId: 'aboutme.vn',
  allowCredentials: [
    { type: 'public-key', id: encodeBase64Url(bytesOf(9, 9, 9)) },
    {
      type: 'public-key',
      id: encodeBase64Url(bytesOf(8, 8, 8)),
      transports: ['internal', 'hybrid'],
    },
  ],
  userVerification: 'required',
};

describe('parseAssertionOptions', () => {
  it('converts a well-formed options object', () => {
    const options = parseAssertionOptions(VALID_PUBLIC_KEY);
    expect(Array.from(options.challenge as Uint8Array)).toEqual([1, 2, 3, 4]);
    expect(options.rpId).toBe('aboutme.vn');
    expect(options.userVerification).toBe('required');
    expect(options.timeout).toBe(300000);
    expect(options.allowCredentials).toHaveLength(2);
    expect(options.allowCredentials?.[0]).toMatchObject({
      type: 'public-key',
    });
    expect(options.allowCredentials?.[1]?.transports).toEqual([
      'internal',
      'hybrid',
    ]);
  });

  it('defaults userVerification to required when omitted', () => {
    const { userVerification: _omit, ...rest } = VALID_PUBLIC_KEY;
    const options = parseAssertionOptions(rest);
    expect(options.userVerification).toBe('required');
  });

  it('omits timeout when the field is not a number', () => {
    const options = parseAssertionOptions({
      ...VALID_PUBLIC_KEY,
      timeout: '300000',
    });
    expect(options.timeout).toBeUndefined();
  });

  it.each([
    ['not an object', 'nope'],
    ['null', null],
    ['missing challenge', { ...VALID_PUBLIC_KEY, challenge: undefined }],
    ['malformed challenge', { ...VALID_PUBLIC_KEY, challenge: 'has spaces' }],
    ['missing rpId', { ...VALID_PUBLIC_KEY, rpId: undefined }],
    ['non-string rpId', { ...VALID_PUBLIC_KEY, rpId: 12 }],
    [
      'allowCredentials not an array',
      { ...VALID_PUBLIC_KEY, allowCredentials: 'x' },
    ],
    ['empty allowCredentials', { ...VALID_PUBLIC_KEY, allowCredentials: [] }],
    [
      'credential missing type',
      {
        ...VALID_PUBLIC_KEY,
        allowCredentials: [{ id: encodeBase64Url(bytesOf(1)) }],
      },
    ],
    [
      'credential with wrong type',
      {
        ...VALID_PUBLIC_KEY,
        allowCredentials: [
          { type: 'password', id: encodeBase64Url(bytesOf(1)) },
        ],
      },
    ],
    [
      'credential with unknown transport',
      {
        ...VALID_PUBLIC_KEY,
        allowCredentials: [
          {
            type: 'public-key',
            id: encodeBase64Url(bytesOf(1)),
            transports: ['carrier-pigeon'],
          },
        ],
      },
    ],
  ])('rejects malformed options: %s', (_name, malformed) => {
    expect(() => parseAssertionOptions(malformed)).toThrow(
      WebAuthnDataInvalid,
    );
  });
});

describe('requestAssertion', () => {
  afterEach(() => {
    Reflect.deleteProperty(navigator, 'credentials');
  });

  it('returns the credential navigator.credentials.get resolves', async () => {
    const credential = { id: 'stub' };
    Object.defineProperty(navigator, 'credentials', {
      value: { get: vi.fn().mockResolvedValue(credential) },
      configurable: true,
    });
    const options = parseAssertionOptions(VALID_PUBLIC_KEY);
    await expect(requestAssertion(options)).resolves.toBe(credential);
  });

  it('rejects with NotAllowedError when the browser resolves null',
    async () => {
      Object.defineProperty(navigator, 'credentials', {
        value: { get: vi.fn().mockResolvedValue(null) },
        configurable: true,
      });
      const options = parseAssertionOptions(VALID_PUBLIC_KEY);
      await expect(requestAssertion(options)).rejects.toMatchObject({
        name: 'NotAllowedError',
      });
    });

  it('propagates a cancellation DOMException from the browser', async () => {
    Object.defineProperty(navigator, 'credentials', {
      value: {
        get: vi.fn().mockRejectedValue(
          new DOMException('cancelled', 'NotAllowedError'),
        ),
      },
      configurable: true,
    });
    const options = parseAssertionOptions(VALID_PUBLIC_KEY);
    await expect(requestAssertion(options)).rejects.toThrow(DOMException);
  });
});

describe('isWebAuthnCancellation', () => {
  it('is true for NotAllowedError and AbortError', () => {
    expect(isWebAuthnCancellation(new DOMException('x', 'NotAllowedError')))
      .toBe(true);
    expect(isWebAuthnCancellation(new DOMException('x', 'AbortError')))
      .toBe(true);
  });

  it('is false for another DOMException name or a plain error', () => {
    expect(isWebAuthnCancellation(new DOMException('x', 'SecurityError')))
      .toBe(false);
    expect(isWebAuthnCancellation(new Error('boom'))).toBe(false);
    expect(isWebAuthnCancellation(null)).toBe(false);
  });
});

describe('assertionToJSON', () => {
  it('encodes the accepted fields and pins clientExtensionResults to {}',
    () => {
      const rawId = bytesOf(1, 2, 3).buffer;
      const clientDataJSON = bytesOf(4, 5).buffer;
      const authenticatorData = bytesOf(6, 7, 8).buffer;
      const signature = bytesOf(9).buffer;
      const userHandle = bytesOf(10, 11).buffer;
      const credential = {
        rawId,
        response: {
          clientDataJSON,
          authenticatorData,
          signature,
          userHandle,
          getClientExtensionResults: () => ({ credProps: { rk: true } }),
        },
      } as unknown as PublicKeyCredential;

      const json = assertionToJSON(credential);

      expect(json.type).toBe('public-key');
      expect(json.id).toBe(json.rawId);
      expect(Array.from(decodeBase64Url(json.id))).toEqual([1, 2, 3]);
      expect(Array.from(decodeBase64Url(json.response.clientDataJSON)))
        .toEqual([4, 5]);
      expect(Array.from(decodeBase64Url(json.response.authenticatorData)))
        .toEqual([6, 7, 8]);
      expect(Array.from(decodeBase64Url(json.response.signature)))
        .toEqual([9]);
      expect(json.response.userHandle).not.toBeNull();
      expect(Array.from(decodeBase64Url(json.response.userHandle as string)))
        .toEqual([10, 11]);
      // Fixed {} regardless of what the browser attaches — the server
      // accepts no extension output for this ceremony.
      expect(json.clientExtensionResults).toEqual({});
    });

  it('encodes a null userHandle as null', () => {
    const credential = {
      rawId: bytesOf(1).buffer,
      response: {
        clientDataJSON: bytesOf(1).buffer,
        authenticatorData: bytesOf(1).buffer,
        signature: bytesOf(1).buffer,
        userHandle: null,
      },
    } as unknown as PublicKeyCredential;

    expect(assertionToJSON(credential).response.userHandle).toBeNull();
  });
});
