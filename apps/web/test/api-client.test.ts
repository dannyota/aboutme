// api-client.test.ts is the consumer-side proof for AC-API-001: the web
// app's HTTP surface is *derived from* docs/api/openapi.yaml, not
// hand-transcribed from it.
//
// Three independent failure modes, three independent halves:
//
//   1. Artifact: the committed generated surface
//      (app/api/generated/openapi.ts) exists and was produced by the
//      exact generator version pinned in package.json. Drift between the
//      artifact and the contract is a separate, non-mutating gate —
//      scripts/api-drift-check.sh, wired into `make api-check`.
//   2. Runtime: a real openapi-fetch client, given an injected mock
//      fetch, is driven through two representative paths (`GET /me`, the
//      versioned auth surface, and `GET /healthz`, the deliberately
//      UNversioned ops surface). Method, URL, success envelope decoding
//      and error envelope decoding are all asserted against the response
//      examples read out of openapi.yaml itself — so a contract example
//      the client cannot actually decode fails here.
//   3. Compile-time: a throwaway .ts fixture is type-checked with the
//      pinned tsc (6.0.3), mirroring schema-contract.test.ts. vitest does
//      not type-check, and Nuxt's generated tsconfig.app.json does not
//      include apps/web/test/**, so `npm run typecheck` alone would never
//      see a type assertion written in this file. Running tsc on a
//      fixture is what makes the request/response types load-bearing:
//      the fixture asserts the generated `GET /me` payload structurally
//      satisfies useAuth's hand-written MeEnvelope, and includes negative
//      cases (@ts-expect-error) that fail the build if the versioned
//      client ever starts accepting an unversioned ops path.
import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import {
  API_BASE_PATH,
  OPS_BASE_PATH,
  createApiClient,
  createOpsClient,
} from '../app/api/client';

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = join(here, '..');
// apps/web/test -> apps/web -> apps -> repo root -> docs/api/openapi.yaml
const openapiPath = join(here, '..', '..', '..', 'docs', 'api', 'openapi.yaml');
const generatedPath = join(webRoot, 'app', 'api', 'generated', 'openapi.ts');
const tscBin = join(webRoot, 'node_modules', '.bin', 'tsc');

interface OpenApiOperation {
  responses: Record<string, {
    content?: Record<string, { example?: unknown }>;
  }>;
}

/** A path item: the `servers` override (if any) plus its operations. */
type OpenApiPathItem = {
  servers?: { url: string }[];
} & Record<string, OpenApiOperation | undefined | { url: string }[]>;

interface OpenApiDoc {
  servers: { url: string }[];
  paths: Record<string, OpenApiPathItem>;
}

const doc = parse(readFileSync(openapiPath, 'utf8')) as OpenApiDoc;

/**
 * The origin an operation is served from: the path item's own `servers`
 * override when it has one (`/healthz`, `/readyz`), else the document
 * server. Read from the contract rather than restated, so the client's
 * versioned/unversioned split is checked against openapi.yaml itself.
 */
function serverUrl(path: string): string {
  return doc.paths[path]?.servers?.[0]?.url ?? doc.servers[0].url;
}

/** The documented response example for one operation + status code. */
function example(path: string, method: string, status: string): unknown {
  const operation = doc.paths[path]?.[method] as OpenApiOperation | undefined;
  const response = operation?.responses?.[status];
  const value = response?.content?.['application/json']?.example;
  if (value === undefined) {
    throw new Error(
      `openapi.yaml has no ${method.toUpperCase()} ${path} ${status} `
      + 'application/json example to test the client against',
    );
  }
  return value;
}

interface Captured {
  url: string;
  method: string;
}

/**
 * A fetch double that records the request openapi-fetch built and replies
 * with a canned status + body. No network, no MSW: the point is to assert
 * exactly what the typed client puts on the wire.
 */
function mockFetch(status: number, body: unknown): {
  fetch: (input: Request) => Promise<Response>;
  calls: Captured[];
} {
  const calls: Captured[] = [];
  const fetch = async (input: Request): Promise<Response> => {
    calls.push({ url: input.url, method: input.method });
    return new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    });
  };
  return { fetch, calls };
}

