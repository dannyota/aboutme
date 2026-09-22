import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { readBody, setResponseStatus, type H3Event } from 'h3';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import {
  createPasskeyCredential,
  downloadRecoveryCodes,
  mapPasskeyCompletionError,
  mapPasskeyOptionsError,
  mapPasskeyRemovalError,
  mapRecoveryRegenerationError,
  recoveryCodesDownloadText,
  type RegistrationPublicKeyInput,
} from '../app/composables/secondFactorSettings';
import {
  isWebAuthnCancellation,
  isWebAuthnSupported,
} from '../app/utils/webauthn';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

mockNuxtImport('navigateTo', () => vi.fn());

// --- Pure helper unit tests -------------------------------------------------

describe('recoveryCodesDownloadText', () => {
  const codes = Array.from({ length: 10 }, (_, i) => `amr_code-${i}`);

  it('uses the exact English byte contract', () => {
    expect(recoveryCodesDownloadText(codes, 'en')).toBe(
      `aboutme recovery codes\n\n${codes.join('\n')}\n`,
    );
  });

  it('uses the exact Vietnamese byte contract', () => {
    expect(recoveryCodesDownloadText(codes, 'vi')).toBe(
      `Mã khôi phục aboutme\n\n${codes.join('\n')}\n`,
    );
  });

  it('never inserts a byte-order mark or CRLF', () => {
    const text = recoveryCodesDownloadText(codes, 'en');
    expect(text.startsWith('﻿')).toBe(false);
    expect(text.includes('\r')).toBe(false);
    expect(text.endsWith('\n')).toBe(true);
    expect(text.endsWith('\n\n')).toBe(false);
  });
});

describe('downloadRecoveryCodes', () => {
  it('creates one object URL, clicks the anchor, then revokes it', () => {
    const createObjectURL = vi.fn(() => 'blob:recovery');
    const revokeObjectURL = vi.fn();
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(() => undefined);
    let capturedAnchor: HTMLAnchorElement | null = null;
    const originalCreateElement = document.createElement.bind(document);
    vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      const element = originalCreateElement(tag);
      if (tag === 'a') capturedAnchor = element as HTMLAnchorElement;
      return element;
    });

    downloadRecoveryCodes(['amr_a', 'amr_b'], 'en', {
      createObjectURL,
      revokeObjectURL,
    });

    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(capturedAnchor?.href).toBe('blob:recovery');
    expect(capturedAnchor?.download).toBe('aboutme-recovery-codes.txt');
    expect(clickSpy).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:recovery');

    vi.restoreAllMocks();
  });

  it('revokes the object URL even when the anchor click throws', () => {
    const createObjectURL = vi.fn(() => 'blob:recovery');
    const revokeObjectURL = vi.fn();
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {
      throw new Error('click failed');
    });

    expect(() =>
      downloadRecoveryCodes(['amr_a'], 'en', {
        createObjectURL,
        revokeObjectURL,
      })).toThrow('click failed');
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:recovery');

    vi.restoreAllMocks();
  });
});

describe('isWebAuthnCancellation', () => {
  it('is true for a browser-cancelled or aborted ceremony', () => {
    expect(isWebAuthnCancellation(new DOMException('x', 'NotAllowedError')))
      .toBe(true);
    expect(isWebAuthnCancellation(new DOMException('x', 'AbortError')))
      .toBe(true);
  });

  it('is false for any other error', () => {
    expect(isWebAuthnCancellation(new DOMException('x', 'SecurityError')))
      .toBe(false);
    expect(isWebAuthnCancellation(new Error('network'))).toBe(false);
    expect(isWebAuthnCancellation(null)).toBe(false);
  });
});

describe('isWebAuthnSupported', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('is false when the platform has no WebAuthn API', () => {
    expect(isWebAuthnSupported()).toBe(false);
  });

  it('is true once PublicKeyCredential and navigator.credentials exist', () => {
    vi.stubGlobal('PublicKeyCredential', function PublicKeyCredential() {});
    vi.stubGlobal('navigator', {
      ...navigator,
      credentials: { create: vi.fn(), get: vi.fn() },
    });
    expect(isWebAuthnSupported()).toBe(true);
  });
});

class FakePublicKeyCredential {
  id: string;
  rawId: ArrayBuffer;
  type = 'public-key';
  response: unknown;

  constructor(id: string, rawId: ArrayBuffer, response: unknown) {
    this.id = id;
    this.rawId = rawId;
    this.response = response;
  }
}

