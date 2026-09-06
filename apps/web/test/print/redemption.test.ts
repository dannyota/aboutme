// @vitest-environment node

import { createServer } from 'node:http';

import { describe, expect, it, vi } from 'vitest';

import { PRINT_ENVELOPE_MAX_BYTES } from '../../server/utils/print/envelope';
import {
  createPrintRedemptionClient,
  directPrintTransport,
  PRINT_REDEMPTION_DEADLINE_MS,
} from '../../server/utils/print/redemption';
import {
  CAPABILITY,
  JOB_ID,
  printEnvelope,
  RESUME_ID,
} from './fixture';

const response = (body: BodyInit, headers: Record<string, string> = {}) => {
  const bytes = typeof body === 'string'
    ? Buffer.byteLength(body)
    : (body as Uint8Array).byteLength;
  return new Response(body, {
    status: 200,
    headers: {
      'cache-control': 'no-store',
      'content-length': String(bytes),
      'content-type': 'application/json',
      ...headers,
    },
  });
};

const redeem = (
  transport: typeof fetch,
  signal = new AbortController().signal,
  origin = 'http://127.0.0.1:20082',
) => createPrintRedemptionClient({ origin, signal, transport }).redeem({
  capability: CAPABILITY,
  jobId: JOB_ID,
  resumeId: RESUME_ID,
});

