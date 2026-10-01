import { expect, test, type Page } from '@playwright/test';
import { randomBytes, randomUUID } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';

import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import {
  UnauthorizedError,
  type OAuthClientProvider,
} from '@modelcontextprotocol/sdk/client/auth.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import type {
  OAuthClientInformationFull,
  OAuthClientMetadata,
  OAuthTokens,
} from '@modelcontextprotocol/sdk/shared/auth.js';
import type { FetchLike } from '@modelcontextprotocol/sdk/shared/transport.js';

import { deleteRecordedResume } from './editor-fixtures';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  isUnexpectedConsoleError,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  pinEnglish,
  signInWithGoogle,
  waitForHydration,
} from './harness-lib';
import { ALLOWED_ORIGIN } from './network-policy';

// Drives the pinned official TypeScript MCP SDK client
// (@modelcontextprotocol/sdk) against the dev-https stack exactly as a
// Claude Code style client would: the SDK owns discovery, dynamic client
// registration, PKCE, token exchange, refresh, and Streamable HTTP framing.
// This proof only supplies the browser half of the human-in-the-loop steps
// (sign-in and consent) and the loopback redirect a native client would
// register. See docs/design/mcp-client-compatibility.md ("Proofs") and
// docs/adr/0018-mcp-agent-access.md.

const ORIGIN = ALLOWED_ORIGIN;
const CA_PATH = '/uat-input/caddy-root.crt';
const CLIENT_NAME_PATH = '/uat-input/mcp-client-name';
const EVIDENCE_PATH = '/evidence/mcp-ts-sdk-proof.json';
// A loopback host, not an IP literal, so this proof exercises the native
// redirect rule a Claude Code style client relies on
// (docs/design/mcp-client-compatibility.md, gap 3). Fixed and distinct from
// mcp.spec.ts's port so the two proofs never collide if ever run together.
const REDIRECT_PORT = 20097;
const REDIRECT_URI = `http://localhost:${REDIRECT_PORT}/callback`;
const UAT_ACCOUNT = 'MCP Proof — mcp-proof@example.invalid';
const RESUME_TITLE = 'MCP TS SDK Proof Resume';
const REFRESH_FORM_KEYS = ['client_id', 'grant_type', 'refresh_token', 'resource'];

const EXPECTED_TOOLS = [
  'create_resume',
  'delete_entry',
  'delete_photo',
  'delete_resume',
  'get_photo',
  'get_resume',
  'list_resumes',
  'update_customization',
  'update_personal_details',
  'update_photo_crop',
  'update_resume_metadata',
  'update_section',
  'update_structure',
  'upload_photo',
  'upsert_entry',
] as const;

function stage(name: string): void {
  console.log(`mcp-ts-sdk-stage:${name}`);
}

