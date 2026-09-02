import { expect, test, type Page } from '@playwright/test';
import { createHash, randomBytes, randomUUID } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { request as httpsRequest, type IncomingHttpHeaders } from 'node:https';

import { deleteRecordedResume } from './editor-fixtures';
import {
  installExternalRequestFirewall,
  installExternalWebSocketFirewall,
  isUnexpectedConsoleError,
  newDiagnosticCounters,
  pageDiagnosticsAttacher,
  signInWithGoogle,
  waitForHydration,
} from './harness-lib';
import { ALLOWED_ORIGIN } from './network-policy';

const ORIGIN = ALLOWED_ORIGIN;
const EVIDENCE_PATH = '/evidence/mcp-proof.json';
const CA_PATH = '/uat-input/caddy-root.crt';
const CLIENT_NAME_PATH = '/uat-input/mcp-client-name';
const REDIRECT_URI = 'http://127.0.0.1:20090/callback';
const UAT_ACCOUNT = 'Bob Local — bob@example.invalid';
const ENTRY_TITLE = 'MCP UAT Entry';
const ENTRY_ID = '53000000-0000-4000-8000-000000000021';
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

interface TrustedResponse {
  readonly body: string;
  readonly headers: IncomingHttpHeaders;
  readonly status: number;
}

interface JSONRPCResponse {
  readonly error?: unknown;
  readonly id?: unknown;
  readonly jsonrpc?: unknown;
  readonly result?: unknown;
}

interface MCPCallResult {
  readonly isError?: unknown;
  readonly structuredContent?: unknown;
}

function object(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function stage(name: string): void {
  console.log(`mcp-stage:${name}`);
}

function trustedRequest(
  ca: Buffer,
  path: string,
  method: 'GET' | 'POST',
  headers: Readonly<Record<string, string>>,
  body = '',
): Promise<TrustedResponse> {
  if (!path.startsWith('/') || path.startsWith('//')) {
    return Promise.reject(new Error('trusted request path rejected'));
  }
  return new Promise((resolve, reject) => {
    const request = httpsRequest(
      {
        ca,
        headers,
        hostname: 'localhost',
        method,
        path,
        port: 20443,
        protocol: 'https:',
      },
      (response) => {
        const chunks: Buffer[] = [];
        let size = 0;
        response.on('data', (chunk: Buffer) => {
          size += chunk.length;
          if (size > 4 * 1024 * 1024) {
            request.destroy(new Error('trusted response exceeded bound'));
            return;
          }
          chunks.push(chunk);
        });
        response.on('end', () => {
          resolve({
            body: Buffer.concat(chunks).toString('utf8'),
            headers: response.headers,
            status: response.statusCode ?? 0,
          });
        });
      },
    );
    request.on('error', reject);
    if (body !== '') request.write(body);
    request.end();
  });
}

function parseJSON(body: string, label: string): Record<string, unknown> {
  try {
    const parsed = object(JSON.parse(body));
    if (parsed !== null) return parsed;
  } catch {
    // Report a fixed error that cannot expose a token, code, or response body.
  }
  throw new Error(`${label} returned invalid JSON`);
}

function parseMCPResponse(response: TrustedResponse): JSONRPCResponse {
  const contentType = String(response.headers['content-type'] ?? '');
  let raw = response.body;
  if (contentType.startsWith('text/event-stream')) {
    const data = raw
      .split(/\r?\n/)
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).trim())
      .filter(Boolean)
      .at(-1);
    if (data === undefined) throw new Error('MCP returned empty event stream');
    raw = data;
  } else if (!contentType.startsWith('application/json')) {
    throw new Error('MCP returned an invalid content type');
  }
  return parseJSON(raw, 'MCP') as JSONRPCResponse;
}