class FakeAttestationResponse {
  clientDataJSON: ArrayBuffer;
  attestationObject: ArrayBuffer;
  #transports: string[];

  constructor(
    clientDataJSON: ArrayBuffer,
    attestationObject: ArrayBuffer,
    transports: string[],
  ) {
    this.clientDataJSON = clientDataJSON;
    this.attestationObject = attestationObject;
    this.#transports = transports;
  }

  getTransports(): string[] {
    return this.#transports;
  }
}

function bytes(...values: number[]): Uint8Array {
  return new Uint8Array(values);
}

describe('createPasskeyCredential', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('decodes options and re-encodes the credential as base64url', async () => {
    vi.stubGlobal('PublicKeyCredential', FakePublicKeyCredential);
    vi.stubGlobal(
      'AuthenticatorAttestationResponse',
      FakeAttestationResponse,
    );
    let captured: CredentialCreationOptions | undefined;
    const create = vi.fn(async (options: CredentialCreationOptions) => {
      captured = options;
      return new FakePublicKeyCredential(
        'AQIDBA', // base64url("\x01\x02\x03\x04")
        bytes(1, 2, 3, 4).buffer,
        new FakeAttestationResponse(
          bytes(5, 6).buffer,
          bytes(7, 8, 9).buffer,
          ['internal', 'hybrid'],
        ),
      );
    });
    vi.stubGlobal('navigator', { ...navigator, credentials: { create } });

    const publicKey: RegistrationPublicKeyInput = {
      // base64url("\x00\x00\x00\x00") with 32 bytes trimmed for the test
      challenge: 'AAAAAA',
      rp: { name: 'aboutme', id: 'aboutme.vn' },
      user: { id: 'AQIDBA', name: 'ada@example.com', displayName: 'Ada' },
      pubKeyCredParams: [
        { type: 'public-key', alg: -7 },
        { type: 'public-key', alg: -257 },
      ],
      timeout: 300000,
      excludeCredentials: [{ type: 'public-key', id: 'AQIDBA' }],
      authenticatorSelection: {
        residentKey: 'required',
        requireResidentKey: true,
        userVerification: 'required',
      },
      attestation: 'none',
    };

    const credential = await createPasskeyCredential(publicKey);

    expect(create).toHaveBeenCalledTimes(1);
    const sentPublicKey = captured?.publicKey;
    expect(sentPublicKey?.challenge).toBeInstanceOf(Uint8Array);
    expect(Array.from(sentPublicKey?.challenge as Uint8Array)).toEqual([
      0, 0, 0, 0,
    ]);
    expect(Array.from(sentPublicKey?.user.id as Uint8Array)).toEqual([
      1, 2, 3, 4,
    ]);
    expect(
      Array.from(
        (sentPublicKey?.excludeCredentials as { id: Uint8Array }[])[0]!.id,
      ),
    ).toEqual([1, 2, 3, 4]);

    expect(credential).toEqual({
      id: 'AQIDBA',
      rawId: 'AQIDBA',
      type: 'public-key',
      response: {
        clientDataJSON: 'BQY',
        attestationObject: 'BwgJ',
        transports: ['internal', 'hybrid'],
      },
      clientExtensionResults: {},
    });
  });

  it('rejects when the browser returns an unusable credential', async () => {
    vi.stubGlobal('PublicKeyCredential', FakePublicKeyCredential);
    vi.stubGlobal('navigator', {
      ...navigator,
      credentials: { create: vi.fn(async () => null) },
    });
    await expect(createPasskeyCredential({
      challenge: 'AAAA',
      rp: { name: 'aboutme', id: 'aboutme.vn' },
      user: { id: 'AAAA', name: 'a@example.com', displayName: 'A' },
      pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
      timeout: 300000,
      excludeCredentials: [],
      authenticatorSelection: {
        residentKey: 'required',
        requireResidentKey: true,
        userVerification: 'required',
      },
      attestation: 'none',
    })).rejects.toThrow();
  });
});

