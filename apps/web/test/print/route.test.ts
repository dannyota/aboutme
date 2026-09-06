// @vitest-environment node

import { createServer, request, type Server } from 'node:http';

import { createApp, toNodeListener } from 'h3';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  createPrintHandler,
  PRINT_NOT_FOUND_BODY,
} from '../../server/utils/print/handler';
import {
  PRINT_CONTENT_SECURITY_POLICY,
} from '../../server/utils/print/protocol';
import {
  CAPABILITY,
  JOB_ID,
  printEnvelope,
  RESUME_ID,
} from './fixture';

const servers: Server[] = [];

afterEach(async () => {
  await Promise.all(
    servers.splice(0).map((server) => new Promise<void>((resolve) => {
      server.close(() => resolve());
    })),
  );
});

const start = async (
  transport: typeof fetch,
  run = vi.fn(async () => '<!doctype html><title>Resume</title>'),
): Promise<{ run: typeof run; url: string }> => {
  const app = createApp();
  app.use(createPrintHandler({
    origin: 'http://127.0.0.1:20082',
    transport,
    run,
    workerUrl: new URL('file:///print-worker.mjs'),
  }));
  const server = createServer(toNodeListener(app));
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, resolve));
  const address = server.address();
  if (address === null || typeof address === 'string') throw new Error('port');
  return { run, url: `http://127.0.0.1:${address.port}` };
};

const authority = {
  'authorization': `RenderCapability ${CAPABILITY}`,
  'x-render-job-id': JOB_ID,
};

const envelopeResponse = (resumeId = RESUME_ID): Response => {
  const body = JSON.stringify({ ...printEnvelope(), resumeId });
  return new Response(body, {
    status: 200,
    headers: {
      'cache-control': 'no-store',
      'content-length': String(Buffer.byteLength(body)),
      'content-type': 'application/json',
    },
  });
};

const rawRequest = async (
  url: string,
  options: {
    method?: string;
    headers?: Record<string, string | string[]>;
  } = {},
  body?: string,
): Promise<{ body: string; headers: Headers; status: number }> =>
  new Promise((resolve, reject) => {
    const target = new URL(url);
    const outgoing = request({
      host: target.hostname,
      port: target.port,
      path: `${target.pathname}${target.search}`,
      method: options.method ?? 'GET',
      headers: options.headers,
    }, (incoming) => {
      const chunks: Buffer[] = [];
      incoming.on('data', (chunk) => chunks.push(Buffer.from(chunk)));
      incoming.on('end', () => resolve({
        body: Buffer.concat(chunks).toString('utf8'),
        headers: new Headers(incoming.headers as Record<string, string>),
        status: incoming.statusCode ?? 0,
      }));
    });
    outgoing.on('error', reject);
    if (body !== undefined) outgoing.write(body);
    outgoing.end();
  });