function object(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

// The native stack serves Nuxt in development mode, which compiles a route
// on its first visit; a fresh hosted runner can hydrate later than the
// default five-second expect timeout (mcp.spec.ts carries the same note).
const FIRST_VISIT_HYDRATION_MS = 40_000;

async function waitForFirstVisitHydration(page: Page): Promise<void> {
  await expect.poll(
    () => page.evaluate(() => Boolean(
      (document.getElementById('__nuxt') as HTMLElement & {
        __vue_app__?: unknown;
      } | null)?.__vue_app__,
    )),
    { timeout: FIRST_VISIT_HYDRATION_MS },
  ).toBe(true);
}

function minimalWorkDocument(): Record<string, unknown> {
  return {
    schemaVersion: 5,
    personalDetails: { fullName: 'Bob Local', details: [] },
    content: {
      work: { sectionType: 'work', iconKey: 'briefcase', entries: [] },
    },
    customization: {
      font: { family: 'inter', baseSizePx: 14 },
      colors: { primary: '#1a1a1a', text: '#1a1a1a', background: '#ffffff' },
      spacing: { sectionGap: 16, entryGap: 8, lineHeight: 1.4 },
      heading: { style: 'normal', showRule: false },
      layout: { columns: 1, sections: { main: ['work'], sidebar: [] } },
      sectionDisplay: {
        skill: { style: 'text' },
        language: { style: 'text' },
      },
      pageFormat: 'a4',
      dateFormat: 'MM/YYYY',
    },
  };
}

function mutationState(result: {
  readonly structuredContent?: unknown;
  readonly isError?: unknown;
}): { readonly revision: string; readonly state: Record<string, unknown> } {
  expect(result.isError ?? false).toBe(false);
  const structured = object(result.structuredContent);
  const state = object(structured?.state);
  const revision = structured?.revision;
  if (state === null || typeof revision !== 'string') {
    throw new Error('MCP mutation returned invalid structured content');
  }
  return { revision, state };
}

// RefreshObservation records only the form field names a refresh grant sent,
// never a token, code, or client value (docs/design/mcp-owner-workflow.md
// "Privacy, revocation, and evidence").
interface RefreshObservation {
  seen: boolean;
  keys: readonly string[];
}

// recordingFetch wraps the platform fetch so the transport, registration,
// discovery, token, and refresh calls all go through one function: the spot
// this proof needs to observe the refresh request's field names from, and
// the spot a custom CA trust could hook if the environment ever stopped
// trusting the harness root through NODE_EXTRA_CA_CERTS (run.sh sets it for
// this mode before Node starts).
function makeRecordingFetch(observation: RefreshObservation): FetchLike {
  return async (url, init) => {
    const target = url instanceof URL ? url : new URL(url);
    const body = init?.body;
    // The pinned SDK's executeTokenRequest (client/auth.js) posts token
    // requests with a URLSearchParams body, not a pre-encoded string; only
    // a hand-built request would use a string. Accept either so a real
    // refresh grant is never missed.
    const params = body instanceof URLSearchParams
      ? body
      : typeof body === 'string' ? new URLSearchParams(body) : null;
    if (
      target.origin === ORIGIN && target.pathname === '/oauth/token' &&
      params?.get('grant_type') === 'refresh_token'
    ) {
      observation.seen = true;
      observation.keys = [...params.keys()].sort();
    }
    return fetch(url, init);
  };
}

// ProofOAuthProvider is a minimal in-memory OAuthClientProvider: client
// information, tokens, the PKCE verifier, and the OAuth state all live only
// for this test's lifetime, matching a native client's own session-scoped
// storage (docs/design/mcp-client-compatibility.md "Evidence").
class ProofOAuthProvider implements OAuthClientProvider {
  private info: OAuthClientInformationFull | undefined;
  private tokenSet: OAuthTokens | undefined;
  private verifier: string | undefined;
  private readonly oauthState = randomBytes(24).toString('base64url');
  authorizationURL: URL | undefined;

  constructor(
    private readonly redirect: string,
    private readonly metadata: OAuthClientMetadata,
  ) {}

  get redirectUrl(): string {
    return this.redirect;
  }

  get clientMetadata(): OAuthClientMetadata {
    return this.metadata;
  }

  state(): string {
    return this.oauthState;
  }

  clientInformation(): OAuthClientInformationFull | undefined {
    return this.info;
  }

  saveClientInformation(info: OAuthClientInformationFull): void {
    this.info = info;
  }

  tokens(): OAuthTokens | undefined {
    return this.tokenSet;
  }

  saveTokens(tokens: OAuthTokens): void {
    this.tokenSet = tokens;
  }

  // invalidateCredentials clears the scope the SDK names after a closed
  // token error, so a dead refresh token is never retried a second time
  // (client/auth.js's `auth` wrapper retries authInternal once on
  // InvalidGrantError).
  invalidateCredentials(scope: 'all' | 'client' | 'tokens' | 'verifier'): void {
    if (scope === 'all' || scope === 'client') this.info = undefined;
    if (scope === 'all' || scope === 'tokens') this.tokenSet = undefined;
    if (scope === 'all' || scope === 'verifier') this.verifier = undefined;
  }

  redirectToAuthorization(url: URL): void {
    this.authorizationURL = url;
  }

  saveCodeVerifier(verifier: string): void {
    this.verifier = verifier;
  }

  codeVerifier(): string {
    if (this.verifier === undefined) throw new Error('no code verifier saved');
    return this.verifier;
  }
}

test('proves the official TypeScript MCP SDK client over trusted HTTPS', async ({
  context,
  page,
}) => {
  await pinEnglish(context);
  const counters = newDiagnosticCounters();
  const attachPageDiagnostics = pageDiagnosticsAttacher(counters, {
    countConsoleError: isUnexpectedConsoleError,
  });
  attachPageDiagnostics(page);
  context.on('page', attachPageDiagnostics);
  await installExternalRequestFirewall(context, counters);
  await installExternalWebSocketFirewall(context, counters);

  const clientName = (await readFile(CLIENT_NAME_PATH, 'utf8')).trim();
  expect(clientName).toMatch(
    /^aboutme MCP UAT [0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  );
  // Read only to prove the harness mounted the trust root this mode's CA
  // input requires; NODE_EXTRA_CA_CERTS, set by run.sh before Node started,
  // is what actually extends trust for every fetch below.
  await readFile(CA_PATH);

  const refresh: RefreshObservation = { seen: false, keys: [] };
  const provider = new ProofOAuthProvider(REDIRECT_URI, {
    client_name: clientName,
    redirect_uris: [REDIRECT_URI],
    grant_types: ['authorization_code', 'refresh_token'],
    response_types: ['code'],
    token_endpoint_auth_method: 'none',
    scope: 'resumes:read resumes:write',
    // An unknown member the server must ignore rather than reject
    // (docs/design/mcp-client-compatibility.md, gap 2); the SDK sends it
    // because real clients do.
    software_id: 'aboutme-mcp-ts-sdk-proof',
  } as OAuthClientMetadata);

  const client = new Client({ name: 'aboutme-mcp-ts-sdk-proof', version: '1.0.0' });
  const transport = new StreamableHTTPClientTransport(new URL('/mcp', ORIGIN), {
    authProvider: provider,
    fetch: makeRecordingFetch(refresh),
  });

  let resumeID: string | null = null;
  let teardownComplete = false;

  try {
    stage('discover-and-register');
    let unauthorized = false;
    try {
      await client.connect(transport);
    } catch (error) {
      if (!(error instanceof UnauthorizedError)) throw error;
      unauthorized = true;
    }
    expect(unauthorized).toBe(true);
    expect(provider.clientInformation()?.client_id).toMatch(/^[0-9a-f-]{36}$/i);
    expect(provider.authorizationURL).toBeDefined();
    const authorizeURL = provider.authorizationURL as URL;
    expect(authorizeURL.origin).toBe(ORIGIN);
    expect(authorizeURL.pathname).toBe('/oauth/authorize');
    expect(authorizeURL.searchParams.get('redirect_uri')).toBe(REDIRECT_URI);

    stage('authorize-redirect');
    let callbackURL: URL | null = null;
    await context.route(`${REDIRECT_URI}**`, async (route) => {
      callbackURL = new URL(route.request().url());
      await route.fulfill({
        body: 'Authorization complete.',
        contentType: 'text/plain',
        status: 200,
      });
    });
    const login = await page.goto(authorizeURL.href);
    expect(login?.status()).toBe(200);
    expect(new URL(page.url()).pathname).toBe('/login');

    stage('provider-login');
    await signInWithGoogle(page, {
      accountLabel: UAT_ACCOUNT,
      returnPath: '/authorize',
    });
    stage('provider-login-hydration');
    await waitForHydration(page);
    stage('provider-login-heading');
    await expect(
      page.getByRole('heading', {
        name: `Allow ${clientName} to edit your resumes?`,
        exact: true,
      }),
    ).toBeVisible();
    stage('provider-login-client');
    await expect(page.getByTestId('consent-client-name')).toHaveText(clientName);
    stage('provider-login-scopes');
    const permissions = page.getByTestId('consent-scopes');
    await expect(permissions).toContainText('Read resumes');
    await expect(permissions).toContainText('Write resumes');
    stage('provider-login-return-to');
    // The redirect is a loopback host, not an IP literal or an https host, so
    // the consent page must show the loopback line
    // (docs/design/mcp-client-compatibility.md, gap 6).
    await expect(page.getByTestId('consent-return-to')).toHaveText(
      'After you approve, you return to an app on this computer.',
    );

    stage('approve-consent');
    await Promise.all([
      page.waitForURL(
        (url) =>
          url.origin === new URL(REDIRECT_URI).origin &&
          url.pathname === new URL(REDIRECT_URI).pathname,
      ),
      page.getByRole('button', { name: 'Approve' }).click(),
    ]);
    expect(callbackURL).not.toBeNull();
    const resolvedCallback = callbackURL as URL;
    const code = resolvedCallback.searchParams.get('code');
    expect(code).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(resolvedCallback.searchParams.get('state')).toBe(provider.state());
    expect(resolvedCallback.searchParams.has('error')).toBe(false);
    const iss = resolvedCallback.searchParams.get('iss');
    expect(iss).toBe(ORIGIN);

    stage('exchange-token');
    await transport.finishAuth(code as string);
    // The failed first connect already aborted the original transport
    // (StreamableHTTPClientTransport never clears its internal abort
    // controller on close, so `start()` refuses a second call on the same
    // instance); a fresh transport over the same authProvider carries the
    // exchanged tokens into the retry the SDK's own finishAuth doc comment
    // describes, without repeating discovery or registration.
    const authenticated = new StreamableHTTPClientTransport(new URL('/mcp', ORIGIN), {
      authProvider: provider,
      fetch: makeRecordingFetch(refresh),
    });
    await client.connect(authenticated);
    expect(provider.tokens()?.access_token).toBeTruthy();

    stage('list-tools');
    const { tools } = await client.listTools();
    const names = tools.map((tool) => tool.name).sort();
    expect(names).toEqual([...EXPECTED_TOOLS].sort());
    expect(tools).toHaveLength(15);

    stage('create-resume');
    const created = mutationState(
      await client.callTool({
        name: 'create_resume',
        arguments: {
          idempotency_key: randomUUID(),
          title: RESUME_TITLE,
          document: minimalWorkDocument(),
        },
      }),
    );
    expect(created.revision).toBe('1');
    expect(created.state.id).toMatch(/^[0-9a-f-]{36}$/i);
    resumeID = created.state.id as string;

    stage('forced-refresh');
    const liveTokens = provider.tokens();
    if (liveTokens === undefined) throw new Error('no live tokens to force-expire');
    // Syntactically valid (same alphabet and shape a real access token uses)
    // but unknown to the server, so the next call must fail with a bearer
    // challenge the SDK resolves on its own
    // (docs/design/mcp-client-compatibility.md, gap 4).
    provider.saveTokens({
      ...liveTokens,
      access_token: randomBytes(32).toString('base64url'),
    });
    const listed = await client.callTool({ name: 'list_resumes', arguments: {} });
    expect(listed.isError ?? false).toBe(false);
    expect(refresh.seen).toBe(true);
    expect(refresh.keys).toEqual([...REFRESH_FORM_KEYS].sort());
    const refreshedAccessToken = provider.tokens()?.access_token;
    expect(refreshedAccessToken).toBeTruthy();
    expect(refreshedAccessToken).not.toBe(liveTokens.access_token);

    stage('revoke-grant');
    const settings = await page.goto('/app/settings/sessions');
    expect(settings?.status()).toBe(200);
    await waitForFirstVisitHydration(page);
    await expect(
      page.getByRole('heading', { name: 'Signed-in devices' }),
    ).toBeVisible();
    const grantRow = page.getByTestId('agent-row').filter({ hasText: clientName });
    await expect(grantRow).toHaveCount(1);
    const revoked = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return (
        response.request().method() === 'DELETE' &&
        url.origin === ORIGIN &&
        /^\/api\/v1\/me\/agents\/[0-9a-f-]{36}$/i.test(url.pathname)
      );
    });
    await grantRow.getByTestId('agent-revoke').click();
    const dialog = page.getByRole('alertdialog');
    await expect(dialog).toBeVisible();
    await dialog.getByRole('button', { name: 'Revoke access' }).click();
    expect((await revoked).status()).toBe(204);
    await expect(grantRow).toHaveCount(0);

    stage('reject-revoked-token');
    const revokedAccessToken = refreshedAccessToken as string;
    await expect(client.callTool({ name: 'list_resumes', arguments: {} }))
      .rejects.toThrow();

    const rawRejected = await fetch(new URL('/mcp', ORIGIN), {
      method: 'POST',
      headers: {
        Accept: 'application/json, text/event-stream',
        Authorization: `Bearer ${revokedAccessToken}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: 1,
        method: 'tools/list',
        params: {},
      }),
    });
    expect(rawRejected.status).toBe(401);

    stage('teardown');
    await deleteRecordedResume(page, resumeID);
    teardownComplete = true;
    resumeID = null;

    const { certificateErrors, consoleErrors, externalRequests, pageErrors } = counters;
    expect({ certificateErrors, consoleErrors, externalRequests, pageErrors }).toEqual({
      certificateErrors: 0,
      consoleErrors: 0,
      externalRequests: 0,
      pageErrors: 0,
    });

    await writeEvidence();
  } finally {
    if (!teardownComplete && resumeID !== null) {
      try {
        await deleteRecordedResume(page, resumeID);
      } catch {
        // The fixture cleanup is the final fail-closed cleanup for an
        // aborted proof.
      }
    }
    try {
      await client.close();
    } catch {
      // Best-effort: the client may already be unauthorized or disconnected.
    }
  }

  async function writeEvidence(): Promise<void> {
    await writeFile(
      EVIDENCE_PATH,
      `${JSON.stringify(
        {
          errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
          origin: ORIGIN,
          scenario: 'mcp-ts-sdk-agent-access',
          schemaVersion: 1,
          steps: {
            clientRegistered: true,
            authorizeRedirected: true,
            consentApproved: true,
            tokenExchanged: true,
            toolsListed: true,
            resumeCreated: true,
            refreshedAfterForcedExpiry: true,
            grantRevoked: true,
            revokedRejected: true,
          },
        },
        null,
        2,
      )}\n`,
      { flag: 'wx', mode: 0o600 },
    );
  }
});
