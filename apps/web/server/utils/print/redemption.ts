import { request as httpRequest } from 'node:http';
import { Readable } from 'node:stream';

import {
  decodePrintEnvelopeBytes,
  PRINT_ENVELOPE_MAX_BYTES,
  PRINT_FAILURE,
  type PrintEnvelope,
} from './envelope';

export const PRINT_REDEMPTION_DEADLINE_MS = 5_000;

export interface PrintRedemptionRequest {
  resumeId: string;
  jobId: string;
  capability: string;
}

export interface PrintRedemptionClient {
  redeem: (request: PrintRedemptionRequest) => Promise<PrintEnvelope>;
}

export interface PrintRedemptionClientOptions {
  origin: string;
  signal: AbortSignal;
  transport: typeof fetch;
}

const printOrigins = new Set([
  'http://127.0.0.1:20082',
  'http://127.0.0.1:20445',
  'http://127.0.0.1:8081',
  'http://10.91.0.2:8081',
]);

export const directPrintTransport: typeof fetch = (async (input, init) => {
  if (typeof input !== 'string' || init?.method !== 'POST') {
    throw new Error(PRINT_FAILURE);
  }
  const url = new URL(input);
  const headers = new Headers(init.headers);
  const body = typeof init.body === 'string'
    ? Buffer.from(init.body, 'utf8')
    : undefined;
  if (body === undefined) throw new Error(PRINT_FAILURE);
  headers.set('content-length', String(body.byteLength));
  return new Promise<Response>((resolve, reject) => {
    const outgoing = httpRequest(url, {
      method: 'POST',
      headers: Object.fromEntries(headers),
      signal: init.signal ?? undefined,
      agent: false,
    }, (incoming) => {
      const responseHeaders = new Headers();
      for (let index = 0; index < incoming.rawHeaders.length; index += 2) {
        responseHeaders.append(
          incoming.rawHeaders[index] ?? '',
          incoming.rawHeaders[index + 1] ?? '',
        );
      }
      const status = incoming.statusCode ?? 0;
      const bodyless = status === 204 || status === 205 || status === 304;
      resolve(new Response(
        bodyless
          ? null
          : Readable.toWeb(incoming) as unknown as BodyInit,
        { headers: responseHeaders, status },
      ));
    });
    outgoing.once('error', reject);
    outgoing.end(body);
  });
}) as typeof fetch;

const canonicalUUID = (value: string): boolean =>
  value !== '00000000-0000-0000-0000-000000000000'
  && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u
    .test(value);

export const isRenderCapability = (value: string): boolean => {
  if (!/^[A-Za-z0-9_-]{43}$/u.test(value)) return false;
  const decoded = Buffer.from(value, 'base64url');
  return decoded.length === 32 && decoded.toString('base64url') === value;
};

const fail = (): never => {
  throw new Error(PRINT_FAILURE);
};

const rejectResponse = async (response: Response): Promise<never> => {
  await response.body?.cancel().catch(() => undefined);
  return fail();
};

const readEnvelope = async (response: Response): Promise<PrintEnvelope> => {
  if (
    response.status !== 200
    || response.redirected
    || response.headers.get('content-type') !== 'application/json'
    || response.headers.get('cache-control') !== 'no-store'
    || response.headers.has('content-encoding')
    || response.headers.has('set-cookie')
  ) return rejectResponse(response);
  const lengthHeader = response.headers.get('content-length');
  if (lengthHeader === null || !/^(?:0|[1-9]\d*)$/u.test(lengthHeader)) {
    return rejectResponse(response);
  }
  const declaredLength = Number(lengthHeader);
  const body = response.body;
  if (
    !Number.isSafeInteger(declaredLength)
    || declaredLength > PRINT_ENVELOPE_MAX_BYTES
    || body === null
  ) return rejectResponse(response);

  const reader = body.getReader();
  const chunks: Uint8Array[] = [];
  let received = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    received += value.byteLength;
    if (received > PRINT_ENVELOPE_MAX_BYTES) {
      await reader.cancel().catch(() => undefined);
      fail();
    }
    chunks.push(value);
  }
  if (received !== declaredLength) fail();
  const source = new Uint8Array(received);
  let offset = 0;
  for (const chunk of chunks) {
    source.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return decodePrintEnvelopeBytes(source);
};

export function createPrintRedemptionClient(
  options: PrintRedemptionClientOptions,
): PrintRedemptionClient {
  const configured = printOrigins.has(options.origin);
  return {
    async redeem(request) {
      if (
        !configured
        || options.signal.aborted
        || !canonicalUUID(request.resumeId)
        || !canonicalUUID(request.jobId)
        || !isRenderCapability(request.capability)
      ) return fail();
      const body = JSON.stringify({
        resumeId: request.resumeId,
        audience: 'nuxt-print',
      });
      if (Buffer.byteLength(body, 'utf8') > 128) return fail();

      const controller = new AbortController();
      const abort = (): void => controller.abort();
      options.signal.addEventListener('abort', abort, { once: true });
      const timer = setTimeout(abort, PRINT_REDEMPTION_DEADLINE_MS);
      try {
        const response = await options.transport(
          `${options.origin}/internal-render/print/redeem`,
          {
            method: 'POST',
            headers: {
              'authorization': `RenderCapability ${request.capability}`,
              'content-type': 'application/json',
              'x-render-job-id': request.jobId,
            },
            body,
            cache: 'no-store',
            credentials: 'omit',
            redirect: 'error',
            referrerPolicy: 'no-referrer',
            signal: controller.signal,
          },
        );
        if (controller.signal.aborted) return fail();
        return await readEnvelope(response);
      } catch {
        return fail();
      } finally {
        clearTimeout(timer);
        options.signal.removeEventListener('abort', abort);
      }
    },
  };
}
