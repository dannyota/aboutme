import { computed, nextTick, ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import {
  createPdfDownloadController,
  MAX_PDF_DOWNLOAD_BYTES,
} from '../../app/editor/pdfDownload';
import { acceptedFixture } from './fixture';

const resumeId = '00000000-0000-4000-8000-000000000001';

describe('PDF download', () => {
  it('flushes before fetching the accepted owner PDF as resume.pdf',
    async () => {
      const order: string[] = [];
      const context = setup({
        flush: async () => {
          order.push('flush');
        },
        fetcher: async (input, init) => {
          order.push('fetch');
          expect(input).toBe(`/api/v1/resumes/${resumeId}/pdf`);
          expect(init).toMatchObject({
            method: 'GET',
            credentials: 'same-origin',
          });
          expect(init?.body).toBeUndefined();
          expect(init?.headers).toBeUndefined();
          return pdfResponse([new Uint8Array([1, 2, 3])]);
        },
      });

      await context.controller.download();

      expect(order).toEqual(['flush', 'fetch']);
      expect(context.download).toHaveBeenCalledWith('blob:pdf', 'resume.pdf');
      expect(context.revokeObjectURL).toHaveBeenCalledWith('blob:pdf');
    });

  it('does not fetch when a save remains unresolved after flushing',
    async () => {
      const record = acceptedRecord();
      const context = setup({
        record,
        flush: async () => {
          record.pending = [{}] as never;
        },
      });

      await context.controller.download();

      expect(context.fetcher).not.toHaveBeenCalled();
      expect(context.controller.state.value).toEqual({
        kind: 'error',
        message: 'Save changes before downloading PDF.',
      });
    });

  it('flushes ordinary pending saves before requesting the PDF', async () => {
    const record = acceptedRecord();
    record.pending = [{}] as never;
    const context = setup({
      record,
      flush: async () => {
        record.pending = [];
      },
    });

    await context.controller.download();

    expect(context.fetcher).toHaveBeenCalledOnce();
  });

  it.each([
    [
      'conflict',
      (record: ReturnType<typeof acceptedRecord>) => {
        record.conflicts = [{}] as never;
      },
    ],
    [
      'failed save',
      (record: ReturnType<typeof acceptedRecord>) => {
        record.attempt = { kind: 'failed' } as never;
      },
    ],
    [
      'uncertain save',
      (record: ReturnType<typeof acceptedRecord>) => {
        record.attempt = { kind: 'unknown' } as never;
      },
    ],
    [
      'partial template',
      (record: ReturnType<typeof acceptedRecord>) => {
        record.templateState = { kind: 'partial' } as never;
      },
    ],
    [
      'opaque photo',
      (record: ReturnType<typeof acceptedRecord>) => {
        record.opaquePhotoOutcome = {} as never;
      },
    ],
    [
      'complete read',
      (record: ReturnType<typeof acceptedRecord>) => {
        record.completeReadRequired = true;
      },
    ],
  ])('blocks an unresolved %s state', async (_name, arrange) => {
    const record = acceptedRecord();
    arrange(record);
    const context = setup({ record });

    await context.controller.download();

    expect(context.fetcher).not.toHaveBeenCalled();
  });

  it('allows incomplete drafts without invoking publish validation',
    async () => {
      const record = acceptedRecord();
      record.current.document.personalDetails.fullName = '';
      const context = setup({ record });

      await context.controller.download();

      expect(context.fetcher).toHaveBeenCalledOnce();
    });

  it('blocks known save issues without running draft completeness validation',
    async () => {
      const record = acceptedRecord();
      record.issues = { save: [{ path: 'title' }] } as never;
      const context = setup({ record });

      await context.controller.download();

      expect(context.fetcher).not.toHaveBeenCalled();
    });

  it('prevents a duplicate click while a download is pending', async () => {
    let resolveFetch: ((response: Response) => void) | undefined;
    const context = setup({
      fetcher: () =>
        new Promise<Response>((resolve) => {
          resolveFetch = resolve;
        }),
    });

    const first = context.controller.download();
    const second = context.controller.download();
    await Promise.resolve();
    expect(context.fetcher).toHaveBeenCalledOnce();
    expect(context.controller.state.value).toEqual({ kind: 'pending' });
    resolveFetch?.(pdfResponse([new Uint8Array([1])]));
    await Promise.all([first, second]);
  });

  it.each([
    ['non-success status', () => new Response(null, { status: 500 })],
    [
      'wrong media type',
      () =>
        new Response('x', {
          status: 200,
          headers: noStoreHeaders({ 'Content-Type': 'text/plain' }),
        }),
    ],
    [
      'missing no-store policy',
      () =>
        new Response('x', {
          status: 200,
          headers: { 'Content-Type': 'application/pdf' },
        }),
    ],
    ['empty body', () => pdfResponse([])],
    [
      'oversized content length',
      () =>
        new Response('x', {
          status: 200,
          headers: noStoreHeaders({
            'Content-Length': String(MAX_PDF_DOWNLOAD_BYTES + 1),
          }),
        }),
    ],
    [
      'oversized streaming body',
      () =>
        pdfResponse([
          new Uint8Array(MAX_PDF_DOWNLOAD_BYTES),
          new Uint8Array([0]),
        ]),
    ],
  ])('rejects a %s response before downloading', async (_name, response) => {
    const context = setup({ fetcher: async () => response() });

    await context.controller.download();

    expect(context.download).not.toHaveBeenCalled();
    expect(context.createObjectURL).not.toHaveBeenCalled();
    expect(context.controller.state.value).toEqual({
      kind: 'error',
      message: 'PDF download failed. Try again.',
    });
  });

  it('cancels an invalid response stream and aborts its request', async () => {
    let canceled = false;
    let signal: AbortSignal | undefined;
    const response = new Response(new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new Uint8Array([1]));
      },
      cancel() {
        canceled = true;
      },
    }), {
      status: 200,
      headers: noStoreHeaders({ 'Content-Type': 'text/plain' }),
    });
    const context = setup({
      fetcher: (_input, init) => {
        signal = init?.signal;
        return Promise.resolve(response);
      },
    });

    await context.controller.download();

    expect(canceled).toBe(true);
    expect(signal?.aborted).toBe(true);
  });

  it('retries a rate-limited download only after a later click', async () => {
    const context = setup({
      fetcher: vi
        .fn()
        .mockResolvedValueOnce(new Response(null, { status: 429 }))
        .mockResolvedValueOnce(pdfResponse([new Uint8Array([1])])),
    });

    await context.controller.download();
    expect(context.controller.state.value).toEqual({
      kind: 'error',
      message: 'PDF is temporarily unavailable. Try again.',
    });
    await context.controller.download();

    expect(context.fetcher).toHaveBeenCalledTimes(2);
    expect(context.download).toHaveBeenCalledOnce();
  });

  it('aborts and discards a response when the owner session changes',
    async () => {
      let resolveFetch: ((response: Response) => void) | undefined;
      let signal: AbortSignal | undefined;
      const context = setup({
        fetcher: (_input, init) =>
          new Promise<Response>((resolve) => {
            signal = init?.signal;
            resolveFetch = resolve;
          }),
      });

      const pending = context.controller.download();
      await Promise.resolve();
      context.user.value = { id: 'different-owner' };
      await nextTick();
      expect(signal?.aborted).toBe(true);
      resolveFetch?.(pdfResponse([new Uint8Array([1])]));
      await pending;

      expect(context.download).not.toHaveBeenCalled();
      expect(context.createObjectURL).not.toHaveBeenCalled();
    });

  it('aborts on disposal and permits a later deliberate retry after failure',
    async () => {
      let signal: AbortSignal | undefined;
      const context = setup({
        fetcher: (_input, init) => {
          signal = init?.signal;
          return Promise.reject(new DOMException('Aborted', 'AbortError'));
        },
      });

      const pending = context.controller.download();
      await Promise.resolve();
      context.controller.dispose();
      await pending;
      expect(signal?.aborted).toBe(true);

      context.fetcher.mockResolvedValueOnce(pdfResponse([new Uint8Array([1])]));
      await context.controller.download();
      expect(context.fetcher).toHaveBeenCalledTimes(2);
    });
});

