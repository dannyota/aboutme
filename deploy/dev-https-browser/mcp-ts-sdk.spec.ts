import { expect, test, type Page, type Route } from '@playwright/test';
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

const ORIGIN = ALLOWED_ORIGIN;
const CA_PATH = '/uat-input/caddy-root.crt';
const CLIENT_NAME_PATH = '/uat-input/mcp-client-name';
const EVIDENCE_PATH = '/evidence/mcp-ts-sdk-proof.json';
const REDIRECT_PORT = 20097;
const REDIRECT_URI = `http://localhost:${REDIRECT_PORT}/callback`;
const UAT_ACCOUNT = 'MCP Proof — mcp-proof@example.invalid';
const RESUME_TITLE = 'MCP TS SDK Proof Resume';
const REFRESH_FORM_KEYS = ['client_id', 'grant_type', 'refresh_token', 'resource'];
const ALL_SCOPES = 'resumes:read resumes:write projects:read projects:write';

const EXPECTED_TOOLS = [
  'create_project',
  'create_resume',
  'delete_entry',
  'delete_photo',
  'delete_project',
  'delete_resume',
  'get_photo',
  'get_project',
  'get_resume',
  'link_project',
  'list_projects',
  'list_resumes',
  'refresh_project_entry',
  'update_customization',
  'update_personal_details',
  'update_photo_crop',
  'update_project',
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
    schemaVersion: 6,
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

interface RefreshObservation {
  seen: boolean;
  keys: readonly string[];
  cimd?: { clientID: string; discovered: boolean; codeExchanged: boolean; refreshed: boolean };
}

function makeRecordingFetch(observation: RefreshObservation): FetchLike {
  return async (url, init) => {
    const target = url instanceof URL ? url : new URL(url);
    const body = init?.body;
    // The pinned SDK's executeTokenRequest posts a URLSearchParams body, not a string (@modelcontextprotocol/sdk client/auth.js).
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
    if (observation.cimd !== undefined && target.origin === ORIGIN) {
      if (target.pathname === '/oauth/register') {
        throw new Error('CIMD client attempted dynamic registration');
      }
      if (target.pathname === '/oauth/token') {
        expect(params?.get('client_id')).toBe(observation.cimd.clientID);
        if (params?.get('grant_type') === 'authorization_code') observation.cimd.codeExchanged = true;
        if (params?.get('grant_type') === 'refresh_token') observation.cimd.refreshed = true;
      }
    }
    const response = await fetch(url, init);
    if (observation.cimd !== undefined && target.origin === ORIGIN
      && target.pathname === '/.well-known/oauth-authorization-server') {
      const metadata = await response.clone().json();
      expect(metadata.client_id_metadata_document_supported).toBe(true);
      expect(metadata.token_endpoint_auth_methods_supported).toContain('none');
      observation.cimd.discovered = true;
    }
    return response;
  };
}

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

  // The SDK's auth wrapper retries authInternal once on InvalidGrantError (@modelcontextprotocol/sdk client/auth.js).
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
  await readFile(CA_PATH);

  const refresh: RefreshObservation = { seen: false, keys: [] };
  const provider = new ProofOAuthProvider(REDIRECT_URI, {
    client_name: clientName,
    redirect_uris: [REDIRECT_URI],
    grant_types: ['authorization_code', 'refresh_token'],
    response_types: ['code'],
    token_endpoint_auth_method: 'none',
    scope: ALL_SCOPES,
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
        name: `Allow ${clientName} to edit your resumes and projects?`,
        exact: true,
      }),
    ).toBeVisible();
    stage('provider-login-client');
    await expect(page.getByTestId('consent-client-name')).toHaveText(clientName);
    stage('provider-login-scopes');
    const permissions = page.getByTestId('consent-scopes');
    await expect(permissions).toContainText('Read resumes');
    await expect(permissions).toContainText('Write resumes');
    await expect(permissions).toContainText('Read projects');
    await expect(permissions).toContainText('Write projects');
    stage('provider-login-return-to');
    await expect(page.getByTestId('consent-return-to')).toHaveText(
      'After you approve, you return to an app on this computer (localhost).',
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
    // StreamableHTTPClientTransport never clears its abort controller on close, so start() refuses a second call (@modelcontextprotocol/sdk).
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
    expect(tools).toHaveLength(22);

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

    await proveCIMDPresentation(page);

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

    await proveCIMDSDK(page, clientName);

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
      }
    }
    try {
      await client.close();
    } catch {
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
            cimdDiscovered: true,
            cimdConsentApproved: true,
            cimdTokenExchanged: true,
            cimdToolsListed: true,
            cimdRefreshed: true,
            cimdGrantRevoked: true,
            cimdRevokedRejected: true,
          },
        },
        null,
        2,
      )}\n`,
      { flag: 'wx', mode: 0o600 },
    );
  }
});

async function proveCIMDPresentation(page: Page): Promise<void> {
  const host = `${'a'.repeat(60)}.client.example`;
  const name = '<img src=x>\u202EClaude';
  const decisionValues: string[] = [];
  const originalViewport = page.viewportSize();
  const consentPattern = /\/api\/v1\/oauth\/consent(?:\?|$)/;
  const grantPattern = /\/api\/v1\/me\/agents$/;
  const consentRoute = async (route: Route): Promise<void> => {
    if (new URL(route.request().url()).origin !== ORIGIN) {
      await route.fallback();
      return;
    }
    if (route.request().method() === 'POST') {
      decisionValues.push(route.request().postDataJSON().decision);
      await route.fulfill({
        json: { data: { redirectTo: `${ORIGIN}/app/settings/sessions` } },
      });
      return;
    }
    await route.fulfill({
      json: {
        data: {
          clientKind: 'cimd', clientHost: host, clientName: name,
          scopes: ['resumes:read'],
        },
      },
    });
  };
  const grantRoute = async (route: Route): Promise<void> => {
    if (new URL(route.request().url()).origin !== ORIGIN) {
      await route.fallback();
      return;
    }
    await route.fulfill({
      json: { data: { grants: [{
        id: randomUUID(), clientKind: 'cimd', clientHost: host,
        clientName: name, scopes: ['resumes:read'],
        createdAt: '2026-10-14T00:00:00Z', lastUsedAt: null,
      }] } },
    });
  };
  await page.route(consentPattern, consentRoute);
  await page.route(grantPattern, grantRoute);
  try {
    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      for (const locale of ['vi', 'en'] as const) {
        await page.context().addCookies([
          { name: 'aboutme-locale', value: locale, url: ORIGIN },
        ]);
        const query = new URLSearchParams({
          client_id: `https://${host}/metadata`, redirect_uri: REDIRECT_URI,
          response_type: 'code', scope: 'resumes:read',
          code_challenge: 'A'.repeat(43), code_challenge_method: 'S256',
        });
        await page.goto(`/authorize?${query}`);
        await waitForHydration(page);
        await expect(page.getByRole('heading', { level: 1 })).toHaveText(
          locale === 'vi'
            ? `${host} muốn truy cập CV của bạn.`
            : `${host} wants access to your resumes.`,
        );
        await expect(page.getByTestId('consent-client-host'))
          .toHaveAttribute('dir', 'ltr');
        await expect(page.getByTestId('consent-client-name')).toHaveText(name);
        await expect(page.getByTestId('authorize-page').locator('img'))
          .toHaveCount(0);
        await expect(page.getByTestId('consent-loopback-warning')).toHaveText(
          locale === 'vi'
            ? 'Chỉ cho phép nếu bạn vừa bắt đầu đăng nhập từ một ứng dụng trên máy tính này.'
            : 'Approve only if you just started this sign-in from an app on this computer.',
        );
        await expect(page.getByTestId('consent-return-to')).toContainText(
          '(localhost).',
        );
        expect(await page.evaluate(() =>
          document.documentElement.scrollWidth <= window.innerWidth,
        )).toBe(true);
        const decision = locale === 'vi' ? 'approve' : 'deny';
        await page.locator(`[data-decision="${decision}"]`).focus();
        await Promise.all([
          page.waitForURL('**/app/settings/sessions'),
          page.keyboard.press('Enter'),
        ]);
        const agent = page.getByTestId('agent-row');
        await expect(agent.getByRole('heading', { level: 3 }))
          .toHaveText(`${name} (${host})`);
        const agentHost = agent.getByTestId('agent-client-host');
        await expect(agentHost).toHaveText(`(${host})`);
        await expect(agentHost).toHaveAttribute('dir', 'ltr');
        expect(await agentHost.textContent()).toBe(` (${host})`);
      }
    }
    expect(decisionValues).toEqual(['approve', 'deny', 'approve', 'deny']);
  } finally {
    await page.unroute(consentPattern, consentRoute);
    await page.unroute(grantPattern, grantRoute);
    await pinEnglish(page.context());
    if (originalViewport !== null) await page.setViewportSize(originalViewport);
  }
}

