import { CURRENT_VERSION } from '@aboutme/schema/released';
import { describe, expect, it, vi } from 'vitest';

import {
  createResumeApi,
  freezeCreateAttempt,
  parseObjectETag,
} from '../../app/editor/resumeApi';
import type { FrozenAttempt } from '../../app/editor/attempt';
import type { EditorRuntime } from '../../app/editor/types';
import { acceptedFixture } from './fixture';

const runtime: EditorRuntime = {
  nowEpochMs: () => 0,
  uuid: () => 'key-1',
  delay: async () => {},
};

const cacheHeaders = {
  'Cache-Control': 'no-store, no-transform',
  'Content-Type': 'application/json',
};

function response(
  status: number,
  body: unknown,
  headers = cacheHeaders,
): Response {
  return new Response(body === null ? null : JSON.stringify(body), {
    status,
    headers,
  });
}

function error(code: string, details?: unknown): { error: unknown } {
  return {
    error: {
      code,
      message: 'raw server message',
      ...(details === undefined ? {} : { details }),
    },
  };
}

function resumeData() {
  const accepted = acceptedFixture();
  return {
    ...accepted.metadata,
    revision: accepted.revision,
    document: accepted.document,
  };
}

function createAttempt(): FrozenAttempt {
  return freezeCreateAttempt(
    {
      kind: 'resumeCreate',
      id: 'create-1',
      ownerId: 'owner-1',
      sequence: 0,
      title: 'Fixture',
    },
    runtime,
  );
}