async function mcpRequest(
  ca: Buffer,
  accessToken: string,
  id: number,
  method: string,
  params: Record<string, unknown>,
): Promise<Record<string, unknown>> {
  const body = JSON.stringify({ jsonrpc: '2.0', id, method, params });
  const response = await trustedRequest(
    ca,
    '/mcp',
    'POST',
    {
      Accept: 'application/json, text/event-stream',
      Authorization: `Bearer ${accessToken}`,
      'Content-Type': 'application/json',
    },
    body,
  );
  expect(response.status).toBe(200);
  const envelope = parseMCPResponse(response);
  expect(envelope.jsonrpc).toBe('2.0');
  expect(envelope.id).toBe(id);
  expect(envelope.error).toBeUndefined();
  const result = object(envelope.result);
  expect(result).not.toBeNull();
  return result as Record<string, unknown>;
}

async function callTool(
  ca: Buffer,
  accessToken: string,
  id: number,
  name: string,
  args: Record<string, unknown>,
): Promise<MCPCallResult> {
  const result = await mcpRequest(ca, accessToken, id, 'tools/call', {
    arguments: args,
    name,
  });
  expect(result.isError ?? false).toBe(false);
  return result;
}

function mutation(result: MCPCallResult): {
  readonly revision: string;
  readonly state: Record<string, unknown>;
} {
  const structured = object(result.structuredContent);
  const state = object(structured?.state);
  const revision = structured?.revision;
  if (state === null || typeof revision !== 'string') {
    throw new Error('MCP mutation returned invalid structured content');
  }
  return { revision, state };
}

