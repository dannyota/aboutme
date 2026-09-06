import {
  defineEventHandler,
  setHeader,
  setResponseStatus,
} from 'h3';

import { PRINT_FAILURE, type PrintEnvelope } from './envelope';
import {
  createPrintRedemptionClient,
  isRenderCapability,
} from './redemption';
import {
  PRINT_WORKER_DEADLINE_MS,
  type runPrintWorker,
} from './runner';
import { PRINT_CONTENT_SECURITY_POLICY } from './protocol';

export const PRINT_NOT_FOUND_BODY
  = '{"error":{"code":"not_found","message":"not found"}}\n';

export interface PrintHandlerDependencies {
  origin: string;
  transport: typeof fetch;
  run: typeof runPrintWorker;
  workerUrl: URL | string;
}

const canonicalUUID = (value: string): boolean =>
  value !== '00000000-0000-0000-0000-000000000000'
  && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u
    .test(value);

const headerValues = (
  rawHeaders: readonly string[],
  name: string,
): string[] => {
  const values: string[] = [];
  for (let index = 0; index < rawHeaders.length; index += 2) {
    if (rawHeaders[index]?.toLowerCase() === name) {
      values.push(rawHeaders[index + 1] ?? '');
    }
  }
  return values;
};

export function createPrintHandler(dependencies: PrintHandlerDependencies) {
  return defineEventHandler(async (event) => {
    setHeader(event, 'cache-control', 'no-store');
    setHeader(event, 'content-security-policy', PRINT_CONTENT_SECURITY_POLICY);
    setHeader(event, 'referrer-policy', 'no-referrer');
    setHeader(event, 'x-content-type-options', 'nosniff');
    setHeader(event, 'content-type', 'application/json; charset=utf-8');
    if (event.method !== 'GET') {
      setHeader(event, 'allow', 'GET');
      setResponseStatus(event, 405);
      return PRINT_NOT_FOUND_BODY;
    }

    const rawUrl = event.node.req.url ?? '';
    const match = /^\/print\/([0-9a-f-]{36})$/u.exec(rawUrl);
    const resumeId = match?.[1];
    const rawHeaders = event.node.req.rawHeaders;
    const authorizations = headerValues(rawHeaders, 'authorization');
    const jobIDs = headerValues(rawHeaders, 'x-render-job-id');
    const cookies = headerValues(rawHeaders, 'cookie');
    const contentLengths = headerValues(rawHeaders, 'content-length');
    const transferEncodings = headerValues(rawHeaders, 'transfer-encoding');
    const authorization = authorizations[0] ?? '';
    const jobId = jobIDs[0] ?? '';
    const capability = /^RenderCapability ([A-Za-z0-9_-]{43})$/u
      .exec(authorization)?.[1];
    if (
      resumeId === undefined
      || !canonicalUUID(resumeId)
      || authorizations.length !== 1
      || jobIDs.length !== 1
      || cookies.length !== 0
      || contentLengths.length !== 0
      || transferEncodings.length !== 0
      || capability === undefined
      || !isRenderCapability(capability)
      || !canonicalUUID(jobId)
    ) {
      setResponseStatus(event, 404);
      return PRINT_NOT_FOUND_BODY;
    }

    for await (const chunk of event.node.req) {
      if (Buffer.byteLength(chunk) > 0) {
        setResponseStatus(event, 404);
        return PRINT_NOT_FOUND_BODY;
      }
    }

    const controller = new AbortController();
    let finished = false;
    const abort = (): void => controller.abort();
    const responseClosed = (): void => {
      if (!finished && !event.node.res.writableEnded) abort();
    };
    event.node.req.once('aborted', abort);
    event.node.res.once('close', responseClosed);
    try {
      if (event.node.req.aborted) return PRINT_NOT_FOUND_BODY;
      const envelope: PrintEnvelope = await createPrintRedemptionClient({
        origin: dependencies.origin,
        signal: controller.signal,
        transport: dependencies.transport,
      }).redeem({ capability, jobId, resumeId });
      if (envelope.resumeId !== resumeId) throw new Error(PRINT_FAILURE);
      const html = await dependencies.run(envelope, {
        signal: controller.signal,
        deadlineMs: PRINT_WORKER_DEADLINE_MS,
        workerUrl: dependencies.workerUrl,
      });
      finished = true;
      setHeader(event, 'content-type', 'text/html; charset=utf-8');
      return html;
    } catch {
      setResponseStatus(event, 404);
      return PRINT_NOT_FOUND_BODY;
    } finally {
      event.node.req.removeListener('aborted', abort);
      event.node.res.removeListener('close', responseClosed);
    }
  });
}