async function proveCIMDSDK(page: Page, clientName: string): Promise<void> {
  const clientID = `https://client.example/oauth/${clientName.slice('aboutme MCP UAT '.length)}`;
  const provider = new ProofOAuthProvider(REDIRECT_URI, {
    client_name: clientName, redirect_uris: [REDIRECT_URI],
    grant_types: ['authorization_code', 'refresh_token'], response_types: ['code'],
    token_endpoint_auth_method: 'none', scope: ALL_SCOPES,
  });
  provider.saveClientInformation({ ...provider.clientMetadata, client_id: clientID });
  const observation: RefreshObservation = {
    seen: false, keys: [],
    cimd: { clientID, discovered: false, codeExchanged: false, refreshed: false },
  };
  const recordingFetch = makeRecordingFetch(observation);
  const client = new Client({ name: 'aboutme-mcp-cimd-proof', version: '1.0.0' });
  const transport = new StreamableHTTPClientTransport(new URL('/mcp', ORIGIN), {
    authProvider: provider, fetch: recordingFetch,
  });
  let callback: URL | undefined;
  const callbackRoute = async (route: Route): Promise<void> => {
    callback = new URL(route.request().url());
    await route.fulfill({ status: 200, contentType: 'text/plain', body: 'Authorization complete.' });
  };
  await page.route(`${REDIRECT_URI}**`, callbackRoute);
  try {
    stage('cimd-discovery');
    await expect(client.connect(transport)).rejects.toBeInstanceOf(UnauthorizedError);
    expect(observation.cimd?.discovered).toBe(true);
    expect(provider.clientInformation()?.client_id).toBe(clientID);
    const authorization = provider.authorizationURL;
    if (authorization === undefined) throw new Error('CIMD authorization URL missing');
    expect(authorization.searchParams.get('client_id')).toBe(clientID);
    expect(authorization.searchParams.get('redirect_uri')).toBe(REDIRECT_URI);
    stage('cimd-consent');
    await page.goto(authorization.href);
    await waitForHydration(page);
    await expect(page.getByRole('heading', { level: 1 }))
      .toHaveText('client.example wants access to your resumes and projects.');
    await expect(page.getByTestId('consent-client-host')).toHaveText('client.example');
    await expect(page.getByTestId('consent-client-name')).toHaveText(clientName);
    await expect(page.getByTestId('consent-loopback-warning')).toBeVisible();
    await expect(page.getByTestId('consent-return-to')).toHaveText(
      'After you approve, you return to an app on this computer (localhost).',
    );
    await Promise.all([
      page.waitForURL(`${REDIRECT_URI}**`),
      page.getByRole('button', { name: 'Approve', exact: true }).click(),
    ]);
    if (callback === undefined) throw new Error('CIMD callback missing');
    expect(callback.searchParams.get('state')).toBe(provider.state());
    expect(callback.searchParams.get('iss')).toBe(ORIGIN);
    const code = callback.searchParams.get('code');
    if (code === null) throw new Error('CIMD code missing');
    stage('cimd-token');
    await transport.finishAuth(code);
    expect(observation.cimd?.codeExchanged).toBe(true);
    await client.connect(new StreamableHTTPClientTransport(new URL('/mcp', ORIGIN), {
      authProvider: provider, fetch: recordingFetch,
    }));
    expect((await client.listTools()).tools.map((tool) => tool.name).sort())
      .toEqual([...EXPECTED_TOOLS].sort());
    stage('cimd-refresh');
    const tokens = provider.tokens();
    if (tokens === undefined) throw new Error('CIMD tokens missing');
    provider.saveTokens({ ...tokens, access_token: randomBytes(32).toString('base64url') });
    expect((await client.callTool({ name: 'list_resumes', arguments: {} })).isError ?? false).toBe(false);
    expect(observation.cimd?.refreshed).toBe(true);
    expect(observation.keys).toEqual([...REFRESH_FORM_KEYS].sort());
    const rotated = provider.tokens();
    if (rotated === undefined) throw new Error('CIMD refreshed tokens missing');
    expect(rotated.access_token).not.toBe(tokens.access_token);
    stage('cimd-revoke');
    await page.goto('/app/settings/sessions');
    await waitForFirstVisitHydration(page);
    const grant = page.getByTestId('agent-row').filter({ hasText: clientName });
    await expect(grant).toHaveCount(1);
    await expect(grant.getByRole('heading', { level: 3 }))
      .toHaveText(`${clientName} (client.example)`);
    const host = grant.getByTestId('agent-client-host');
    await expect(host).toHaveText('(client.example)');
    expect(await host.textContent()).toBe(' (client.example)');
    const revoked = page.waitForResponse((response) =>
      response.request().method() === 'DELETE'
      && new URL(response.url()).origin === ORIGIN
      && /^\/api\/v1\/me\/agents\/[0-9a-f-]{36}$/i.test(new URL(response.url()).pathname));
    await grant.getByTestId('agent-revoke').click();
    await page.getByRole('alertdialog').getByRole('button', { name: 'Revoke access' }).click();
    expect((await revoked).status()).toBe(204);
    await expect(grant).toHaveCount(0);
    const rejected = await fetch(new URL('/mcp', ORIGIN), {
      method: 'POST', headers: {
        Accept: 'application/json, text/event-stream',
        Authorization: `Bearer ${rotated.access_token}`, 'Content-Type': 'application/json',
      }, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/list', params: {} }),
    });
    expect(rejected.status).toBe(401);
    const refreshRejected = await fetch(new URL('/oauth/token', ORIGIN), {
      method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({
        grant_type: 'refresh_token', client_id: clientID,
        refresh_token: rotated.refresh_token ?? '', resource: new URL('/mcp', ORIGIN).href,
      }),
    });
    expect(refreshRejected.status).toBe(400);
    expect((await refreshRejected.json()).error).toBe('invalid_grant');
  } finally {
    await page.unroute(`${REDIRECT_URI}**`, callbackRoute);
    await client.close().catch(() => undefined);
  }
}