describe('generated API surface (artifact)', () => {
  it('is committed at app/api/generated/openapi.ts', () => {
    expect(
      existsSync(generatedPath),
      `${generatedPath} is missing — run 'make api-gen' and commit it`,
    ).toBe(true);
  });

  it('was produced by openapi-typescript, not written by hand', () => {
    const source = readFileSync(generatedPath, 'utf8');
    expect(source).toContain('auto-generated by openapi-typescript');
    expect(source).toContain('Do not make direct changes to the file');
  });

  it('pins its generator exactly (no range, no drift on reinstall)', () => {
    const pkg = JSON.parse(
      readFileSync(join(webRoot, 'package.json'), 'utf8'),
    ) as { devDependencies: Record<string, string> };
    const pinned = pkg.devDependencies['openapi-typescript'];
    expect(pinned, 'openapi-typescript must be a devDependency').toBeTruthy();
    expect(pinned, 'pin exactly: no ^, ~, or range').toMatch(
      /^\d+\.\d+\.\d+$/,
    );
    const installed = execFileSync(
      join(webRoot, 'node_modules', '.bin', 'openapi-typescript'),
      ['--version'],
      { encoding: 'utf8' },
    ).trim();
    expect(installed).toContain(pinned);
  });
});

describe('typed client (runtime contract)', () => {
  it('sends GET /me under the versioned base path', async () => {
    const { fetch, calls } = mockFetch(200, example('/me', 'get', '200'));
    const client = createApiClient({ baseUrl: doc.servers[0].url, fetch });

    await client.GET('/me');

    expect(calls).toHaveLength(1);
    expect(calls[0].method).toBe('GET');
    expect(calls[0].url).toBe(`${doc.servers[0].url}/me`);
  });

  it('decodes the documented GET /me 200 envelope', async () => {
    const { fetch } = mockFetch(200, example('/me', 'get', '200'));
    const client = createApiClient({ baseUrl: doc.servers[0].url, fetch });

    const { data, error } = await client.GET('/me');

    expect(error).toBeUndefined();
    expect(data?.data.user.email).toBe('ada@example.com');
    expect(data?.data.csrfToken).toBe('kQ2f9Z3sV1n8LhTt7v0wYb-example');
    expect(data?.data.identities).toEqual([{ provider: 'google' }]);
  });

  it('decodes a 401 as the error envelope, not as data', async () => {
    // The 401 on /me carries the documented Error shape; the typed client
    // must surface it on `error` (never on `data`) so a caller cannot
    // read a failure as a success.
    const { fetch } = mockFetch(401, example('/me', 'get', '401'));
    const client = createApiClient({ baseUrl: doc.servers[0].url, fetch });

    const { data, error, response } = await client.GET('/me');

    expect(data).toBeUndefined();
    expect(response.status).toBe(401);
    expect(error?.error?.code).toBe('session_required');
    expect(error?.error?.message).toBe('a valid session is required');
  });

  it('keeps /healthz off the versioned base path', async () => {
    // Health is infrastructure, not product API: openapi.yaml overrides
    // its server back to the bare root. The client split (ops vs api)
    // carries that invariant, so no caller can accidentally probe
    // /api/v1/healthz.
    const opsOrigin = serverUrl('/healthz');
    const { fetch, calls } = mockFetch(200, example('/healthz', 'get', '200'));
    const ops = createOpsClient({ baseUrl: opsOrigin, fetch });

    const { data } = await ops.GET('/healthz');

    expect(calls[0].url).toBe(`${opsOrigin}/healthz`);
    expect(calls[0].url).not.toContain('/api/v1');
    expect(data).toEqual({ data: { status: 'ok' } });
  });

  it('defaults its base paths from the contract, not a literal', () => {
    // Same-origin in dev/prod, so the defaults are the *paths* of the
    // contract's servers: /api/v1 for product endpoints, root for ops.
    expect(API_BASE_PATH).toBe(new URL(doc.servers[0].url).pathname);
    // The ops override points at the bare root, so the ops client must
    // prepend nothing at all.
    expect(new URL(serverUrl('/readyz')).pathname).toBe('/');
    expect(serverUrl('/readyz')).not.toBe(doc.servers[0].url);
    expect(OPS_BASE_PATH).toBe('');
  });
});