describe('error mapping', () => {
  it.each([
    [403, 'reauth_required', 'reauth-required'],
    [404, 'not_found', 'enrollment-closed'],
    [409, 'passkey_limit_reached', 'limit-reached'],
    [429, 'rate_limited', 'rate-limited'],
    [500, 'internal', 'unavailable'],
  ] as const)('maps options %s/%s to %s', (status, code, expected) => {
    const failure = mapPasskeyOptionsError({
      statusCode: status,
      data: { error: { code } },
    });
    expect(failure.kind).toBe(expected);
  });

  it.each([
    [400, 'challenge_invalid', 'challenge-invalid'],
    [400, 'verification_failed', 'verification-failed'],
  ] as const)('maps completion %s/%s to %s', (status, code, expected) => {
    const failure = mapPasskeyCompletionError({
      statusCode: status,
      data: { error: { code } },
    });
    expect(failure.kind).toBe(expected);
  });

  it('maps a missing passkey removal to not-found', () => {
    expect(
      mapPasskeyRemovalError({ statusCode: 404, data: { error: {} } }).kind,
    ).toBe('not-found');
  });

  it('maps regeneration for an unenrolled account to not-found', () => {
    expect(
      mapRecoveryRegenerationError({ statusCode: 404, data: { error: {} } })
        .kind,
    ).toBe('not-found');
  });
});

// --- Integrated settings-page tests -----------------------------------------

interface PasskeyFixture {
  id: string;
  createdAt: string;
  lastUsedAt: string | null;
}

const meData = {
  user: {
    id: 'user-1',
    email: 'demo@example.com',
    name: 'Demo User',
    avatarKey: null,
    hasPassword: true,
  },
  csrfToken: 'csrf-1',
  identities: [] as { provider: string }[],
};

let meCalls = 0;
let sessionsCalls = 0;
let stateResponse: unknown;
let passkeys: PasskeyFixture[];
let recoveryCodesRemaining: number;
let enabled: boolean;
let passwordReauthResponse: () => unknown;

function resetFixtures(): void {
  meCalls = 0;
  sessionsCalls = 0;
  meData.identities = [];
  meData.user.hasPassword = true;
  passkeys = [
    { id: 'pk-1', createdAt: '2026-09-01T00:00:00Z', lastUsedAt: null },
  ];
  recoveryCodesRemaining = 10;
  enabled = true;
  stateResponse = undefined;
  passwordReauthResponse = () => null;
}

resetFixtures();

registerEndpoint('/api/v1/me', {
  method: 'GET',
  handler: () => {
    meCalls += 1;
    return { data: meData };
  },
});
registerEndpoint('/api/v1/sessions', () => {
  sessionsCalls += 1;
  return {
    data: [
      {
        id: 'sess-1',
        createdAt: '2026-07-01T00:00:00Z',
        lastSeenAt: '2026-09-01T00:00:00Z',
        ua: 'Chrome on macOS',
        ip: null,
        current: true,
      },
    ],
  };
});
registerEndpoint('/api/v1/me/second-factor', {
  method: 'GET',
  handler: (_event) => {
    if (stateResponse !== undefined) return stateResponse;
    return {
      data: { enabled, passkeys, recoveryCodesRemaining },
    };
  },
});
registerEndpoint('/api/v1/auth/password/reauth', {
  method: 'POST',
  handler: () => passwordReauthResponse(),
});
registerEndpoint('/api/v1/auth/google/start', {
  method: 'POST',
  handler: () => ({ data: { authorizeUrl: 'https://accounts.google.com/o/oauth2/v2/auth?state=x' } }),
});

/** Sets the status, then returns the closed `{error:{code,message}}` shape
 * every other test file in this suite uses to simulate a server rejection:
 * a plain success return leaves h3's default 200, so an error case must set
 * its own status before returning. */
function errorBody(
  event: H3Event,
  statusCode: number,
  code: string,
): { error: { code: string; message: string } } {
  setResponseStatus(event, statusCode);
  return { error: { code, message: code } };
}

let optionsResponse: (event: H3Event) => unknown;
let completionResponse: (event: H3Event, body: unknown) => unknown;
let removalResponses: Record<string, (event: H3Event) => unknown>;
let regenerateResponse: (event: H3Event) => unknown;

registerEndpoint('/api/v1/me/second-factor/passkeys/options', {
  method: 'POST',
  handler: (event) => optionsResponse(event),
});
registerEndpoint('/api/v1/me/second-factor/passkeys', {
  method: 'POST',
  handler: async (event) => completionResponse(event, await readBody(event)),
});
registerEndpoint('/api/v1/me/second-factor/recovery-codes', {
  method: 'POST',
  handler: (event) => regenerateResponse(event),
});

function registerRemoval(
  id: string,
  respond: (event: H3Event) => unknown,
): void {
  removalResponses[id] = respond;
  registerEndpoint(`/api/v1/me/second-factor/passkeys/${id}`, {
    method: 'DELETE',
    handler: (event) => removalResponses[id]!(event),
  });
}