describe('private print route', () => {
  it('returns the generic print 404 for a bare resume ID', async () => {
    const transport = vi.fn<typeof fetch>();
    const { run, url } = await start(transport);
    const response = await fetch(
      `${url}/print/10000000-0000-4000-8000-000000000001`,
    );

    expect(response.status).toBe(404);
    expect(response.headers.get('cache-control')).toBe('no-store');
    expect(await response.text()).toBe(PRINT_NOT_FOUND_BODY);
    expect(transport).not.toHaveBeenCalled();
    expect(run).not.toHaveBeenCalled();
  });

  it('redeems once and returns only the joined worker HTML', async () => {
    const transport = vi.fn<typeof fetch>(async () => envelopeResponse());
    const { run, url } = await start(transport);
    const response = await fetch(`${url}/print/${RESUME_ID}`, {
      headers: authority,
    });

    expect(response.status).toBe(200);
    expect(await response.text()).toBe('<!doctype html><title>Resume</title>');
    expect(transport).toHaveBeenCalledTimes(1);
    expect(run).toHaveBeenCalledTimes(1);
    expect(run.mock.calls[0]?.[0]).toEqual(printEnvelope());
    expect(run.mock.calls[0]?.[1]).toMatchObject({
      deadlineMs: 5_000,
      workerUrl: new URL('file:///print-worker.mjs'),
    });
    expect(response.headers.get('cache-control')).toBe('no-store');
    expect(response.headers.get('content-type'))
      .toBe('text/html; charset=utf-8');
    expect(response.headers.get('content-security-policy'))
      .toBe(PRINT_CONTENT_SECURITY_POLICY);
    expect(response.headers.get('x-content-type-options')).toBe('nosniff');
    expect(response.headers.get('referrer-policy')).toBe('no-referrer');
  });

  it('rejects malformed paths, headers, cookies, queries, and bodies first',
    async () => {
      const cases: Array<{
        path: string;
        headers?: Record<string, string | string[]>;
        body?: string;
      }> = [
        { path: `/print/${RESUME_ID}?query=1`, headers: authority },
        { path: `/print/${RESUME_ID.toUpperCase()}`, headers: authority },
        { path: `/print/${RESUME_ID}/extra`, headers: authority },
        { path: `/print/${RESUME_ID}`, headers: { ...authority, cookie: '' } },
        { path: `/print/${RESUME_ID}`, headers: {
          ...authority,
          authorization: [authority.authorization, authority.authorization],
        } },
        { path: `/print/${RESUME_ID}`, headers: {
          ...authority,
          'x-render-job-id': [JOB_ID, JOB_ID],
        } },
        { path: `/print/${RESUME_ID}`, headers: {
          ...authority,
          authorization: `Bearer ${CAPABILITY}`,
        } },
        { path: `/print/${RESUME_ID}`, headers: {
          ...authority,
          'x-render-job-id': '00000000-0000-0000-0000-000000000000',
        } },
        { path: `/print/${RESUME_ID}`, headers: {
          ...authority,
          'content-length': '1',
        }, body: 'x' },
      ];
      for (const value of cases) {
        const transport = vi.fn<typeof fetch>();
        const { run, url } = await start(transport);
        const response = await rawRequest(
          `${url}${value.path}`,
          { headers: value.headers },
          value.body,
        );
        expect(response.status).toBe(404);
        expect(response.body).toBe(PRINT_NOT_FOUND_BODY);
        expect(transport).not.toHaveBeenCalled();
        expect(run).not.toHaveBeenCalled();
      }
    },
  );

  it('returns 405 with Allow for other methods before redemption', async () => {
    const transport = vi.fn<typeof fetch>();
    const { run, url } = await start(transport);
    const response = await rawRequest(`${url}/print/${RESUME_ID}`, {
      method: 'POST',
      headers: authority,
    });
    expect(response.status).toBe(405);
    expect(response.headers.get('allow')).toBe('GET');
    expect(transport).not.toHaveBeenCalled();
    expect(run).not.toHaveBeenCalled();
  });

  it(
    'rejects a returned resume mismatch before starting a worker',
    async () => {
      const transport = vi.fn<typeof fetch>(async () =>
        envelopeResponse('30000000-0000-4000-8000-000000000003'));
      const { run, url } = await start(transport);
      const response = await fetch(`${url}/print/${RESUME_ID}`, {
        headers: authority,
      });
      expect(response.status).toBe(404);
      expect(await response.text()).toBe(PRINT_NOT_FOUND_BODY);
      expect(run).not.toHaveBeenCalled();
    },
  );

  it('maps redemption and worker details to the same generic 404', async () => {
    const redemption = await start(vi.fn<typeof fetch>(async () => {
      throw new Error(`secret ${CAPABILITY}`);
    }));
    const worker = await start(
      vi.fn<typeof fetch>(async () => envelopeResponse()),
      vi.fn(async () => {
        throw new Error(`document ${RESUME_ID}`);
      }),
    );
    for (const url of [redemption.url, worker.url]) {
      const response = await fetch(`${url}/print/${RESUME_ID}`, {
        headers: authority,
      });
      expect(response.status).toBe(404);
      expect(await response.text()).toBe(PRINT_NOT_FOUND_BODY);
    }
  });
});
