import { describe, expect, it, vi } from 'vitest';

import {
  createProjectEvidenceApi,
  parseRetryAfter,
} from '../../app/composables/projectEvidenceApi';
import {
  candidate,
  EVIDENCE_IDS,
  evidenceFailed,
  evidenceItem,
  project,
  written,
} from './fixtures';

const BASE = 'http://localhost/api/v1';
const PROJECT = '0190f000-0000-7000-8000-0000000000c1';
const EVIDENCE = EVIDENCE_IDS[0];

class PlainHeadersRequest extends Request {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    const headers = Object.fromEntries(new Headers(init?.headers).entries());
    super(input, { ...init, headers });
  }
}

function respond(
  status: number,
  body?: unknown,
  headers: Record<string, string> = {},
): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: {
      'Cache-Control': 'no-store, no-transform',
      'Content-Type': 'application/json',
      ...headers,
    },
  });
}

function problem(
  status: number,
  code: string,
  extra: { details?: unknown; headers?: Record<string, string> } = {},
): Response {
  return respond(status, {
    error: {
      code,
      message: 'fixed',
      ...(extra.details === undefined ? {} : { details: extra.details }),
    },
  }, extra.headers);
}

function recorder(...responses: Response[]) {
  const requests: Request[] = [];
  const fetch = vi.fn(async (request: Request) => {
    requests.push(request.clone());
    return responses.shift() ?? respond(500);
  });
  return {
    requests,
    api: createProjectEvidenceApi({
      baseUrl: BASE, fetch, Request: PlainHeadersRequest,
    }),
  };
}

const writtenBody = {
  data: {
    evidence: [evidenceItem()],
    projectRevision: '4',
    resumeRevisions: [{ id: 'resume-1', revision: '9' }],
  },
};

describe('project evidence API requests', () => {
  it('lists candidates with kind, page and the CSRF token', async () => {
    const page = {
      items: [candidate({ kind: 'pullRequest', repository: {
        id: null, fullName: 'octocat/hello',
      } })],
      nextPage: 3,
      incomplete: true,
    };
    const { requests, api } = recorder(respond(200, { data: page }));

    const result = await api.candidates(
      PROJECT, { kind: 'pullRequest', page: 2 }, 'csrf-1',
    );

    expect(result).toEqual({ kind: 'ok', value: page });
    const [request] = requests;
    expect(request?.method).toBe('GET');
    const url = new URL(request!.url);
    expect(url.pathname)
      .toBe(`/api/v1/projects/${PROJECT}/evidence/candidates`);
    expect([...url.searchParams.entries()])
      .toEqual([['kind', 'pullRequest'], ['page', '2']]);
    expect(request?.headers.get('X-CSRF-Token')).toBe('csrf-1');
  });

  it('names the repository only for releases', async () => {
    const empty = { data: { items: [], nextPage: null, incomplete: false } };
    const { requests, api } = recorder(
      respond(200, empty), respond(200, empty),
    );

    await api.candidates(
      PROJECT, { kind: 'release', page: 1, repository: 101 }, 'c',
    );
    await api.candidates(
      PROJECT, { kind: 'repository', page: 1, repository: 101 }, 'c',
    );

    expect([...new URL(requests[0]!.url).searchParams.entries()])
      .toEqual([['kind', 'release'], ['page', '1'], ['repository', '101']]);
    expect([...new URL(requests[1]!.url).searchParams.entries()])
      .toEqual([['kind', 'repository'], ['page', '1']]);
  });

  it.each([
    ['a candidate list', { kind: 'pullRequest', candidateId: 'abc' }],
    ['a link', {
      kind: 'pullRequest', link: 'https://github.com/octocat/hello/pull/42',
    }],
  ] as const)('attaches from %s with If-Match, Idempotency-Key and the CSRF '
    + 'token', async (_name, body) => {
    const { requests, api } = recorder(respond(200, writtenBody));

    const result = await api.attach(PROJECT, '3', body, 'key-1', 'csrf-1');

    expect(result).toEqual(written([evidenceItem()], '4'));
    const [request] = requests;
    expect(request?.method).toBe('POST');
    expect(new URL(request!.url).pathname)
      .toBe(`/api/v1/projects/${PROJECT}/evidence`);
    expect(request?.headers.get('If-Match')).toBe('"r3"');
    expect(request?.headers.get('Idempotency-Key')).toBe('key-1');
    expect(request?.headers.get('X-CSRF-Token')).toBe('csrf-1');
    expect(await request?.json()).toEqual(body);
  });

  it('checks again and removes without a body', async () => {
    const { requests, api } = recorder(
      respond(200, writtenBody), respond(200, writtenBody),
    );

    await api.recheck(PROJECT, EVIDENCE, '3', 'k1', 'c');
    await api.remove(PROJECT, EVIDENCE, '4', 'k2', 'c');

    expect(requests.map((request) => [
      request.method, new URL(request.url).pathname,
      request.headers.get('If-Match'), request.headers.get('Idempotency-Key'),
    ])).toEqual([
      ['POST', `/api/v1/projects/${PROJECT}/evidence/${EVIDENCE}/recheck`,
        '"r3"', 'k1'],
      ['DELETE', `/api/v1/projects/${PROJECT}/evidence/${EVIDENCE}`,
        '"r4"', 'k2'],
    ]);
    expect(await requests[0]?.text()).toBe('');
    expect(await requests[1]?.text()).toBe('');
  });

  it('reorders with every id once', async () => {
    const { requests, api } = recorder(respond(200, writtenBody));
    const order = [EVIDENCE_IDS[1], EVIDENCE_IDS[0]];

    await api.reorder(PROJECT, '3', order, 'k', 'c');

    const [request] = requests;
    expect(request?.method).toBe('PUT');
    expect(new URL(request!.url).pathname)
      .toBe(`/api/v1/projects/${PROJECT}/evidence/order`);
    expect(request?.headers.get('If-Match')).toBe('"r3"');
    expect(await request?.json()).toEqual({ order });
  });
});