function minimalWorkDocument(): Record<string, unknown> {
  return {
    schemaVersion: 2,
    personalDetails: { fullName: 'Bob Local', details: [] },
    content: {
      work: {
        sectionType: 'work',
        iconKey: 'briefcase',
        entries: [],
      },
    },
    customization: {
      font: { family: 'inter', baseSizePx: 14 },
      colors: {
        primary: '#1a1a1a',
        text: '#1a1a1a',
        background: '#ffffff',
      },
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

async function bestEffortDelete(
  page: Page,
  resumeID: string | null,
): Promise<void> {
  if (resumeID === null) return;
  try {
    await deleteRecordedResume(page, resumeID);
  } catch {
    // The fixture cleanup is the final fail-closed cleanup for an aborted proof.
  }
}

test('proves MCP agent access over trusted HTTPS', async ({
  context,
  page,
}) => {
  const counters = newDiagnosticCounters();
  const attachPageDiagnostics = pageDiagnosticsAttacher(counters, {
    countConsoleError: isUnexpectedConsoleError,
  });
  attachPageDiagnostics(page);
  context.on('page', attachPageDiagnostics);
  await installExternalRequestFirewall(context, counters);
  await installExternalWebSocketFirewall(context, counters);

  const ca = await readFile(CA_PATH);
  const clientName = (await readFile(CLIENT_NAME_PATH, 'utf8')).trim();
  expect(clientName).toMatch(
    /^aboutme MCP UAT [0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  );
  let resumeID: string | null = null;
  let teardownComplete = false;
  const createIdempotencyKey = randomUUID();
  const upsertIdempotencyKey = randomUUID();

  try {
    stage('register-client');
    const registrationBody = JSON.stringify({
      client_name: clientName,
      redirect_uris: [REDIRECT_URI],
      token_endpoint_auth_method: 'none',
    });
    const registration = await trustedRequest(
      ca,
      '/oauth/register',
      'POST',
      {
        'Content-Type': 'application/json',
      },
      registrationBody,
    );
    expect(registration.status).toBe(201);
    const registered = parseJSON(registration.body, 'registration');
    const clientID = registered.client_id;
    expect(clientID).toMatch(/^[0-9a-f-]{36}$/i);
    expect(registered).toEqual({
      client_id: clientID,
      client_name: clientName,
      redirect_uris: [REDIRECT_URI],
      token_endpoint_auth_method: 'none',
    });

    stage('authorize-redirect');
    const verifier = randomBytes(48).toString('base64url');
    const challenge = createHash('sha256').update(verifier).digest('base64url');
    const oauthState = randomBytes(24).toString('base64url');
    const authorize = new URL('/oauth/authorize', ORIGIN);
    authorize.search = new URLSearchParams({
      client_id: clientID as string,
      redirect_uri: REDIRECT_URI,
      response_type: 'code',
      scope: 'resumes:read resumes:write',
      state: oauthState,
      code_challenge: challenge,
      code_challenge_method: 'S256',
    }).toString();

    let callbackURL: URL | null = null;
    await context.route(`${REDIRECT_URI}**`, async (route) => {
      callbackURL = new URL(route.request().url());
      await route.fulfill({
        body: 'Authorization complete.',
        contentType: 'text/plain',
        status: 200,
      });
    });
    const login = await page.goto(authorize.href);
    expect(login?.status()).toBe(200);
    expect(new URL(page.url()).pathname).toBe('/login');
    const next = new URL(page.url()).searchParams.get('next');
    expect(next).not.toBeNull();
    const returnedAuthorize = new URL(next as string, ORIGIN);
    expect(returnedAuthorize.origin).toBe(ORIGIN);
    expect(returnedAuthorize.pathname).toBe('/oauth/authorize');
    expect(Object.fromEntries(returnedAuthorize.searchParams)).toEqual(
      Object.fromEntries(authorize.searchParams),
    );

    stage('provider-login');
    await signInWithGoogle(page, {
      accountLabel: UAT_ACCOUNT,
      returnPath: '/authorize',
    });
    await waitForHydration(page);
    await expect(
      page.getByRole('heading', { name: 'Allow access' }),
    ).toBeVisible();
    await expect(page.getByTestId('consent-client-name')).toHaveText(
      clientName,
    );
    await expect(
      page.getByRole('list', { name: 'Requested permissions' }),
    ).toContainText('Read resumes');
    await expect(
      page.getByRole('list', { name: 'Requested permissions' }),
    ).toContainText('Write resumes');

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
    const code = callbackURL?.searchParams.get('code');
    expect(code).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(callbackURL?.searchParams.get('state')).toBe(oauthState);
    expect(callbackURL?.searchParams.has('error')).toBe(false);

    stage('exchange-token');
    const tokenBody = new URLSearchParams({
      grant_type: 'authorization_code',
      code: code as string,
      redirect_uri: REDIRECT_URI,
      client_id: clientID as string,
      code_verifier: verifier,
    }).toString();
    const tokenResponse = await trustedRequest(
      ca,
      '/oauth/token',
      'POST',
      {
        'Content-Type': 'application/x-www-form-urlencoded',
      },
      tokenBody,
    );
    expect(tokenResponse.status).toBe(200);
    expect(tokenResponse.headers['cache-control']).toBe('no-store');
    const tokens = parseJSON(tokenResponse.body, 'token exchange');
    const accessToken = tokens.access_token;
    expect(typeof accessToken).toBe('string');
    expect(tokens.token_type).toBe('Bearer');
    expect(tokens.expires_in).toBe(3600);
    expect(tokens.scope).toBe('resumes:read resumes:write');
    expect(typeof tokens.refresh_token).toBe('string');

    stage('list-tools');
    await mcpRequest(ca, accessToken as string, 1, 'initialize', {});
    const list = await mcpRequest(
      ca,
      accessToken as string,
      2,
      'tools/list',
      {},
    );
    const tools = Array.isArray(list.tools) ? list.tools : [];
    const names = tools
      .map((tool) => object(tool)?.name)
      .filter((name): name is string => typeof name === 'string')
      .sort();
    expect(names).toEqual([...EXPECTED_TOOLS]);
    expect(tools).toHaveLength(15);

    stage('create-resume');
    const created = mutation(
      await callTool(ca, accessToken as string, 3, 'create_resume', {
        idempotency_key: createIdempotencyKey,
        title: 'MCP UAT Resume',
        document: minimalWorkDocument(),
      }),
    );
    expect(created.revision).toBe('1');
    expect(created.state.id).toMatch(/^[0-9a-f-]{36}$/i);
    resumeID = created.state.id as string;

    stage('replay-create-resume');
    const replayed = mutation(
      await callTool(ca, accessToken as string, 4, 'create_resume', {
        idempotency_key: createIdempotencyKey,
        title: 'MCP UAT Resume',
        document: minimalWorkDocument(),
      }),
    );
    expect(replayed.state.id).toBe(resumeID);
    expect(replayed.revision).toBe(created.revision);
    const listedAfterReplay = await callTool(
      ca,
      accessToken as string,
      5,
      'list_resumes',
      {},
    );
    const listedState = object(listedAfterReplay.structuredContent)?.resumes;
    expect(Array.isArray(listedState)).toBe(true);
    expect(listedState).toHaveLength(1);

    stage('upsert-entry');
    const upserted = mutation(
      await callTool(ca, accessToken as string, 6, 'upsert_entry', {
        idempotency_key: upsertIdempotencyKey,
        resume_id: resumeID,
        revision: created.revision,
        section_key: 'work',
        entry: {
          id: ENTRY_ID,
          jobTitle: ENTRY_TITLE,
          employer: 'Trusted local agent',
        },
      }),
    );
    expect(upserted.revision).toBe('2');

    stage('open-editor');
    const editor = await page.goto(`/app/resumes/${resumeID}`);
    expect(editor?.status()).toBe(200);
    stage('hydrate-editor');
    await waitForHydration(page);
    stage('verify-editor-content');
    await page
      .getByRole('navigation', { name: 'Resume outline' })
      .getByRole('button', { name: 'Experience' })
      .click();
    stage('verify-entry-value');
    const agentEntry = page.locator(`[data-entry-id="${ENTRY_ID}"]`);
    await expect(agentEntry).toBeVisible();
    await expect(
      agentEntry.getByRole('heading', { name: ENTRY_TITLE }),
    ).toBeVisible();

    stage('revoke-grant');
    const settings = await page.goto('/app/settings/sessions');
    expect(settings?.status()).toBe(200);
    await waitForHydration(page);
    await expect(
      page.getByRole('heading', { name: 'Signed-in devices' }),
    ).toBeVisible();
    const grantRow = page
      .getByTestId('agent-row')
      .filter({ hasText: clientName });
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
    const dialog = page.getByRole('alertdialog', { name: 'Revoke access' });
    await expect(dialog).toBeVisible();
    await dialog.getByRole('button', { name: 'Revoke access' }).click();
    expect((await revoked).status()).toBe(204);
    await expect(grantRow).toHaveCount(0);

    stage('reject-revoked-token');
    const revokedBody = JSON.stringify({
      jsonrpc: '2.0',
      id: 7,
      method: 'tools/list',
      params: {},
    });
    const rejected = await trustedRequest(
      ca,
      '/mcp',
      'POST',
      {
        Accept: 'application/json, text/event-stream',
        Authorization: `Bearer ${accessToken as string}`,
        'Content-Type': 'application/json',
      },
      revokedBody,
    );
    expect(rejected.status).toBe(401);
    expect(rejected.body).toBe('{"error":"unauthorized"}');

    stage('teardown');
    await deleteRecordedResume(page, resumeID);
    teardownComplete = true;
    resumeID = null;

    const { certificateErrors, consoleErrors, externalRequests, pageErrors } =
      counters;
    expect({
      certificateErrors,
      consoleErrors,
      externalRequests,
      pageErrors,
    }).toEqual({
      certificateErrors: 0,
      consoleErrors: 0,
      externalRequests: 0,
      pageErrors: 0,
    });
    expect(teardownComplete).toBe(true);

    await writeFile(
      EVIDENCE_PATH,
      `${JSON.stringify(
        {
          errors: { certificate: 0, console: 0, externalRequest: 0, page: 0 },
          origin: ORIGIN,
          scenario: 'mcp-agent-access',
          schemaVersion: 1,
          steps: {
            clientRegistered: true,
            authorizeRedirected: true,
            consentApproved: true,
            tokenExchanged: true,
            toolsListed: true,
            resumeCreated: true,
            entryUpserted: true,
            editorVisible: true,
            grantRevoked: true,
            revokedRejected: true,
          },
        },
        null,
        2,
      )}\n`,
      { flag: 'wx', mode: 0o600 },
    );
  } finally {
    if (!teardownComplete) await bestEffortDelete(page, resumeID);
  }
});