function stubWebAuthnSupport(): void {
  vi.stubGlobal('PublicKeyCredential', FakePublicKeyCredential);
  vi.stubGlobal('navigator', {
    ...navigator,
    credentials: {
      // `isWebAuthnSupported` (utils/webauthn.ts) checks `.get`, the
      // assertion method it uses; only `.create` runs a registration here.
      get: vi.fn(),
      create: vi.fn(async () =>
        new FakePublicKeyCredential(
          'AQID',
          bytes(1, 2, 3).buffer,
          new FakeAttestationResponse(
            bytes(1).buffer,
            bytes(2).buffer,
            ['internal'],
          ),
        )),
    },
  });
}

let currentWrapper: Awaited<ReturnType<typeof mountSuspended>> | null = null;

/**
 * Mounts attached to `document.body`, the pattern `connected-agents.test.ts`
 * and `privacy-settings.test.ts` use, because `ConfirmDialog` and the
 * recovery-reveal `Dialog` both teleport to `document.body`: their content
 * is unreachable through `wrapper.get`/`wrapper.find`/`wrapper.text()`, and
 * `document.activeElement` only tracks a connected element's real focus.
 */
async function mountSettings(
  capabilities: { passkeyEnrollment?: boolean } = {},
) {
  registerCapabilities({
    providerLogin: true,
    agentAccess: false,
    ...capabilities,
  });
  const wrapper = await mountSuspended(SessionsPage, {
    route: '/app/settings/sessions',
    attachTo: document.body,
  });
  await flushPromises();
  currentWrapper = wrapper;
  return wrapper;
}

function dialogBody(): HTMLElement {
  const dialog = document.body.querySelector<HTMLElement>(
    '[role="dialog"], [role="alertdialog"]',
  );
  expect(dialog, 'no open dialog in document.body').not.toBeNull();
  return dialog!;
}

function clickInDialog(selector: string): void {
  const button = document.body.querySelector<HTMLButtonElement>(selector);
  expect(button, `missing ${selector} in document.body`).not.toBeNull();
  button!.click();
}