function setup(
  overrides: {
    record?: ReturnType<typeof acceptedRecord>;
    flush?: () => Promise<void>;
    fetcher?: typeof fetch;
  } = {},
) {
  const record = ref(overrides.record ?? acceptedRecord());
  const user = ref<{ id: string } | null>({ id: 'owner-1' });
  const fetcher = vi.fn(
    overrides.fetcher ?? (async () => pdfResponse([new Uint8Array([1])])),
  );
  const createObjectURL = vi.fn(() => 'blob:pdf');
  const revokeObjectURL = vi.fn();
  const download = vi.fn();
  const controller = createPdfDownloadController({
    resumeId,
    record: computed(() => record.value),
    flush: overrides.flush ?? (async () => undefined),
    auth: {
      authState: ref('authenticated'),
      user,
    },
    fetcher,
    createObjectURL,
    revokeObjectURL,
    download,
  });
  return {
    controller,
    createObjectURL,
    download,
    fetcher,
    record,
    revokeObjectURL,
    user,
  };
}

function acceptedRecord() {
  const accepted = acceptedFixture({
    metadata: {
      ...acceptedFixture().metadata,
      id: resumeId,
    },
  });
  return {
    accepted,
    current: {
      document: structuredClone(accepted.document),
      metadata: structuredClone(accepted.metadata),
    },
    pending: [],
    attempt: null,
    conflicts: [],
    issues: {},
    templateState: null,
    photoRead: { kind: 'none' } as const,
    completeReadRequired: false,
    sessionLost: false,
    opaquePhotoOutcome: null,
  };
}

function pdfResponse(chunks: readonly Uint8Array[]): Response {
  return new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks) controller.enqueue(chunk);
        controller.close();
      },
    }),
    {
      status: 200,
      headers: noStoreHeaders(),
    },
  );
}

function noStoreHeaders(extra: HeadersInit = {}): Headers {
  return new Headers({
    'Cache-Control': 'no-store, no-transform',
    'Content-Type': 'application/pdf',
    ...extra,
  });
}
