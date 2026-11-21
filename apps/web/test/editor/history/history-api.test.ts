import { CURRENT_VERSION } from '@aboutme/schema/released';
import { describe, expect, it, vi } from 'vitest';

import {
  createHistoryApi,
  freezeHistoryWrite,
} from '../../../app/editor/historyApi';
import { parseRevision } from '../../../app/editor/revision';
import { acceptedFixture } from '../fixture';

const runtime = { uuid: () => 'key-1' };

function json(
  status: number,
  body: unknown,
  headers: Record<string, string> = {},
): Response {
  return new Response(body === null ? null : JSON.stringify(body), {
    status,
    headers: {
      'Cache-Control': 'no-store, no-transform',
      'Content-Type': 'application/json',
      ...headers,
    },
  });
}

function summary(over: Record<string, unknown> = {}) {
  return {
    id: 'cp-1',
    startedAt: '2026-11-21T07:05:00Z',
    cause: 'edit',
    actor: { kind: 'owner' },
    title: 'CV',
    byteSize: 100,
    ...over,
  };
}

function restoreWrite(revision = '3') {
  return freezeHistoryWrite(
    {
      operation: 'restore',
      resumeId: 'resume-1',
      checkpointId: 'cp-1',
      revision: parseRevision(revision),
      ownerId: 'owner-1',
    },
    runtime,
  );
}

function request(fetcher: ReturnType<typeof vi.fn>, call = 0): Request {
  return fetcher.mock.calls[call]![0] as Request;
}

describe('history API reads', () => {
  it('lists with the session cookie only', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      json(200, {
        data: [
          summary(),
          summary({
            id: 'cp-2',
            cause: 'assistant',
            actor: { kind: 'assistant', name: '' },
          }),
        ],
      }),
    );
    const result = await createHistoryApi(fetcher).list('resume-1');
    expect(result.kind).toBe('ok');
    if (result.kind === 'ok') {
      expect(result.value.map((item) => item.actor)).toEqual([
        { kind: 'owner' },
        { kind: 'assistant', name: '' },
      ]);
    }
    const sent = request(fetcher);
    expect(sent.method).toBe('GET');
    expect(new URL(sent.url, 'http://x').pathname).toBe(
      '/api/v1/resumes/resume-1/checkpoints',
    );
    expect(sent.credentials).toBe('include');
    expect(sent.headers.get('X-CSRF-Token')).toBeNull();
    expect(sent.headers.get('If-Match')).toBeNull();
    expect(sent.headers.get('Idempotency-Key')).toBeNull();
  });

  it('rejects a malformed list item', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      json(200, { data: [summary({ cause: 'other' })] }),
    );
    expect(await createHistoryApi(fetcher).list('resume-1')).toEqual({
      kind: 'failed',
    });
  });

  it('reads one version and validates its document', async () => {
    const document = acceptedFixture().document;
    const { photo: _photo, ...personalDetails } = document.personalDetails;
    const fetcher = vi.fn().mockResolvedValue(
      json(200, {
        data: {
          ...summary(),
          lng: 'en',
          schemaVersion: CURRENT_VERSION,
          document: {
            personalDetails,
            content: document.content,
            customization: document.customization,
          },
        },
      }),
    );
    const result = await createHistoryApi(fetcher).read('resume-1', 'cp-1');
    expect(result.kind).toBe('ok');
    expect(new URL(request(fetcher).url, 'http://x').pathname).toBe(
      '/api/v1/resumes/resume-1/checkpoints/cp-1',
    );
  });

  it.each([
    [401, 'session-lost'],
    [404, 'not-found'],
    [429, 'failed'],
    [500, 'failed'],
  ])('maps read status %i to %s', async (status, kind) => {
    const fetcher = vi.fn().mockResolvedValue(
      json(status, { error: { code: 'x', message: 'x' } }),
    );
    expect((await createHistoryApi(fetcher).read('r', 'c')).kind).toBe(kind);
  });

  it('reports a transport failure as failed', async () => {
    const fetcher = vi.fn().mockRejectedValue(new TypeError('offline'));
    expect((await createHistoryApi(fetcher).list('r')).kind).toBe('failed');
  });
});

