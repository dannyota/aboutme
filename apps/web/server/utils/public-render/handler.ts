import {
  createError,
  defineEventHandler,
  getHeader,
  setHeader,
  setResponseStatus,
} from 'h3';

import {
  decodePublicRenderEnvelopeBytes,
  PUBLIC_RENDER_FAILURE,
  PUBLIC_RENDER_REQUEST_MAX_BYTES,
} from './envelope';
import type { runPublicRenderWorker } from './runner';
import { PUBLIC_RENDER_DEADLINE_MS, PublicRenderDeadlineError } from './runner';

export interface PublicRenderHandlerDependencies {
  run: typeof runPublicRenderWorker;
  workerUrl: URL | string;
}

export function createPublicRenderHandler(
  dependencies: PublicRenderHandlerDependencies,
) {
  return defineEventHandler(async (event) => {
    if (event.method !== 'POST') {
      setHeader(event, 'allow', 'POST');
      throw createError({
        statusCode: 405,
        statusMessage: PUBLIC_RENDER_FAILURE,
      });
    }
    const contentType = getHeader(event, 'content-type');
    if (
      contentType === undefined
      || !/^application\/json(?:;\s*charset=utf-8)?$/iu.test(contentType)
    ) {
      throw createError({
        statusCode: 415,
        statusMessage: PUBLIC_RENDER_FAILURE,
      });
    }
    const controller = new AbortController();
    let renderFinished = false;
    const abort = () => controller.abort();
    const responseClosed = () => {
      if (!renderFinished && !event.node.res.writableEnded) abort();
    };
    event.node.req.once('aborted', abort);
    event.node.res.once('close', responseClosed);
    try {
      if (
        event.node.req.aborted
        || (event.node.req.destroyed && !event.node.req.complete)
      ) {
        controller.abort();
      }
      let bytes = 0;
      const chunks: Buffer[] = [];
      for await (const chunk of event.node.req) {
        const data = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
        bytes += data.length;
        if (bytes > PUBLIC_RENDER_REQUEST_MAX_BYTES) {
          setResponseStatus(event, 413);
          return '';
        }
        chunks.push(data);
      }
      let request;
      try {
        request = decodePublicRenderEnvelopeBytes(Buffer.concat(chunks));
      } catch {
        setResponseStatus(event, 400);
        return '';
      }
      if (event.node.req.aborted) controller.abort();
      const html = await dependencies.run(request, {
        signal: controller.signal,
        deadlineMs: PUBLIC_RENDER_DEADLINE_MS,
        workerUrl: dependencies.workerUrl,
      });
      renderFinished = true;
      setHeader(event, 'content-type', 'text/html; charset=utf-8');
      return html;
    } catch (error) {
      setResponseStatus(
        event,
        error instanceof PublicRenderDeadlineError ? 504 : 500,
      );
      setHeader(event, 'content-type', 'text/html; charset=utf-8');
      return '<!doctype html><title>Temporarily unavailable</title>\n';
    } finally {
      event.node.req.removeListener('aborted', abort);
      event.node.res.removeListener('close', responseClosed);
    }
  });
}