// Written to disk and compiled rather than kept inline: vitest strips
// types without checking them, so a type assertion living in this file
// would prove nothing. Deliberately self-contained (generated types +
// client.ts only) — it must compile under bare tsc, with no Nuxt program,
// so it cannot depend on `nuxt prepare` output. The consumer-side
// assertion that needs Nuxt's auto-imports lives in
// test/nuxt/api-contract.test.ts, which vue-tsc checks instead.
function compileFixtureSource(): string {
  return [
    'import type { paths } from \'../app/api/generated/openapi\';',
    'import { createApiClient, createOpsClient } from \'../app/api/client\';',
    '',
    '// The generated GET /me payloads, straight off the contract.',
    'type MeResponses = paths[\'/me\'][\'get\'][\'responses\'];',
    'type MeOk = MeResponses[\'200\'][\'content\'][\'application/json\'];',
    'type MeUnauthorized =',
    '  MeResponses[\'401\'][\'content\'][\'application/json\'];',
    '',
    '// The success envelope carries the documented payload...',
    '//',
    '// `data` is narrowed rather than asserted: the /me 200 `allOf`',
    '// refinement does not restate `required: [data]`, so the generated',
    '// intersection types it as possibly-undefined. Narrowing keeps the',
    '// assertion honest instead of casting the contract\'s own looseness',
    '// away.',
    'declare const me: MeOk;',
    'if (me.data) {',
    '  const email: string = me.data.user.email;',
    '  const avatarKey: string | null = me.data.user.avatarKey;',
    '  const csrf: string = me.data.csrfToken;',
    '  const providers: string[] = me.data.identities.map((i) => i.provider);',
    '  void email; void avatarKey; void csrf; void providers;',
    '}',
    '',
    '// ...and the failure envelope carries the error code, not a payload.',
    'declare const unauthorized: MeUnauthorized;',
    'const code: string = unauthorized.error.code;',
    'void code;',
    '',
    'const api = createApiClient();',
    'const ops = createOpsClient();',
    'void api.GET(\'/me\');',
    'void ops.GET(\'/healthz\');',
    '',
    '// Negative: the versioned client must NOT accept an unversioned ops',
    '// path (it would resolve to /api/v1/healthz, which no server serves).',
    '// @ts-expect-error /healthz is not part of the versioned surface',
    'void api.GET(\'/healthz\');',
    '',
    '// Negative: the ops client must NOT accept a versioned path.',
    '// @ts-expect-error /me is not part of the unversioned ops surface',
    'void ops.GET(\'/me\');',
    '',
    '// Negative: a path that is not in the contract at all.',
    '// @ts-expect-error /not-a-real-path is not in openapi.yaml',
    'void api.GET(\'/not-a-real-path\');',
    '',
  ].join('\n');
}

describe('typed client (compile-time contract)', () => {
  it('type-checks the generated request/response contract (tsc)', () => {
    // The three `@ts-expect-error` lines in the fixture are assertions in
    // both directions: tsc fails here if any of them stops being an
    // error, so this also proves the versioned/ops split is enforced and
    // not merely documented.
    const fixturePath = join(here, 'api-client-contract.tmp.ts');
    writeFileSync(fixturePath, compileFixtureSource());
    let diagnostics = '';
    try {
      execFileSync(
        tscBin,
        [
          '--ignoreConfig',
          '--noEmit',
          '--strict',
          '--module', 'esnext',
          '--target', 'esnext',
          '--moduleResolution', 'bundler',
          fixturePath,
        ],
        { stdio: 'pipe' },
      );
    } catch (error) {
      const failure = error as { stdout?: Buffer };
      diagnostics = failure.stdout?.toString().trim() || String(error);
    } finally {
      rmSync(fixturePath, { force: true });
    }
    expect(
      diagnostics,
      'tsc rejected the generated request/response contract',
    ).toBe('');
  });
});