describe('direct print redemption client', () => {
  it(
    'uses a direct transport without fetch-added request headers',
    async () => {
      let rawHeaderNames: string[] = [];
      const server = createServer((request, outgoing) => {
        rawHeaderNames = request.rawHeaders
          .filter((_value, index) => index % 2 === 0)
          .map((name) => name.toLowerCase())
          .sort();
        const chunks: Buffer[] = [];
        request.on('data', (chunk) => chunks.push(Buffer.from(chunk)));
        request.on('end', () => {
          const body = JSON.stringify(printEnvelope());
          outgoing.writeHead(200, {
            'cache-control': 'no-store',
            'content-length': String(Buffer.byteLength(body)),
            'content-type': 'application/json',
          });
          outgoing.end(body);
        });
      });
      await new Promise<void>((resolve) => server.listen(0, resolve));
      try {
        const address = server.address();
        if (address === null || typeof address === 'string') {
          throw new Error('port');
        }
        const body = JSON.stringify({
          resumeId: RESUME_ID,
          audience: 'nuxt-print',
        });
        const value = await directPrintTransport(
          `http://127.0.0.1:${address.port}/internal-render/print/redeem`,
          {
            method: 'POST',
            headers: {
              'authorization': `RenderCapability ${CAPABILITY}`,
              'content-type': 'application/json',
              'x-render-job-id': JOB_ID,
            },
            body,
            signal: new AbortController().signal,
          },
        );
        await value.arrayBuffer();
        expect(rawHeaderNames).toEqual([
          'authorization',
          'connection',
          'content-length',
          'content-type',
          'host',
          'x-render-job-id',
        ]);
      } finally {
        await new Promise<void>((resolve) => server.close(() => resolve()));
      }
    },
  );

  it(
    'posts the exact bounded request without cookies or redirects',
    async () => {
      const body = JSON.stringify(printEnvelope());
      const transport = vi.fn<typeof fetch>(async (input, init) => {
        expect(input).toBe(
          'http://127.0.0.1:20082/internal-render/print/redeem',
        );
        expect(init).toMatchObject({
          method: 'POST',
          body: JSON.stringify({ resumeId: RESUME_ID, audience: 'nuxt-print' }),
          cache: 'no-store',
          credentials: 'omit',
          redirect: 'error',
          referrerPolicy: 'no-referrer',
        });
        expect(init?.headers).toEqual({
          'authorization': `RenderCapability ${CAPABILITY}`,
          'content-type': 'application/json',
          'x-render-job-id': JOB_ID,
        });
        expect(init?.signal).toBeInstanceOf(AbortSignal);
        return response(body);
      });

      await expect(redeem(transport)).resolves.toEqual(printEnvelope());
      expect(transport).toHaveBeenCalledTimes(1);
    },
  );

  it('allows only the four configured direct origins', async () => {
    const valid = [
      'http://127.0.0.1:20082',
      'http://127.0.0.1:20445',
      'http://127.0.0.1:8081',
      'http://10.91.0.2:8081',
    ];
    for (const origin of valid) {
      const transport = vi.fn<typeof fetch>(async () =>
        response(JSON.stringify(printEnvelope())));
      await expect(redeem(transport, undefined, origin)).resolves.toBeTruthy();
    }
    for (const origin of [
      'http://localhost:20082',
      'https://127.0.0.1:20082',
      'http://127.0.0.1:20082/',
      'http://user@127.0.0.1:20082',
      'http://10.91.0.3:8081',
    ]) {
      const transport = vi.fn<typeof fetch>();
      await expect(redeem(transport, undefined, origin))
        .rejects.toThrow('print failed');
      expect(transport).not.toHaveBeenCalled();
    }
  });

  it('rejects invalid authority before transport', async () => {
    const invalid = [
      { capability: 'short', jobId: JOB_ID, resumeId: RESUME_ID },
      { capability: `${'A'.repeat(42)}=`, jobId: JOB_ID, resumeId: RESUME_ID },
      { capability: `${'A'.repeat(42)}B`, jobId: JOB_ID, resumeId: RESUME_ID },
      { capability: CAPABILITY, jobId: 'not-a-uuid', resumeId: RESUME_ID },
      { capability: CAPABILITY, jobId: JOB_ID, resumeId: 'not-a-uuid' },
    ];
    for (const request of invalid) {
      const transport = vi.fn<typeof fetch>();
      const client = createPrintRedemptionClient({
        origin: 'http://127.0.0.1:20082',
        signal: new AbortController().signal,
        transport,
      });
      await expect(client.redeem(request)).rejects.toThrow('print failed');
      expect(transport).not.toHaveBeenCalled();
    }
  });

  it('requires exact successful response headers and status', async () => {
    const body = JSON.stringify(printEnvelope());
    const cases = [
      new Response(body, { status: 404 }),
      response(body, { 'cache-control': 'private' }),
      response(body, { 'content-type': 'application/json; charset=utf-8' }),
      response(body, { 'content-length': String(body.length + 1) }),
      response(body, { 'content-encoding': 'gzip' }),
      response(body, { 'set-cookie': 'authority=leaked' }),
    ];
    for (const value of cases) {
      await expect(redeem(vi.fn<typeof fetch>(async () => value)))
        .rejects.toThrow('print failed');
    }
  });

  it('caps the streamed payload and rejects malformed bytes', async () => {
    const over = ' '.repeat(PRINT_ENVELOPE_MAX_BYTES + 1);
    const malformed = Uint8Array.of(0xc3, 0x28);
    for (const value of [response(over), response(malformed)]) {
      await expect(redeem(vi.fn<typeof fetch>(async () => value)))
        .rejects.toThrow('print failed');
    }
  });

  it('cancels a rejected response stream before returning', async () => {
    let canceled = false;
    const body = new ReadableStream({
      cancel() {
        canceled = true;
      },
    });
    const rejected = new Response(body, {
      status: 200,
      headers: {
        'cache-control': 'no-store',
        'content-length': String(PRINT_ENVELOPE_MAX_BYTES + 1),
        'content-type': 'application/json',
      },
    });
    await expect(redeem(vi.fn<typeof fetch>(async () => rejected)))
      .rejects.toThrow('print failed');
    expect(canceled).toBe(true);
  });

  it('honors job cancellation and the exact five-second deadline', async () => {
    vi.useFakeTimers();
    try {
      const transport = vi.fn<typeof fetch>(async (_input, init) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => reject(
            new Error('raw transport abort'),
          ));
        }));
      const result = redeem(transport);
      const deadlineRejection = expect(result).rejects.toThrow('print failed');
      await vi.advanceTimersByTimeAsync(PRINT_REDEMPTION_DEADLINE_MS - 1);
      expect(transport.mock.calls[0]?.[1]?.signal?.aborted).toBe(false);
      await vi.advanceTimersByTimeAsync(1);
      await deadlineRejection;
      expect(transport.mock.calls[0]?.[1]?.signal?.aborted).toBe(true);

      const controller = new AbortController();
      const canceled = redeem(transport, controller.signal);
      controller.abort();
      await expect(canceled).rejects.toThrow('print failed');
    } finally {
      vi.useRealTimers();
    }
  });
});
