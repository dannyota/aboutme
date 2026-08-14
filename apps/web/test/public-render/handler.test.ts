// @vitest-environment node

import { readFileSync } from 'node:fs';
import { createServer, type Server } from 'node:http';
import { resolve } from 'node:path';
import { createApp, toNodeListener } from 'h3';
import { afterEach, describe, expect, it } from 'vitest';

import {
  createPublicRenderHandler,
} from '../../server/utils/public-render/handler';
import {
  PUBLIC_RENDER_REQUEST_MAX_BYTES,
} from '../../server/utils/public-render/envelope';
import {
  PublicRenderDeadlineError,
  type runPublicRenderWorker,
} from '../../server/utils/public-render/runner';

const servers: Server[] = [];

afterEach(async () => {
  await Promise.all(
    servers
      .splice(0)
      .map(
        (server) =>
          new Promise<void>((resolveClose) =>
            server.close(() => resolveClose()),
          ),
      ),
  );
});

const document = JSON.parse(
  readFileSync(
    resolve(process.cwd(), '../../packages/schema/fixtures/minimal.json'),
    'utf8',
  ),
);
document.content = {
  profile: {
    sectionType: 'profile',
    entries: [{ id: '00000000-0000-4000-8000-000000000001' }],
  },
};
document.customization.layout.sections.main = ['profile'];

const request = {
  publicResume: {
    slug: 'ada1',
    revision: '7',
    lng: 'en',
    downloadEnabled: false,
    document,
  },
  mode: 'continuous' as const,
  canonicalOrigin: 'https://resume.example',
  discoveryEnabled: false,
};

type Runner = typeof runPublicRenderWorker;

const start = async (run: Runner): Promise<string> => {
  const app = createApp();
  app.use(
    '/internal-render/public',
    createPublicRenderHandler({
      run,
      workerUrl: new URL('file:///worker.mjs'),
    }),
  );
  const server = createServer(toNodeListener(app));
  servers.push(server);
  await new Promise<void>((resolveListen) => server.listen(0, resolveListen));
  const address = server.address();
  if (address === null || typeof address === 'string') throw new Error('port');
  return `http://127.0.0.1:${address.port}/internal-render/public`;
};

const ok: Runner = async () => '<!doctype html><title>ok</title>';

describe('internal public render handler', () => {
  it('enforces POST and exact JSON media type before the runner', async () => {
    let calls = 0;
    const url = await start(async (...args) => {
      calls += 1;
      return ok(...args);
    });
    const method = await fetch(url, { method: 'GET' });
    const media = await fetch(url, {
      method: 'POST',
      headers: { 'content-type': 'application/json; charset=latin1' },
    });
    expect(method.status).toBe(405);
    expect(method.headers.get('allow')).toBe('POST');
    expect(media.status).toBe(415);
    expect(calls).toBe(0);
  });

  it('caps streams before decoding and never calls the runner', async () => {
    let calls = 0;
    const url = await start(async (...args) => {
      calls += 1;
      return ok(...args);
    });
    const response = await fetch(url, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: 'x'.repeat(PUBLIC_RENDER_REQUEST_MAX_BYTES + 1),
    });
    expect(response.status).toBe(413);
    expect(calls).toBe(0);
  });

  it('maps invalid UTF-8 and all closed-shape errors to 400', async () => {
    let calls = 0;
    const url = await start(async (...args) => {
      calls += 1;
      return ok(...args);
    });
    const bodies: BodyInit[] = [
      Uint8Array.of(0xc3, 0x28),
      `${JSON.stringify(request).slice(0, -1)},"mode":"continuous"}`,
      `${JSON.stringify(request).slice(0, -1)},"unknown":true}`,
      `${JSON.stringify(request)} trailing`,
      JSON.stringify({
        ...request,
        publicResume: {
          ...request.publicResume,
          document: { ...document, nope: true },
        },
      }),
    ];
    for (const body of bodies) {
      const response = await fetch(url, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body,
      });
      expect(response.status).toBe(400);
    }
    expect(calls).toBe(0);
  });

  it('maps generic worker failure to 500 and deadline to 504', async () => {
    const generic = await start(async () => {
      throw new Error('worker');
    });
    const deadline = await start(async () => {
      throw new PublicRenderDeadlineError();
    });
    for (const [url, status] of [
      [generic, 500],
      [deadline, 504],
    ] as const) {
      const response = await fetch(url, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(request),
      });
      expect(response.status).toBe(status);
    }
  });

  it('keeps the runner signal live after a successful response closes',
    async () => {
      let signal: AbortSignal | undefined;
      const url = await start(async (_request, options) => {
        signal = options.signal;
        return '<!doctype html><title>ok</title>';
      });
      const response = await fetch(url, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(request),
      });
      await response.text();
      expect(signal?.aborted).toBe(false);
    },
  );
});
