import { computed, nextTick, ref } from 'vue';
import { describe, expect, it, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { mockNuxtImport } from '@nuxt/test-utils/runtime';

import PDFDownloadButton from
  '../../app/components/editor/PDFDownloadButton.vue';
import PDFOpenButton from '../../app/components/editor/PDFOpenButton.vue';
import {
  createPdfDownloadController,
  MAX_PDF_DOWNLOAD_BYTES,
} from '../../app/editor/pdfDownload';
import { acceptedFixture } from './fixture';

const resumeId = '00000000-0000-4000-8000-000000000001';
const locale = ref<'vi' | 'en'>('en');
const vietnameseContentDisposition = [
  'attachment; filename="Nguyen-Van-Duc-Resume.pdf"; ',
  'filename*=UTF-8\'\'Nguy%E1%BB%85n-V%C4%83n-%C4%90%E1%BB%A9c-Resume.pdf',
].join('');
mockNuxtImport('useLocale', () => () => ({ locale }));

describe('PDF download', () => {
  it.each([
    ['en', 'save-required', 'Save changes before downloading PDF.'],
    ['vi', 'save-required', 'Lưu thay đổi trước khi tải PDF.'],
    ['en', 'session-lost', 'Your session ended. Sign in again.'],
    ['vi', 'session-lost', 'Phiên của bạn đã kết thúc. Đăng nhập lại.'],
    ['en', 'download-failed', 'PDF download failed. Try again.'],
    ['vi', 'download-failed', 'Không thể tải PDF. Hãy thử lại.'],
    ['en', 'temporarily-unavailable',
      'PDF is temporarily unavailable. Try again.'],
    ['vi', 'temporarily-unavailable',
      'PDF tạm thời không khả dụng. Hãy thử lại.'],
  ] as const)('renders %s copy for %s', async (nextLocale, code, text) => {
    locale.value = nextLocale;
    const state = ref({ kind: 'error', code } as const);
    const download = vi.fn();
    const wrapper = mount(PDFDownloadButton, {
      props: { controller: { state, download, dispose: vi.fn() } },
    });

    expect(wrapper.get('[data-download-pdf-status]').text()).toBe(text);
    expect(download).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it('changes shown copy without repeating a PDF request or changing bytes',
    async () => {
      locale.value = 'en';
      const bytes = new Uint8Array([3, 1, 4]);
      let downloaded: Blob | undefined;
      const context = setup({
        fetcher: vi.fn().mockResolvedValue(pdfResponse([bytes])),
      });
      context.createObjectURL.mockImplementation((blob: Blob) => {
        downloaded = blob;
        return 'blob:pdf';
      });
      const wrapper = mount(PDFDownloadButton, {
        props: { controller: context.controller },
      });

      expect(wrapper.text()).toContain('Download PDF');
      await wrapper.get('[data-action="download-pdf"]').trigger('click');
      await vi.waitFor(() => expect(context.download).toHaveBeenCalledOnce());
      expect([...new Uint8Array(await downloaded!.arrayBuffer())])
        .toEqual([...bytes]);
      expect(context.fetcher).toHaveBeenCalledOnce();

      locale.value = 'vi';
      await nextTick();

      expect(wrapper.text()).toContain('Tải PDF');
      expect(context.fetcher).toHaveBeenCalledOnce();
      expect(context.download).toHaveBeenCalledOnce();
      expect([...new Uint8Array(await downloaded!.arrayBuffer())])
        .toEqual([...bytes]);
      wrapper.unmount();
    });
  it('flushes before fetching the accepted owner PDF with its UTF-8 filename',
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
          return pdfResponse([new Uint8Array([1, 2, 3])], {
            'Content-Disposition': vietnameseContentDisposition,
          });
        },
      });

      await context.controller.download();

      expect(order).toEqual(['flush', 'fetch']);
      expect(context.download).toHaveBeenCalledWith(
        'blob:pdf',
        'Nguyễn-Văn-Đức-Resume.pdf',
      );
      expect(context.revokeObjectURL).toHaveBeenCalledWith('blob:pdf');
    });

  it('uses the ASCII filename when the UTF-8 filename is absent', async () => {
    const context = setup({
      fetcher: async () => pdfResponse([new Uint8Array([1])], {
        'Content-Disposition': 'attachment; filename="Ada-Lovelace-Resume.pdf"',
      }),
    });

    await context.controller.download();

    expect(context.download).toHaveBeenCalledWith(
      'blob:pdf',
      'Ada-Lovelace-Resume.pdf',
    );
  });

  it('accepts the longest name the server emits', async () => {
    const filename = `${'A'.repeat(64)}-Resume.pdf`;
    const context = setup({
      fetcher: async () => pdfResponse([new Uint8Array([1])], {
        'Content-Disposition': `attachment; filename="${filename}"`,
      }),
    });

    await context.controller.download();

    expect(context.download).toHaveBeenCalledWith('blob:pdf', filename);
  });

  it.each([
    [
      'a malformed UTF-8 filename with an ASCII fallback',
      [
        'attachment; filename="Ada-Lovelace-Resume.pdf"; ',
        'filename*=UTF-8\'\'Ada%ZZ.pdf',
      ].join(''),
      'Ada-Lovelace-Resume.pdf',
    ],
    [
      'a path in the filename',
      'attachment; filename="../../Resume.pdf"',
      'Resume.pdf',
    ],
    [
      'a control character in the filename',
      'attachment; filename="Ada\u0000Resume.pdf"',
      'Resume.pdf',
    ],
  ])('uses a safe filename for %s',
    async (_name, contentDisposition, filename) => {
      const context = setup({
        fetcher: async () =>
          pdfResponseWithContentDisposition(contentDisposition),
      });

      await context.controller.download();

      expect(context.download).toHaveBeenCalledWith('blob:pdf', filename);
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
        code: 'save-required',
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
      code: 'download-failed',
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
      code: 'temporarily-unavailable',
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

describe('PDF in a new tab', () => {
  it('opens the tab inside the click, then shows the fetched bytes in it',
    async () => {
      const order: string[] = [];
      const context = setup({
        flush: async () => {
          order.push('flush');
        },
        fetcher: async () => {
          order.push('fetch');
          return pdfResponse([new Uint8Array([1, 2, 3])]);
        },
      });
      context.openTab.mockImplementation(() => {
        order.push('open');
        return context.tab;
      });

      const pending = context.controller.openInTab();
      expect(order).toEqual(['open']);
      await pending;

      expect(order).toEqual(['open', 'flush', 'fetch']);
      expect(context.tab.navigate).toHaveBeenCalledWith('blob:pdf');
      expect(context.tab.close).not.toHaveBeenCalled();
      expect(context.download).not.toHaveBeenCalled();
      expect(context.controller.state.value).toEqual({ kind: 'idle' });
    });

  it('keeps the object URL until the tab has loaded it', async () => {
    vi.useFakeTimers();
    try {
      const context = setup();
      await context.controller.openInTab();
      expect(context.revokeObjectURL).not.toHaveBeenCalled();
      vi.advanceTimersByTime(60_000);
      expect(context.revokeObjectURL).toHaveBeenCalledWith('blob:pdf');
    } finally {
      vi.useRealTimers();
    }
  });

  it('reports a blocked tab and sends no request', async () => {
    const context = setup();
    context.openTab.mockReturnValue(null as never);

    const state = await context.controller.openInTab();

    expect(state).toEqual({ kind: 'error', code: 'popup-blocked' });
    expect(context.fetcher).not.toHaveBeenCalled();
  });

  it('opens no tab when the session is gone', async () => {
    const context = setup();
    context.user.value = null;

    const state = await context.controller.openInTab();

    expect(state).toEqual({ kind: 'error', code: 'session-lost' });
    expect(context.openTab).not.toHaveBeenCalled();
  });

  it.each([429, 503])('closes the tab and reports a %i', async (status) => {
    const context = setup({
      fetcher: async () => new Response('{}', { status }),
    });

    const state = await context.controller.openInTab();

    expect(state).toEqual({ kind: 'error', code: 'temporarily-unavailable' });
    expect(context.tab.close).toHaveBeenCalledOnce();
    expect(context.tab.navigate).not.toHaveBeenCalled();
  });

  it('closes the tab when changes could not be saved', async () => {
    const record = acceptedRecord();
    const context = setup({
      record,
      flush: async () => {
        record.pending = [{}] as never;
      },
    });

    const state = await context.controller.openInTab();

    expect(state).toEqual({ kind: 'error', code: 'save-required' });
    expect(context.tab.close).toHaveBeenCalledOnce();
    expect(context.fetcher).not.toHaveBeenCalled();
  });

  it.each([
    ['en', 'Open PDF in new tab', 'Opening PDF…'],
    ['vi', 'Mở PDF trong tab mới', 'Đang mở PDF…'],
  ] as const)('labels the %s button for idle and pending', async (
    nextLocale,
    idle,
    pendingLabel,
  ) => {
    locale.value = nextLocale;
    const state = ref<{ kind: string }>({ kind: 'idle' });
    const wrapper = mount(PDFOpenButton, {
      props: {
        controller: {
          state,
          download: vi.fn(),
          openInTab: vi.fn(),
          dispose: vi.fn(),
        },
      } as never,
    });
    const button = wrapper.get('[data-action="open-pdf"]');

    expect(button.text()).toBe(idle);
    expect(button.attributes('disabled')).toBeUndefined();
    state.value = { kind: 'pending' };
    await nextTick();
    expect(button.text()).toBe(pendingLabel);
    expect(button.attributes('disabled')).toBeDefined();
    wrapper.unmount();
  });

  it('opens on click', async () => {
    locale.value = 'en';
    const openInTab = vi.fn();
    const wrapper = mount(PDFOpenButton, {
      props: {
        controller: {
          state: ref({ kind: 'idle' }),
          download: vi.fn(),
          openInTab,
          dispose: vi.fn(),
        },
      } as never,
    });

    await wrapper.get('[data-action="open-pdf"]').trigger('click');

    expect(openInTab).toHaveBeenCalledOnce();
    wrapper.unmount();
  });

  it.each([
    ['en', 'save-required', 'Save changes before opening PDF.'],
    ['vi', 'save-required', 'Lưu thay đổi trước khi mở PDF.'],
    ['en', 'temporarily-unavailable',
      'PDF is temporarily unavailable. Try again.'],
    ['vi', 'temporarily-unavailable',
      'PDF tạm thời không khả dụng. Hãy thử lại.'],
    ['en', 'popup-blocked',
      'The browser blocked the tab. Allow pop-ups.'],
    ['vi', 'popup-blocked',
      'Trình duyệt chặn tab mới. Cho phép cửa sổ bật lên.'],
  ] as const)('renders %s copy for %s', (nextLocale, code, text) => {
    locale.value = nextLocale;
    const wrapper = mount(PDFOpenButton, {
      props: {
        controller: {
          state: ref({ kind: 'error', code }),
          download: vi.fn(),
          openInTab: vi.fn(),
          dispose: vi.fn(),
        },
      } as never,
    });

    expect(wrapper.get('[data-open-pdf-status]').text()).toBe(text);
    wrapper.unmount();
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
  const tab = { navigate: vi.fn(), close: vi.fn() };
  const openTab = vi.fn(() => tab);
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
    openTab,
  });
  return {
    controller,
    createObjectURL,
    download,
    fetcher,
    openTab,
    record,
    revokeObjectURL,
    tab,
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

function pdfResponse(
  chunks: readonly Uint8Array[],
  headers: HeadersInit = {},
): Response {
  return new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks) controller.enqueue(chunk);
        controller.close();
      },
    }),
    {
      status: 200,
      headers: noStoreHeaders(headers),
    },
  );
}

function pdfResponseWithContentDisposition(
  contentDisposition: string,
): Response {
  const response = pdfResponse([new Uint8Array([1])]);
  const headers = response.headers;
  Object.defineProperty(response, 'headers', {
    value: {
      get(name: string) {
        if (name === 'Content-Disposition') return contentDisposition;
        return headers.get(name);
      },
    },
  });
  return response;
}

function noStoreHeaders(extra: HeadersInit = {}): Headers {
  return new Headers({
    'Cache-Control': 'no-store, no-transform',
    'Content-Type': 'application/pdf',
    ...extra,
  });
}