describe('resume API transport', () => {
  it('drops raw validation messages at the transport boundary', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          error: {
            code: 'document_invalid',
            message: 'raw envelope',
            details: {
              issues: [
                {
                  path: 'content.work',
                  code: 'required',
                  message: 'sentinel raw issue',
                },
              ],
            },
          },
        }),
        {
          status: 422,
          headers: {
            'Cache-Control': 'no-store, no-transform',
            'Content-Type': 'application/json',
          },
        },
      ),
    );

    const result = await createResumeApi(fetcher).dispatch(
      freezeCreateAttempt(
        {
          kind: 'resumeCreate',
          id: 'create-1',
          ownerId: 'owner-1',
          sequence: 0,
          title: 'Fixture',
        },
        runtime,
      ),
      'csrf',
    );

    expect(result).toEqual({
      kind: 'validation-rejected',
      issues: [{ path: 'content.work', code: 'required' }],
    });
    expect(JSON.stringify(result)).not.toContain('sentinel');
  });

  it.each([
    [401, error('unauthorized'), { kind: 'session-lost' }],
    [403, error('csrf_rejected'), { kind: 'csrf-rejected' }],
    [409, error('idempotency_key_reuse'), { kind: 'idempotency-reuse' }],
    [400, error('bad_request'), { kind: 'rejected', code: 'bad_request' }],
    [
      413,
      error('body_too_large'),
      { kind: 'rejected', code: 'body_too_large' },
    ],
    [
      415,
      error('media_type_unsupported'),
      { kind: 'rejected', code: 'media_type_unsupported' },
    ],
    [429, error('rate_limited'), { kind: 'rate-limited', retryAfterMs: null }],
    [503, error('media_busy'), { kind: 'media-busy', retryAfterMs: null }],
  ] as const)(
    'maps closed dispatch failure %s',
    async (status, body, expected) => {
      const fetcher = vi.fn().mockResolvedValue(response(status, body));
      await expect(
        createResumeApi(fetcher).dispatch(createAttempt(), 'csrf'),
      ).resolves.toEqual(expected);
    },
  );

  it('accepts only a matching complete owner response', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        response(
          201,
          { data: resumeData() },
          {
            ...cacheHeaders,
            'ETag': '"r1"',
            'X-Resume-Schema-Version': String(CURRENT_VERSION),
          },
        ),
      );

    await expect(
      createResumeApi(fetcher).dispatch(createAttempt(), 'csrf'),
    ).resolves.toMatchObject({
      kind: 'complete',
      status: 201,
      accepted: { revision: '1' },
    });
  });

  it('rejects cache, schema, and ETag disagreement', async () => {
    const body = { data: resumeData() };
    const responses = [
      response(200, body, {
        ...cacheHeaders,
        'ETag': '"r1"',
        'X-Resume-Schema-Version': String(CURRENT_VERSION),
        'Cache-Control': 'no-store',
      }),
      response(200, body, {
        ...cacheHeaders,
        'ETag': '"r1"',
        'X-Resume-Schema-Version': '1',
      }),
      response(200, body, {
        ...cacheHeaders,
        'ETag': '"r2"',
        'X-Resume-Schema-Version': String(CURRENT_VERSION),
      }),
    ];
    for (const candidate of responses) {
      const fetcher = vi.fn().mockResolvedValue(candidate);
      await expect(
        createResumeApi(fetcher).dispatch(createAttempt(), 'csrf'),
      ).resolves.toEqual({
        kind: 'unknown',
        reason: 'server',
      });
    }
  });

  it('rejects a complete response below its frozen revision', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      response(
        200,
        { data: resumeData() },
        {
          ...cacheHeaders,
          'ETag': '"r1"',
          'X-Resume-Schema-Version': String(CURRENT_VERSION),
        },
      ),
    );
    const attempt = { ...createAttempt(), ifMatch: '"r2"' } as FrozenAttempt;

    await expect(
      createResumeApi(fetcher).dispatch(attempt, 'csrf'),
    ).resolves.toEqual({ kind: 'unknown', reason: 'server' });
  });

  it('parses valid stale winners without summary metadata', async () => {
    const valid = response(
      412,
      error('revision_mismatch', {
        revision: '2',
        document: acceptedFixture().document,
      }),
    );
    const malformed = response(
      412,
      error('revision_mismatch', { revision: '0' }),
    );

    await expect(
      createResumeApi(vi.fn().mockResolvedValue(valid)).dispatch(
        createAttempt(),
        'csrf',
      ),
    ).resolves.toEqual({
      kind: 'stale',
      status: 412,
      winner: { revision: '2', document: acceptedFixture().document },
    });
    await expect(
      createResumeApi(vi.fn().mockResolvedValue(malformed)).dispatch(
        createAttempt(),
        'csrf',
      ),
    ).resolves.toEqual({
      kind: 'unknown',
      reason: 'server',
    });
  });

  it('maps exact bodyless child and resume acknowledgements', async () => {
    const entryAttempt = {
      ...createAttempt(),
      operation: 'deleteResumeEntry' as const,
      method: 'DELETE' as const,
      payload: { kind: 'empty' as const },
    };
    const deleteAttempt = {
      ...entryAttempt,
      operation: 'deleteResume' as const,
    };
    const child = new Response(null, {
      status: 204,
      headers: {
        'Cache-Control': 'no-store, no-transform',
        'ETag': '"r2"',
        'X-Resume-Schema-Version': String(CURRENT_VERSION),
      },
    });
    const deleted = new Response(null, {
      status: 204,
      headers: { 'Cache-Control': 'no-store, no-transform' },
    });

    await expect(
      createResumeApi(vi.fn().mockResolvedValue(child)).dispatch(
        entryAttempt,
        'csrf',
      ),
    ).resolves.toEqual({
      kind: 'child-ack',
      status: 204,
      scope: 'entry',
      etag: '"r2"',
    });
    await expect(
      createResumeApi(vi.fn().mockResolvedValue(child)).dispatch(
        { ...entryAttempt, operation: 'deleteResumePhoto' },
        'csrf',
      ),
    ).resolves.toEqual({
      kind: 'child-ack',
      status: 204,
      scope: 'photo',
      etag: '"r2"',
    });
    await expect(
      createResumeApi(vi.fn().mockResolvedValue(deleted)).dispatch(
        deleteAttempt,
        'csrf',
      ),
    ).resolves.toEqual({
      kind: 'resume-deleted',
      status: 204,
    });
  });

  it('contains transport and server failures', async () => {
    await expect(
      createResumeApi(vi.fn().mockRejectedValue(new Error('network'))).dispatch(
        createAttempt(),
        'csrf',
      ),
    ).resolves.toEqual({
      kind: 'unknown',
      reason: 'transport',
    });
    await expect(
      createResumeApi(
        vi.fn().mockResolvedValue(response(500, error('internal_error'))),
      ).dispatch(createAttempt(), 'csrf'),
    ).resolves.toEqual({
      kind: 'unknown',
      reason: 'server',
    });
  });

  it('maps list and owner reads without malformed complete data', async () => {
    const data = resumeData();
    const list = createResumeApi(
      vi.fn().mockResolvedValue(
        response(
          200,
          { data: [data] },
          {
            ...cacheHeaders,
            'X-Resume-Schema-Version': String(CURRENT_VERSION),
          },
        ),
      ),
    );
    const read = createResumeApi(
      vi.fn().mockResolvedValue(
        response(
          200,
          { data },
          {
            ...cacheHeaders,
            'ETag': '"r1"',
            'X-Resume-Schema-Version': String(CURRENT_VERSION),
          },
        ),
      ),
    );

    await expect(list.list()).resolves.toEqual({
      kind: 'ready',
      items: [
        {
          ...acceptedFixture().metadata,
          revision: '1',
        },
      ],
    });
    await expect(read.read('resume-1')).resolves.toMatchObject({
      kind: 'complete',
      accepted: { revision: '1' },
    });
    await expect(
      createResumeApi(
        vi.fn().mockResolvedValue(response(404, error('resume_not_found'))),
      ).read('resume-1'),
    ).resolves.toEqual({ kind: 'unavailable' });
    await expect(
      createResumeApi(
        vi.fn().mockResolvedValue(response(401, error('unauthorized'))),
      ).list(),
    ).resolves.toEqual({ kind: 'session-lost' });
    await expect(
      createResumeApi(
        vi
          .fn()
          .mockResolvedValue(
            response(429, error('rate_limited'), {
              ...cacheHeaders,
              'Retry-After': '3',
            }),
          ),
      ).read('resume-1'),
    ).resolves.toEqual({
      kind: 'rate-limited',
      retryAfterMs: 3000,
    });
  });

  it('requires and retains required download and discovery flags', async () => {
    const data = {
      ...resumeData(),
      downloadEnabled: true,
      seoGeoEnabled: false,
    };
    const listHeaders = {
      ...cacheHeaders,
      'X-Resume-Schema-Version': String(CURRENT_VERSION),
    };
    const readHeaders = {
      ...listHeaders,
      ETag: '"r1"',
    };

    await expect(
      createResumeApi(
        vi.fn().mockResolvedValue(response(200, { data: [data] }, listHeaders)),
      ).list(),
    ).resolves.toMatchObject({
      kind: 'ready',
      items: [{ downloadEnabled: true, seoGeoEnabled: false }],
    });
    await expect(
      createResumeApi(
        vi.fn().mockResolvedValue(response(200, { data }, readHeaders)),
      ).read('resume-1'),
    ).resolves.toMatchObject({
      kind: 'complete',
      accepted: {
        metadata: { downloadEnabled: true, seoGeoEnabled: false },
      },
    });
    await expect(
      createResumeApi(
        vi.fn().mockResolvedValue(
          response(
            200,
            { data: [{ ...data, downloadEnabled: undefined }] },
            listHeaders,
          ),
        ),
      ).list(),
    ).resolves.toEqual({ kind: 'failed', reason: 'response-invalid' });
    await expect(
      createResumeApi(
        vi.fn().mockResolvedValue(
          response(
            200,
            { data: { ...data, seoGeoEnabled: 'false' } },
            readHeaders,
          ),
        ),
      ).read('resume-1'),
    ).resolves.toEqual({ kind: 'failed', reason: 'response-invalid' });
  });

  it('closes list and read network and response failures', async () => {
    await expect(
      createResumeApi(vi.fn().mockRejectedValue(new Error('network'))).list(),
    ).resolves.toEqual({ kind: 'failed', reason: 'network' });
    await expect(
      createResumeApi(
        vi.fn().mockResolvedValue(response(500, error('internal_error'))),
      ).read('resume-1'),
    ).resolves.toEqual({ kind: 'failed', reason: 'response-invalid' });
    await expect(
      createResumeApi(
        vi
          .fn()
          .mockResolvedValue(
            response(200, { data: [] }, {
              ...cacheHeaders,
              'Cache-Control': 'no-store',
            }),
          ),
      ).list(),
    ).resolves.toEqual({ kind: 'failed', reason: 'response-invalid' });
  });

  it.each([
    [200, { 'Content-Type': 'image/png', 'ETag': '"photo-1"' }, 'bytes'],
    [304, { ETag: '"photo-1"' }, 'not-modified'],
    [
      304,
      { 'ETag': '"photo-1"', 'Content-Type': 'text/html' },
      'unavailable',
    ],
    [
      200,
      { 'Content-Type': 'image/svg+xml', 'ETag': '"photo-1"' },
      'unavailable',
    ],
    [
      200,
      { 'Content-Type': 'text/html', 'ETag': '"photo-1"' },
      'unavailable',
    ],
  ] as const)('maps owner photo %s to %s', async (status, headers, kind) => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(
          status === 200 ? new Uint8Array([0x89, 0x50, 0x4e, 0x47]) : null,
          {
            status,
            headers: { 'Cache-Control': 'no-store, no-transform', ...headers },
          },
        ),
      );
    expect(
      (await createResumeApi(fetcher).readOwnerPhoto('resume-1')).kind,
    ).toBe(kind);
  });

  it('rejects weak object tags and sends valid photo tags', async () => {
    expect(() => parseObjectETag('W/"photo-1"')).toThrow();
    expect(parseObjectETag('"ordinary"')).toBe('"ordinary"');
    expect(parseObjectETag('"s"')).toBe('"s"');
    for (const invalid of [
      '"a b"',
      '"a,b"',
      '"a\\b"',
      '"a\t"',
      '"a\n"',
      '"a\u0000"',
      '"a\u001f"',
      '"a\u007f"',
    ]) {
      expect(() => parseObjectETag(invalid)).toThrow();
    }
    const fetcher = vi.fn().mockResolvedValue(
      new Response(null, {
        status: 304,
        headers: {
          'Cache-Control': 'no-store, no-transform',
          'ETag': 'W/"photo-1"',
        },
      }),
    );
    await expect(
      createResumeApi(fetcher).readOwnerPhoto(
        'resume-1',
        parseObjectETag('"photo-0"'),
      ),
    ).resolves.toEqual({
      kind: 'unavailable',
      reason: 'invalid',
    });
    expect(
      (fetcher.mock.calls[0]?.[0] as Request).headers.get('If-None-Match'),
    ).toBe('"photo-0"');
  });

  it('rejects a photo response without a strong object tag', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response(new Uint8Array([1]), {
        status: 200,
        headers: {
          'Cache-Control': 'no-store, no-transform',
          'Content-Type': 'image/png',
        },
      }),
    );

    await expect(
      createResumeApi(fetcher).readOwnerPhoto('resume-1'),
    ).resolves.toEqual({ kind: 'unavailable', reason: 'invalid' });
  });
});
