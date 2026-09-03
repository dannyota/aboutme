import { CURRENT_VERSION } from '@aboutme/schema/released';
import { describe, expect, it, vi } from 'vitest';

import {
  createPublishApi,
  freezePublishAttempt,
} from '../../app/editor/publishApi';
import type { EditorRuntime } from '../../app/editor/types';
import { parseRevision } from '../../app/editor/revision';
import { acceptedFixture } from './fixture';

const runtime: EditorRuntime = {
  nowEpochMs: () => 100,
  uuid: () => 'publish-key-1',
  delay: async () => {},
};

function response(
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

function acceptedBody() {
  const accepted = acceptedFixture();
  return {
    data: { ...accepted.metadata, revision: '2', document: accepted.document },
  };
}

function frozen(revision = '1') {
  return freezePublishAttempt(
    'resume-1',
    parseRevision(revision),
    { live: true, downloadEnabled: false, seoGeoEnabled: false },
    runtime,
    'owner-1',
  );
}

describe('publish API transport', () => {
  it(
    'freezes the exact request and sends publish headers and body',
    async () => {
      const fetcher = vi.fn().mockResolvedValue(
        response(200, acceptedBody(), {
          'ETag': '"r2"',
          'X-Resume-Schema-Version': String(CURRENT_VERSION),
        }),
      );
      const attempt = freezePublishAttempt(
        'resume-1',
        parseRevision('1'),
        {
          slug: 'ada-lovelace',
          live: true,
          downloadEnabled: true,
          seoGeoEnabled: false,
        },
        runtime,
        'owner-1',
      );

      await expect(
        createPublishApi(fetcher).dispatch(attempt, 'csrf-1'),
      ).resolves.toMatchObject({
        kind: 'accepted',
        resume: { revision: '2' },
      });
      const request = fetcher.mock.calls[0]![0] as Request;
      expect(request.url).toContain('/api/v1/resumes/resume-1/publish');
      expect(request.method).toBe('POST');
      expect(request.headers.get('If-Match')).toBe('"r1"');
      expect(request.headers.get('Idempotency-Key')).toBe('publish-key-1');
      expect(request.headers.get('X-CSRF-Token')).toBe('csrf-1');
      expect(request.headers.get('X-Resume-Schema-Version')).toBe(
        String(CURRENT_VERSION),
      );
      expect(request.headers.get('Content-Type')).toBe('application/json');
      expect(await request.text()).toBe(JSON.stringify(attempt.command));
    },
  );

  it(
    'maps closed status families and preserves only validation path/code',
    async () => {
      const cases: Array<[number, unknown, unknown]> = [
        [
          401,
          { error: { code: 'session_required', message: 'raw' } },
          { kind: 'session-lost' },
        ],
        [
          403,
          { error: { code: 'reauth_required', message: 'raw' } },
          { kind: 'reauth-required' },
        ],
        [
          409,
          { error: { code: 'slug_taken', message: 'raw' } },
          { kind: 'slug-taken' },
        ],
        [
          429,
          { error: { code: 'rate_limited', message: 'raw' } },
          { kind: 'rate-limited', retryAfterMs: null },
        ],
        [
          503,
          { error: { code: 'public_state_busy', message: 'raw' } },
          { kind: 'public-state-busy', retryAfterMs: 1000 },
        ],
      ];
      for (const [status, body, expected] of cases) {
        const headers = status === 503 ? { 'Retry-After': '1' } : {};
        await expect(
          createPublishApi(
            vi.fn().mockResolvedValue(response(status, body, headers)),
          ).dispatch(
            freezePublishAttempt(
              'resume-1',
              parseRevision('1'),
              {
                live: true,
                downloadEnabled: false,
                seoGeoEnabled: false,
              },
              runtime,
              'owner-1',
            ),
            'csrf',
          ),
        ).resolves.toEqual(expected);
      }

      await expect(
        createPublishApi(
          vi.fn().mockResolvedValue(
            response(422, {
              error: {
                code: 'publish_invalid',
                message: 'raw',
                details: {
                  issues: [
                    {
                      path: 'personalDetails.fullName',
                      code: 'required',
                      message: 'secret',
                    },
                  ],
                },
              },
            }),
          ),
        ).dispatch(
          freezePublishAttempt(
            'resume-1',
            parseRevision('1'),
            {
              live: true,
              downloadEnabled: false,
              seoGeoEnabled: false,
            },
            runtime,
            'owner-1',
          ),
          'csrf',
        ),
      ).resolves.toEqual({
        kind: 'invalid',
        issues: [{ path: 'personalDetails.fullName', code: 'required' }],
      });
    },
  );

  it(
    'refreshes CSRF only in the controller lane and exposes a rejected '
    + 'response',
    async () => {
      await expect(
        createPublishApi(
          vi.fn().mockResolvedValue(
            response(403, {
              error: { code: 'csrf_rejected', message: 'raw' },
            }),
          ),
        ).dispatch(
          freezePublishAttempt(
            'resume-1',
            parseRevision('1'),
            {
              live: true,
              downloadEnabled: false,
              seoGeoEnabled: false,
            },
            runtime,
            'owner-1',
          ),
          'csrf',
        ),
      ).resolves.toEqual({ kind: 'csrf-rejected' });
    },
  );

  it(
    'retains the same frozen key for an explicit uncertain retry',
    async () => {
      const fetcher = vi
        .fn()
        .mockRejectedValueOnce(new Error('network'))
        .mockResolvedValueOnce(
          response(200, acceptedBody(), {
            'ETag': '"r2"',
            'X-Resume-Schema-Version': String(CURRENT_VERSION),
          }),
        );
      const api = createPublishApi(fetcher);
      const attempt = freezePublishAttempt(
        'resume-1',
        parseRevision('1'),
        {
          live: true,
          downloadEnabled: false,
          seoGeoEnabled: false,
        },
        runtime,
        'owner-1',
      );
      await expect(api.dispatch(attempt, 'csrf')).resolves.toEqual({
        kind: 'unknown',
        reason: 'transport',
      });
      await expect(api.dispatch(attempt, 'csrf')).resolves.toMatchObject({
        kind: 'accepted',
      });
      expect(
        (fetcher.mock.calls[0]![0] as Request).headers.get('Idempotency-Key'),
      ).toBe(
        (fetcher.mock.calls[1]![0] as Request).headers.get('Idempotency-Key'),
      );
    },
  );

  it(
    'keeps 500 uncertain and replays it only with the same frozen key',
    async () => {
      const fetcher = vi
        .fn()
        .mockResolvedValueOnce(
          response(500, {
            error: { code: 'internal_error', message: 'do not expose' },
          }),
        )
        .mockResolvedValueOnce(
          response(200, acceptedBody(), {
            'ETag': '"r2"',
            'X-Resume-Schema-Version': String(CURRENT_VERSION),
          }),
        );
      const api = createPublishApi(fetcher);
      const attempt = frozen();
      await expect(api.dispatch(attempt, 'csrf')).resolves.toEqual({
        kind: 'unknown',
        reason: 'server',
      });
      await expect(api.dispatch(attempt, 'csrf')).resolves.toMatchObject({
        kind: 'accepted',
      });
      expect(
        (fetcher.mock.calls[0]![0] as Request).headers.get('Idempotency-Key'),
      ).toBe(attempt.idempotencyKey);
      expect(
        (fetcher.mock.calls[1]![0] as Request).headers.get('Idempotency-Key'),
      ).toBe(attempt.idempotencyKey);
    },
  );

  it(
    'rejects regressed success revisions and malformed status pairs',
    async () => {
      for (const revision of ['1', '0']) {
        const body = { data: { ...acceptedBody().data, revision } };
        const api = createPublishApi(
          vi.fn().mockResolvedValue(
            response(200, body, {
              'ETag': `"r${revision}"`,
              'X-Resume-Schema-Version': String(CURRENT_VERSION),
            }),
          ),
        );
        await expect(api.dispatch(frozen(), 'csrf')).resolves.toEqual({
          kind: 'failed',
          code: 'response_invalid',
        });
      }
      const mismatches: Array<[number, string]> = [
        [401, 'reauth_required'],
        [404, 'not_found'],
        [405, 'method_not_allowed'],
        [409, 'rate_limited'],
        [422, 'required'],
        [429, 'internal_error'],
        [503, 'internal_error'],
      ];
      for (const [status, code] of mismatches) {
        await expect(
          createPublishApi(
            vi
              .fn()
              .mockResolvedValue(
                response(
                  status,
                  { error: { code, message: 'raw' } },
                  status === 405 ? { Allow: 'GET' } : {},
                ),
              ),
          ).dispatch(frozen(), 'csrf'),
        ).resolves.toEqual({
          kind: 'failed',
          code: 'response_invalid',
        });
      }
    },
  );

  it(
    'accepts only documented Retry-After values and issue shapes',
    async () => {
      for (const retryAfter of [
        '0',
        '-1',
        '1.5',
        '9007199254741',
        '999999999999999999999',
      ]) {
        await expect(
          createPublishApi(
            vi
              .fn()
              .mockResolvedValue(
                response(
                  429,
                  { error: { code: 'rate_limited', message: 'raw' } },
                  { 'Retry-After': retryAfter },
                ),
              ),
          ).dispatch(frozen(), 'csrf'),
        ).resolves.toEqual({
          kind: 'rate-limited',
          retryAfterMs: null,
        });
      }
      await expect(
        createPublishApi(
          vi.fn().mockResolvedValue(
            response(422, {
              error: {
                code: 'publish_invalid',
                message: 'raw',
                details: {
                  issues: [
                    { path: 'slug', code: 'hostile', message: 'secret' },
                  ],
                },
              },
            }),
          ),
        ).dispatch(frozen(), 'csrf'),
      ).resolves.toEqual({
        kind: 'failed',
        code: 'response_invalid',
      });
    },
  );

  it('maps every documented non-special terminal status and code', async () => {
    const cases = [
      [400, 'invalid_client_ip'],
      [400, 'idempotency_key_invalid'],
      [400, 'idempotency_key_required'],
      [400, 'precondition_malformed'],
      [400, 'request_invalid'],
      [400, 'unsupported_schema_version'],
      [404, 'resume_not_found'],
      [409, 'idempotency_key_reuse'],
      [413, 'body_too_large'],
      [428, 'precondition_required'],
    ] as const;
    for (const [status, code] of cases) {
      const result = await createPublishApi(
        vi
          .fn()
          .mockResolvedValue(
            response(status, { error: { code, message: 'raw sentinel' } }),
          ),
      ).dispatch(frozen(), 'csrf');
      expect(result).toEqual({ kind: 'failed', code });
      expect(JSON.stringify(result)).not.toContain('sentinel');
    }

    await expect(
      createPublishApi(
        vi.fn().mockResolvedValue(
          response(
            405,
            {
              error: { code: 'method_not_allowed', message: 'raw sentinel' },
            },
            { Allow: 'POST' },
          ),
        ),
      ).dispatch(frozen(), 'csrf'),
    ).resolves.toEqual({
      kind: 'failed',
      code: 'method_not_allowed',
    });
  });

  it('validates stale winners and all closed publish issue codes', async () => {
    const fixture = acceptedBody().data.document;
    await expect(
      createPublishApi(
        vi.fn().mockResolvedValue(
          response(412, {
            error: {
              code: 'revision_mismatch',
              message: 'raw sentinel',
              details: { revision: '2', document: fixture },
            },
          }),
        ),
      ).dispatch(frozen(), 'csrf'),
    ).resolves.toMatchObject({
      kind: 'stale',
      winner: { revision: '2' },
    });

    const issues = [
      'required_for_live',
      'requires_live',
      'invalid_format',
      'reserved',
      'required',
      'visible_entry_required',
    ].map((code, index) => ({
      path: `path.${index}`,
      code,
      message: `raw sentinel ${index}`,
    }));
    const result = await createPublishApi(
      vi.fn().mockResolvedValue(
        response(422, {
          error: {
            code: 'publish_invalid',
            message: 'resume cannot be published',
            details: { issues },
          },
        }),
      ),
    ).dispatch(frozen(), 'csrf');
    expect(result).toEqual({
      kind: 'invalid',
      issues: issues.map(({ path, code }) => ({ path, code })),
    });
    expect(JSON.stringify(result)).not.toContain('sentinel');
  });

  it(
    'preserves int64 revisions without a JavaScript number conversion',
    async () => {
      const base = '9223372036854775806';
      const next = '9223372036854775807';
      const body = { data: { ...acceptedBody().data, revision: next } };
      const fetcher = vi.fn().mockResolvedValue(
        response(200, body, {
          'ETag': `"r${next}"`,
          'X-Resume-Schema-Version': String(CURRENT_VERSION),
        }),
      );
      await expect(
        createPublishApi(fetcher).dispatch(frozen(base), 'csrf'),
      ).resolves.toMatchObject({
        kind: 'accepted',
        resume: { revision: next },
      });
      expect(
        (fetcher.mock.calls[0]![0] as Request).headers.get('If-Match'),
      ).toBe(`"r${base}"`);
    },
  );

  it(
    'fails closed on malformed stale, error, cache, and success responses',
    async () => {
      const cases = [
        response(412, {
          error: { code: 'revision_mismatch', message: 'x', details: {} },
        }),
        response(422, {
          error: { code: 'publish_invalid', message: 'x', details: {} },
        }),
        response(409, { error: { code: 'slug_taken' } }),
        response(404, { error: { code: 4, message: 'x' } }),
        response(
          404,
          { error: { code: 'resume_not_found', message: 'x' } },
          {
            'Cache-Control': 'public',
          },
        ),
        response(200, acceptedBody(), {
          'ETag': '"r2"',
          'X-Resume-Schema-Version': '999',
        }),
        response(200, acceptedBody(), {
          'X-Resume-Schema-Version': String(CURRENT_VERSION),
        }),
        response(
          200,
          { data: { ...acceptedBody().data, id: 'resume-2' } },
          {
            'ETag': '"r2"',
            'X-Resume-Schema-Version': String(CURRENT_VERSION),
          },
        ),
        response(
          200,
          {
            data: {
              ...acceptedBody().data,
              live: true,
              slug: '//evil.example',
            },
          },
          {
            'ETag': '"r2"',
            'X-Resume-Schema-Version': String(CURRENT_VERSION),
          },
        ),
      ];
      for (const [index, invalid] of cases.entries()) {
        const result = await createPublishApi(
          vi.fn().mockResolvedValue(invalid),
        ).dispatch(frozen(), 'csrf');
        expect(result, `malformed case ${index}`).toEqual({
          kind: 'failed',
          code: 'response_invalid',
        });
      }
    },
  );

  it('parses only positive safe Retry-After seconds', async () => {
    for (const [header, expected] of [
      ['3', 3000],
      ['0', null],
      ['9007199254741', null],
    ] as const) {
      await expect(
        createPublishApi(
          vi
            .fn()
            .mockResolvedValue(
              response(
                429,
                { error: { code: 'rate_limited', message: 'raw' } },
                { 'Retry-After': header },
              ),
            ),
        ).dispatch(frozen(), 'csrf'),
      ).resolves.toEqual({
        kind: 'rate-limited',
        retryAfterMs: expected,
      });
    }
  });
});