describe('second-factor settings', () => {
  beforeEach(() => {
    setSiteLocale('en');
    resetFixtures();
    optionsResponse = () => ({
      data: {
        ceremonyId: 'A'.repeat(43),
        publicKey: {
          challenge: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',
          rp: { name: 'aboutme', id: 'aboutme.vn' },
          user: { id: 'AAAA', name: 'demo@example.com', displayName: 'Demo' },
          pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
          timeout: 300000,
          excludeCredentials: [],
          authenticatorSelection: {
            residentKey: 'required',
            requireResidentKey: true,
            userVerification: 'required',
          },
          attestation: 'none',
        },
      },
    });
    completionResponse = () => ({
      data: {
        passkey: {
          id: 'pk-2',
          createdAt: '2026-09-10T00:00:00Z',
          lastUsedAt: null,
        },
        recoveryCodes: Array.from(
          { length: 10 },
          (_, i) => `amr_code-${i}0000-00000-00000-00000-0`,
        ),
      },
    });
    removalResponses = {};
    registerRemoval('pk-1', () => {
      passkeys = passkeys.filter((passkey) => passkey.id !== 'pk-1');
      return null;
    });
    regenerateResponse = () => ({
      data: {
        recoveryCodes: Array.from(
          { length: 10 },
          (_, i) => `amr_new-${i}00000-00000-00000-00000-0`,
        ),
      },
    });
    vi.mocked(navigateTo).mockReset();
    vi.mocked(navigateTo).mockResolvedValue(undefined);
  });

  afterEach(() => {
    currentWrapper?.unmount();
    currentWrapper = null;
    vi.unstubAllGlobals();
  });

  // --- capability ------------------------------------------------------

  it('hides add-passkey when enrollment is closed', async () => {
    const wrapper = await mountSettings({ passkeyEnrollment: false });
    expect(wrapper.find('[data-testid="passkey-add"]').exists()).toBe(false);
    expect(
      wrapper.find('[data-testid="passkey-row-pk-1"]').exists(),
    ).toBe(true);
    expect(
      wrapper.find('[data-testid="passkey-remove-pk-1"]').exists(),
    ).toBe(true);
    expect(
      wrapper.find('[data-testid="recovery-regenerate"]').exists(),
    ).toBe(true);
  });

  it('treats a malformed enrollment flag as closed', async () => {
    registerEndpoint('/api/v1/capabilities', () => ({
      data: {
        providerLogin: true,
        providers: ['google'],
        agentAccess: false,
        passwordRegistration: true,
        passkeyEnrollment: 'yes',
      },
    }));
    const wrapper = await mountSuspended(SessionsPage, {
      route: '/app/settings/sessions',
    });
    await flushPromises();
    expect(wrapper.find('[data-testid="passkey-add"]').exists()).toBe(false);
  });

  it('shows add-passkey once enrollment and WebAuthn are ready', async () => {
    stubWebAuthnSupport();
    const wrapper = await mountSettings({ passkeyEnrollment: true });
    expect(wrapper.find('[data-testid="passkey-add"]').exists()).toBe(true);
    expect(
      wrapper.find('[data-testid="second-factor-unsupported"]').exists(),
    ).toBe(false);
  });

  it('shows unsupported note when the browser lacks WebAuthn', async () => {
    const wrapper = await mountSettings({ passkeyEnrollment: true });
    expect(wrapper.find('[data-testid="passkey-add"]').exists()).toBe(false);
    expect(
      wrapper.find('[data-testid="second-factor-unsupported"]').exists(),
    ).toBe(true);
  });

  // --- state -------------------------------------------------------------

  it('lists passkeys in order with safe created/last-used times', async () => {
    passkeys = [
      { id: 'pk-1', createdAt: '2026-09-01T00:00:00Z', lastUsedAt: null },
      {
        id: 'pk-2',
        createdAt: '2026-09-05T00:00:00Z',
        lastUsedAt: '2026-09-10T00:00:00Z',
      },
    ];
    const wrapper = await mountSettings();
    expect(wrapper.get('[data-testid="passkey-row-pk-1"]').text())
      .toContain('Never');
    expect(wrapper.get('[data-testid="passkey-row-pk-2"]').text())
      .toContain('Last used');
    const rows = wrapper.findAll('[data-testid^="passkey-row-"]');
    expect(rows.map((row) => row.attributes('data-testid'))).toEqual([
      'passkey-row-pk-1',
      'passkey-row-pk-2',
    ]);
  });

  it('never renders an unexpected field from a passkey entry', async () => {
    stateResponse = {
      data: {
        enabled: true,
        passkeys: [
          {
            id: 'pk-1',
            createdAt: '2026-09-01T00:00:00Z',
            lastUsedAt: null,
            credentialId: 'should-never-render',
            publicKey: 'should-never-render-either',
          },
        ],
        recoveryCodesRemaining: 10,
      },
    };
    const wrapper = await mountSettings();
    expect(wrapper.find('[data-testid="passkey-row-pk-1"]').exists())
      .toBe(true);
    expect(wrapper.text()).not.toContain('should-never-render');
  });

  it('degrades a failed state read to the unenrolled view', async () => {
    stateResponse = { error: { code: 'internal', message: 'oops' } };
    const wrapper = await mountSettings();
    expect(wrapper.find('[data-testid="second-factor-empty"]').exists())
      .toBe(true);
    expect(
      wrapper.find('[data-testid="recovery-codes-section"]').exists(),
    ).toBe(false);
  });

  it('hides the recovery section for an unenrolled account', async () => {
    enabled = false;
    passkeys = [];
    recoveryCodesRemaining = 0;
    const wrapper = await mountSettings();
    expect(
      wrapper.find('[data-testid="recovery-codes-section"]').exists(),
    ).toBe(false);
  });

  // --- enrollment ----------------------------------------------------

  it('reveals ten codes on first enrollment, clears on close', async () => {
    passkeys = [];
    enabled = false;
    recoveryCodesRemaining = 0;
    stubWebAuthnSupport();
    const wrapper = await mountSettings({ passkeyEnrollment: true });
    const sessionsCallsBefore = sessionsCalls;

    await wrapper.get('[data-testid="passkey-add"]').trigger('click');
    await flushPromises();

    const list = document.body.querySelector(
      '[data-testid="recovery-codes-list"]',
    );
    expect(list?.querySelectorAll('li')).toHaveLength(10);
    expect(sessionsCalls).toBeGreaterThan(sessionsCallsBefore);

    clickInDialog('[data-testid="recovery-reveal-close"]');
    await flushPromises();
    expect(document.body.querySelector('[data-testid="recovery-reveal"]'))
      .toBeNull();
  });

  it('adding a later passkey shows success without a reveal', async () => {
    completionResponse = () => ({
      data: {
        passkey: {
          id: 'pk-2',
          createdAt: '2026-09-10T00:00:00Z',
          lastUsedAt: null,
        },
      },
    });
    stubWebAuthnSupport();
    const wrapper = await mountSettings({ passkeyEnrollment: true });

    await wrapper.get('[data-testid="passkey-add"]').trigger('click');
    await flushPromises();

    expect(document.body.querySelector('[data-testid="recovery-reveal"]'))
      .toBeNull();
    expect(wrapper.get('[data-testid="passkey-added-success"]').text())
      .toBe('Passkey added.');
  });

  it('surfaces a cancelled ceremony, not as an error', async () => {
    vi.stubGlobal('PublicKeyCredential', FakePublicKeyCredential);
    vi.stubGlobal('navigator', {
      ...navigator,
      credentials: {
        get: vi.fn(),
        create: vi.fn(async () => {
          throw new DOMException('cancelled', 'NotAllowedError');
        }),
      },
    });
    const wrapper = await mountSettings({ passkeyEnrollment: true });

    await wrapper.get('[data-testid="passkey-add"]').trigger('click');
    await flushPromises();

    expect(wrapper.get('[data-testid="passkey-add-error"]').text())
      .toBe('That was cancelled.');
    expect(
      wrapper.get('[data-testid="passkey-add"]').attributes('disabled'),
    ).toBeUndefined();
  });

  it('maps enrollment closed mid-flow to closed-enrollment', async () => {
    optionsResponse = (event) => errorBody(event, 404, 'not_found');
    stubWebAuthnSupport();
    const wrapper = await mountSettings({ passkeyEnrollment: true });
    await wrapper.get('[data-testid="passkey-add"]').trigger('click');
    await flushPromises();
    expect(wrapper.get('[data-testid="passkey-add-error"]').text())
      .toBe('Adding a new passkey is turned off right now.');
  });

  // --- removal -------------------------------------------------------

  it('asks a non-final confirmation and preserves recovery codes', async () => {
    passkeys = [
      { id: 'pk-1', createdAt: '2026-09-01T00:00:00Z', lastUsedAt: null },
      { id: 'pk-2', createdAt: '2026-09-02T00:00:00Z', lastUsedAt: null },
    ];
    registerRemoval('pk-1', () => {
      passkeys = passkeys.filter((passkey) => passkey.id !== 'pk-1');
      return null;
    });
    const wrapper = await mountSettings();
    const sessionsCallsBefore = sessionsCalls;

    await wrapper.get('[data-testid="passkey-remove-pk-1"]').trigger('click');
    await flushPromises();
    expect(dialogBody().textContent).toContain(
      'Remove this passkey? Every other device and connected agent will be '
      + 'signed out.',
    );

    clickInDialog('[data-action="passkey-remove-confirm"]');
    await flushPromises();

    expect(wrapper.get('[data-testid="passkey-removed-success"]').text())
      .toBe('Passkey removed.');
    expect(sessionsCalls).toBeGreaterThan(sessionsCallsBefore);
  });

  it('warns final removal disables second-factor sign-in', async () => {
    const wrapper = await mountSettings();
    await wrapper.get('[data-testid="passkey-remove-pk-1"]').trigger('click');
    await flushPromises();
    expect(dialogBody().textContent).toContain(
      'This is your last passkey. Removing it turns off two-factor sign-in '
      + 'and deletes your recovery codes.',
    );
  });

  it('maps a foreign or already-removed passkey id to not-found', async () => {
    registerRemoval(
      'pk-1',
      (event) => errorBody(event, 404, 'factor_not_found'),
    );
    const wrapper = await mountSettings();
    await wrapper.get('[data-testid="passkey-remove-pk-1"]').trigger('click');
    await flushPromises();
    clickInDialog('[data-action="passkey-remove-confirm"]');
    await flushPromises();
    expect(wrapper.get('[data-testid="passkey-remove-error"]').text())
      .toBe('That passkey could not be found.');
  });

  // --- recent reauthentication ----------------------------------------

  it('switches to password reauth, then lets the user retry', async () => {
    registerRemoval(
      'pk-1',
      (event) => errorBody(event, 403, 'reauth_required'),
    );
    const wrapper = await mountSettings();

    await wrapper.get('[data-testid="passkey-remove-pk-1"]').trigger('click');
    await flushPromises();
    clickInDialog('[data-action="passkey-remove-confirm"]');
    await flushPromises();

    expect(
      wrapper.find('[data-testid="second-factor-reauth-password"]').exists(),
    ).toBe(true);
    expect(
      document.body.querySelector('[data-action="passkey-remove-confirm"]'),
    ).toBeNull();

    passwordReauthResponse = () => null;
    const passwordInput = wrapper.get(
      '[data-testid="second-factor-reauth-password"] input[type="password"]',
    );
    await passwordInput.setValue('current-password');
    await wrapper.get('[data-testid="second-factor-reauth-submit"]')
      .trigger('click');
    await flushPromises();

    expect(
      wrapper.find('[data-testid="second-factor-reauth-password"]').exists(),
    ).toBe(false);
    expect(wrapper.find('[data-testid="passkey-remove-pk-1"]').exists())
      .toBe(true);
  });

  it(
    'navigates to the pending second-factor page when reauth itself needs a '
    + 'second factor',
    async () => {
      registerRemoval(
        'pk-1',
        (event) => errorBody(event, 403, 'reauth_required'),
      );
      const wrapper = await mountSettings();

      await wrapper.get('[data-testid="passkey-remove-pk-1"]')
        .trigger('click');
      await flushPromises();
      clickInDialog('[data-action="passkey-remove-confirm"]');
      await flushPromises();

      passwordReauthResponse = () => ({ data: { secondFactorRequired: true } });
      const passwordInput = wrapper.get(
        '[data-testid="second-factor-reauth-password"] input[type="password"]',
      );
      await passwordInput.setValue('current-password');
      await wrapper.get('[data-testid="second-factor-reauth-submit"]')
        .trigger('click');
      await flushPromises();

      expect(navigateTo).toHaveBeenCalledWith('/login/second-factor');
    },
  );

  it('offers the provider round trip for a provider-only account', async () => {
    meData.user.hasPassword = false;
    meData.identities = [{ provider: 'google' }];
    registerRemoval(
      'pk-1',
      (event) => errorBody(event, 403, 'reauth_required'),
    );
    const wrapper = await mountSettings();

    await wrapper.get('[data-testid="passkey-remove-pk-1"]').trigger('click');
    await flushPromises();
    clickInDialog('[data-action="passkey-remove-confirm"]');
    await flushPromises();

    expect(
      wrapper.find('[data-testid="second-factor-reauth-provider"]').exists(),
    ).toBe(true);
    await wrapper.get('[data-testid="second-factor-reauth-provider-google"]')
      .trigger('click');
    await flushPromises();

    expect(navigateTo).toHaveBeenCalledWith(
      'https://accounts.google.com/o/oauth2/v2/auth?state=x',
      { external: true },
    );
  });

  // --- recovery --------------------------------------------------------

  it('shows the remaining recovery-code count', async () => {
    recoveryCodesRemaining = 3;
    const wrapper = await mountSettings();
    expect(wrapper.get('[data-testid="recovery-codes-remaining"]').text())
      .toBe('3 recovery codes remaining.');
  });

  it('regenerating warns about other sessions, reveals codes', async () => {
    const wrapper = await mountSettings();
    const sessionsCallsBefore = sessionsCalls;
    await wrapper.get('[data-testid="recovery-regenerate"]').trigger('click');
    await flushPromises();
    expect(dialogBody().textContent).toContain(
      'Regenerating invalidates your old recovery codes and signs out every '
      + 'other device and connected agent.',
    );

    clickInDialog('[data-action="recovery-regenerate-confirm"]');
    await flushPromises();

    expect(dialogBody().textContent).toContain('Your new recovery codes');
    expect(
      document.body
        .querySelector('[data-testid="recovery-codes-list"]')
        ?.querySelectorAll('li'),
    ).toHaveLength(10);
    expect(sessionsCalls).toBeGreaterThan(sessionsCallsBefore);
  });

  it('maps regeneration on an unenrolled account to not-found', async () => {
    // Reachable only if the section briefly renders while enabled; a
    // concurrent disable races the button, so the handler must still map
    // the response instead of assuming success.
    regenerateResponse = (event) => errorBody(event, 404, 'factor_not_found');
    const wrapper = await mountSettings();
    await wrapper.get('[data-testid="recovery-regenerate"]').trigger('click');
    await flushPromises();
    clickInDialog('[data-action="recovery-regenerate-confirm"]');
    await flushPromises();
    expect(wrapper.get('[data-testid="recovery-regenerate-error"]').text())
      .toBe('That passkey could not be found.');
  });

  // --- cleanup -----------------------------------------------------------

  it('clears revealed codes on unmount', async () => {
    const wrapper = await mountSettings();
    await wrapper.get('[data-testid="recovery-regenerate"]').trigger('click');
    await flushPromises();
    clickInDialog('[data-action="recovery-regenerate-confirm"]');
    await flushPromises();
    expect(document.body.querySelector('[data-testid="recovery-reveal"]'))
      .not.toBeNull();

    wrapper.unmount();
    currentWrapper = null;
    // Unmounting tears down the teleported dialog along with the component;
    // nothing is left in `document.body` to hold the plaintext codes.
    expect(document.body.querySelector('[data-testid="recovery-reveal"]'))
      .toBeNull();
  });

  it('keeps a revealed set intact when the refresh fails', async () => {
    let stateCalls = 0;
    registerEndpoint('/api/v1/me/second-factor', {
      method: 'GET',
      handler: (event) => {
        stateCalls += 1;
        if (stateCalls > 1) return errorBody(event, 500, 'internal');
        return { data: { enabled, passkeys, recoveryCodesRemaining } };
      },
    });
    const wrapper = await mountSettings();
    await wrapper.get('[data-testid="recovery-regenerate"]').trigger('click');
    await flushPromises();
    clickInDialog('[data-action="recovery-regenerate-confirm"]');
    await flushPromises();

    expect(
      document.body
        .querySelector('[data-testid="recovery-codes-list"]')
        ?.querySelectorAll('li'),
    ).toHaveLength(10);
  });

  // --- locale ------------------------------------------------------------

  it('preserves an open reveal dialog across a locale change', async () => {
    const wrapper = await mountSettings();
    await wrapper.get('[data-testid="recovery-regenerate"]').trigger('click');
    await flushPromises();
    clickInDialog('[data-action="recovery-regenerate-confirm"]');
    await flushPromises();
    const codesBefore = document.body
      .querySelector('[data-testid="recovery-codes-list"]')!.textContent;

    const locale = useState<'vi' | 'en'>('aboutme-locale');
    locale.value = 'vi';
    await wrapper.vm.$nextTick();

    expect(dialogBody().textContent).toContain('Mã khôi phục mới của bạn');
    expect(
      document.body.querySelector('[data-testid="recovery-codes-list"]')
        ?.textContent,
    ).toBe(codesBefore);
  });

  it('preserves a reauth password draft across a locale change', async () => {
    registerRemoval(
      'pk-1',
      (event) => errorBody(event, 403, 'reauth_required'),
    );
    const wrapper = await mountSettings();
    await wrapper.get('[data-testid="passkey-remove-pk-1"]').trigger('click');
    await flushPromises();
    clickInDialog('[data-action="passkey-remove-confirm"]');
    await flushPromises();

    const passwordInput = wrapper.get(
      '[data-testid="second-factor-reauth-password"] input[type="password"]',
    );
    await passwordInput.setValue('draft-password');

    const locale = useState<'vi' | 'en'>('aboutme-locale');
    locale.value = 'vi';
    await wrapper.vm.$nextTick();

    expect(
      (passwordInput.element as HTMLInputElement).value,
    ).toBe('draft-password');
    expect(wrapper.get('h2#second-factor-title').text()).toBe('Passkey');
  });

  // --- accessibility -------------------------------------------------

  it('focuses the error banner when adding a passkey fails', async () => {
    // Adding has no confirmation step, so nothing else competes for focus
    // the way a closing `ConfirmDialog` restores focus to its opener.
    optionsResponse = (event) => errorBody(event, 500, 'internal');
    stubWebAuthnSupport();
    const wrapper = await mountSettings({ passkeyEnrollment: true });
    await wrapper.get('[data-testid="passkey-add"]').trigger('click');
    await flushPromises();

    const banner = wrapper.get('[data-testid="passkey-add-error"]');
    expect(banner.attributes('role')).toBe('alert');
    expect(banner.element).toBe(document.activeElement);
  });

  // --- session rotation ------------------------------------------------

  it('refetches account and device list after a mutation', async () => {
    const wrapper = await mountSettings();
    const meCallsBefore = meCalls;
    const sessionsCallsBefore = sessionsCalls;

    await wrapper.get('[data-testid="passkey-remove-pk-1"]').trigger('click');
    await flushPromises();
    clickInDialog('[data-action="passkey-remove-confirm"]');
    await flushPromises();

    expect(meCalls).toBeGreaterThan(meCallsBefore);
    expect(sessionsCalls).toBeGreaterThan(sessionsCallsBefore);
  });

  it('states in page copy that factor changes end other sessions', async () => {
    const wrapper = await mountSettings();
    expect(
      wrapper.get('[data-testid="second-factor-sessions-notice"]').text(),
    ).toBe(
      'Adding or removing a passkey, or regenerating recovery codes, signs '
      + 'out every other device and connected agent. This device stays '
      + 'signed in.',
    );
  });
});