describe('history API writes', () => {
  it('sends If-Match, key and CSRF on restore only', async () => {
    const accepted = acceptedFixture();
    const fetcher = vi.fn().mockResolvedValue(
      json(
        200,
        {
          data: {
            ...accepted.metadata,
            revision: '4',
            document: accepted.document,
          },
        },
        {
          'ETag': '"r4"',
          'X-Resume-Schema-Version': String(CURRENT_VERSION),
        },
      ),
    );
    const result = await createHistoryApi(fetcher).write(
      restoreWrite(),
      'csrf-1',
    );
    expect(result.kind).toBe('accepted');
    const sent = request(fetcher);
    expect(sent.method).toBe('POST');
    expect(new URL(sent.url, 'http://x').pathname).toBe(
      '/api/v1/resumes/resume-1/checkpoints/cp-1/restore',
    );
    expect(sent.headers.get('If-Match')).toBe('"r3"');
    expect(sent.headers.get('Idempotency-Key')).toBe('key-1');
    expect(sent.headers.get('X-CSRF-Token')).toBe('csrf-1');
    expect(sent.headers.get('X-Resume-Schema-Version')).toBe(
      String(CURRENT_VERSION),
    );
  });

  it('sends no If-Match on delete and clear', async () => {
    const fetcher = vi.fn().mockImplementation(
      async () => new Response(null, { status: 204 }),
    );
    const api = createHistoryApi(fetcher);
    const del = freezeHistoryWrite(
      {
        operation: 'delete',
        resumeId: 'resume-1',
        checkpointId: 'cp-1',
        ownerId: 'o',
      },
      runtime,
    );
    const clear = freezeHistoryWrite(
      { operation: 'clear', resumeId: 'resume-1', ownerId: 'o' },
      runtime,
    );
    expect(await api.write(del, 'csrf')).toEqual({ kind: 'done' });
    expect(await api.write(clear, 'csrf')).toEqual({ kind: 'done' });
    const first = request(fetcher, 0);
    const second = request(fetcher, 1);
    expect(first.method).toBe('DELETE');
    expect(new URL(first.url, 'http://x').pathname).toBe(
      '/api/v1/resumes/resume-1/checkpoints/cp-1',
    );
    expect(new URL(second.url, 'http://x').pathname).toBe(
      '/api/v1/resumes/resume-1/checkpoints',
    );
    for (const sent of [first, second]) {
      expect(sent.headers.get('If-Match')).toBeNull();
      expect(sent.headers.get('X-Resume-Schema-Version')).toBeNull();
      expect(sent.headers.get('Idempotency-Key')).toBe('key-1');
      expect(sent.headers.get('X-CSRF-Token')).toBe('csrf');
    }
  });

  it('reuses the frozen key when the same write is sent again', async () => {
    let n = 0;
    const counting = { uuid: () => `key-${++n}` };
    const write = freezeHistoryWrite(
      { operation: 'clear', resumeId: 'r', ownerId: 'o' },
      counting,
    );
    const fetcher = vi.fn().mockRejectedValue(new TypeError('offline'));
    const api = createHistoryApi(fetcher);
    expect(await api.write(write, 'c')).toEqual({ kind: 'network' });
    expect(await api.write(write, 'c')).toEqual({ kind: 'network' });
    expect(request(fetcher, 0).headers.get('Idempotency-Key')).toBe('key-1');
    expect(request(fetcher, 1).headers.get('Idempotency-Key')).toBe('key-1');
    expect(n).toBe(1);
  });

  it.each([
    [401, 'session_required', 'session-lost'],
    [403, 'csrf_rejected', 'csrf-rejected'],
    [404, 'resume_not_found', 'not-found'],
    [422, 'resume_document_invalid', 'invalid'],
    [429, 'rate_limited', 'busy'],
    [503, 'public_state_busy', 'busy'],
    [500, 'internal', 'failed'],
    [409, 'idempotency_key_reuse', 'failed'],
  ])('maps restore %i %s to %s', async (status, code, kind) => {
    const fetcher = vi.fn().mockResolvedValue(
      json(status, { error: { code, message: 'x' } }, { 'Retry-After': '7' }),
    );
    const result = await createHistoryApi(fetcher).write(
      restoreWrite(),
      'csrf',
    );
    expect(result.kind).toBe(kind);
    if (result.kind === 'busy') expect(result.retryAfterMs).toBe(7000);
  });

  it('returns the winner of a 412', async () => {
    const accepted = acceptedFixture();
    const fetcher = vi.fn().mockResolvedValue(
      json(412, {
        error: {
          code: 'revision_mismatch',
          message: 'x',
          details: { revision: '9', document: accepted.document },
        },
      }),
    );
    const result = await createHistoryApi(fetcher).write(
      restoreWrite(),
      'csrf',
    );
    expect(result.kind).toBe('stale');
    if (result.kind === 'stale') expect(result.winner.revision).toBe('9');
  });

  it('treats a transport error on delete as network', async () => {
    const fetcher = vi.fn().mockRejectedValue(new TypeError('offline'));
    const write = freezeHistoryWrite(
      { operation: 'delete', resumeId: 'r', checkpointId: 'c', ownerId: 'o' },
      runtime,
    );
    expect(await createHistoryApi(fetcher).write(write, 'c')).toEqual({
      kind: 'network',
    });
  });
});