describe('project evidence API answers', () => {
  it.each([
    [404, 'evidence_unavailable', 'evidence-unavailable'],
    [404, 'project_not_found', 'project-not-found'],
    [404, 'evidence_not_found', 'evidence-not-found'],
    [409, 'evidence_session_required', 'session-required'],
    [409, 'evidence_duplicate', 'duplicate'],
    [409, 'idempotency_key_reuse', 'key-reuse'],
    [422, 'evidence_not_confirmed', 'not-confirmed'],
    [422, 'evidence_limit', 'limit'],
    [422, 'evidence_unsupported_link', 'unsupported-link'],
    [422, 'document_invalid', 'size'],
    [502, 'evidence_upstream', 'upstream'],
    [429, 'rate_limited', 'rate-limited'],
    [503, 'public_state_busy', 'busy'],
    [400, 'request_invalid', 'invalid'],
    [401, 'session_required', 'session-lost'],
    [403, 'csrf_rejected', 'csrf-rejected'],
    [413, 'body_too_large', 'too-large'],
    [500, 'internal', 'unavailable'],
    [412, 'revision_mismatch', 'unavailable'],
  ] as const)('maps %i %s on a write', async (status, code, failure) => {
    const { api } = recorder(problem(status, code));

    expect(await api.recheck(PROJECT, EVIDENCE, '3', 'k', 'c'))
      .toEqual(evidenceFailed(failure));
  });

  it('maps the same codes on the candidate list', async () => {
    const { api } = recorder(
      problem(409, 'evidence_session_required'),
      problem(502, 'evidence_upstream'),
      problem(403, 'csrf_rejected'),
    );
    const query = { kind: 'repository', page: 1 } as const;

    expect(await api.candidates(PROJECT, query, 'c'))
      .toEqual(evidenceFailed('session-required'));
    expect(await api.candidates(PROJECT, query, 'c'))
      .toEqual(evidenceFailed('upstream'));
    expect(await api.candidates(PROJECT, query, 'c'))
      .toEqual(evidenceFailed('csrf-rejected'));
  });

  it('returns the winner on a revision mismatch', async () => {
    const winner = project({
      id: PROJECT, revision: '7', evidence: [evidenceItem()],
    });
    const { api } = recorder(problem(412, 'revision_mismatch', {
      details: { revision: '7', project: winner },
    }));

    expect(await api.remove(PROJECT, EVIDENCE, '3', 'k', 'c'))
      .toEqual({ kind: 'stale', project: winner });
  });

  it('carries Retry-After on a rate limit', async () => {
    const { api } = recorder(
      problem(429, 'rate_limited', { headers: { 'Retry-After': '120' } }),
      problem(429, 'rate_limited'),
    );
    const query = { kind: 'pullRequest', page: 1 } as const;

    expect(await api.candidates(PROJECT, query, 'c'))
      .toEqual(evidenceFailed('rate-limited', 120));
    expect(await api.candidates(PROJECT, query, 'c'))
      .toEqual(evidenceFailed('rate-limited'));
  });

  it.each([
    ['120', 120],
    ['1', 1],
    [' 60 ', 60],
    ['0', null],
    ['-5', null],
    ['1.5', null],
    ['soon', null],
    ['', null],
    [null, null],
  ] as const)('parses Retry-After %j as %j', (value, seconds) => {
    expect(parseRetryAfter(value)).toBe(seconds);
  });

  it('treats a network failure as unavailable', async () => {
    const api = createProjectEvidenceApi({
      baseUrl: BASE,
      fetch: vi.fn(async () => {
        throw new TypeError('offline');
      }),
    });

    expect(await api.remove(PROJECT, EVIDENCE, '3', 'k', 'c'))
      .toEqual(evidenceFailed('unavailable'));
  });

  it.each([
    ['no evidence array', { data: { projectRevision: '4' } }],
    ['a url outside github.com', {
      data: {
        ...writtenBody.data,
        evidence: [evidenceItem({ url: 'https://example.com/octocat' })],
      },
    }],
    ['a github.com lookalike', {
      data: {
        ...writtenBody.data,
        evidence: [evidenceItem({ url: 'https://github.com.example.net/a' })],
      },
    }],
    ['an unknown kind', {
      data: {
        ...writtenBody.data,
        evidence: [{ ...evidenceItem(), kind: 'issue' }],
      },
    }],
    ['an unknown flag reason', {
      data: {
        ...writtenBody.data,
        evidence: [{ ...evidenceItem(), flagReason: 'stale' }],
      },
    }],
    ['a non-numeric revision', {
      data: { ...writtenBody.data, projectRevision: 'r4' },
    }],
    ['eleven items', {
      data: {
        ...writtenBody.data,
        evidence: Array.from({ length: 11 }, () => evidenceItem()),
      },
    }],
  ])('treats a write answer with %s as unavailable', async (_name, body) => {
    const { api } = recorder(respond(200, body));

    expect(await api.recheck(PROJECT, EVIDENCE, '3', 'k', 'c'))
      .toEqual(evidenceFailed('unavailable'));
  });

  it.each([
    ['a missing nextPage', { items: [], incomplete: false }],
    ['a malformed candidate', {
      items: [{ ...candidate(), candidateId: 7 }],
      nextPage: null, incomplete: false,
    }],
    ['a non-boolean incomplete', { items: [], nextPage: null, incomplete: 1 }],
  ])('treats a candidate page with %s as unavailable', async (_name, page) => {
    const { api } = recorder(respond(200, { data: page }));

    expect(await api.candidates(
      PROJECT, { kind: 'repository', page: 1 }, 'c',
    )).toEqual(evidenceFailed('unavailable'));
  });
});
