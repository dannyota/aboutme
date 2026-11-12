import { describe, expect, it, vi } from 'vitest';

import {
  createProjectLinker,
  createProjectLinksApi,
  rememberUnlinked,
  takeUnlinked,
  type LinkResult,
} from '../../app/components/projects/projectLinks';
import type { ResumeListResult } from '../../app/editor/resumeApi';
import { fakeAuth, resumeSummary as summary, sequence } from './fixtures';

const BASE = 'http://localhost/api/v1';
const PROJECT = '0190f000-0000-7000-8000-0000000000c1';

// The test environment's Request drops headers passed as a Headers object,
// which is how the generated client hands them over; browsers keep them.
class PlainHeadersRequest extends Request {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    const headers = Object.fromEntries(new Headers(init?.headers).entries());
    super(input, { ...init, headers });
  }
}

function respond(status: number, body?: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: {
      'Cache-Control': 'no-store, no-transform',
      'Content-Type': 'application/json',
    },
  });
}

function problem(status: number, code: string): Response {
  return respond(status, { error: { code, message: 'fixed' } });
}

function recorder(...responses: Response[]) {
  const requests: Request[] = [];
  const fetch = vi.fn(async (request: Request) => {
    requests.push(request.clone());
    return responses.shift() ?? respond(500);
  });
  return {
    requests,
    api: createProjectLinksApi({
      baseUrl: BASE, fetch, Request: PlainHeadersRequest,
    }),
  };
}

function ready(...items: unknown[]): ResumeListResult {
  return { kind: 'ready', items: items as never };
}

const ok: LinkResult = { kind: 'ok' };
const failed = (failure: string) => ({ kind: 'failed', failure }) as LinkResult;

function linkerSetup() {
  const { auth, user, csrfToken } = fakeAuth();
  const links = { link: vi.fn() };
  const resumes = { list: vi.fn() };
  const linker = createProjectLinker({
    links, resumes, auth, uuid: sequence(),
  });
  return { auth, user, csrfToken, links, resumes, linker };
}

describe('project link client', () => {
  it('posts the project id with If-Match, Idempotency-Key and the CSRF token',
    async () => {
      const { requests, api } = recorder(respond(200, { data: {} }));

      expect(await api.link('resume-1', '5', PROJECT, 'key-1', 'csrf'))
        .toEqual(ok);

      const [request] = requests;
      expect(request?.method).toBe('POST');
      expect(new URL(request!.url).pathname)
        .toBe('/api/v1/resumes/resume-1/project-links');
      expect(request?.headers.get('If-Match')).toBe('"r5"');
      expect(request?.headers.get('Idempotency-Key')).toBe('key-1');
      expect(request?.headers.get('X-CSRF-Token')).toBe('csrf');
      expect(await request?.json()).toEqual({ projectId: PROJECT });
    });

  it.each([
    [409, 'project_already_linked', 'already-linked'],
    [412, 'revision_mismatch', 'stale'],
    [422, 'document_invalid', 'document-invalid'],
    [503, 'public_state_busy', 'busy'],
    [429, 'rate_limited', 'rate-limited'],
    [500, 'internal_error', 'unavailable'],
    [401, 'session_required', 'session-lost'],
    [403, 'csrf_rejected', 'csrf-rejected'],
    [404, 'project_not_found', 'project-not-found'],
    [404, 'resume_not_found', 'resume-not-found'],
  ] as const)('maps %i %s', async (status, code, failure) => {
    const { api } = recorder(problem(status, code));

    expect(await api.link('resume-1', '5', PROJECT, 'k', 'c'))
      .toEqual(failed(failure));
  });

  it('treats a network failure as unavailable', async () => {
    const api = createProjectLinksApi({
      baseUrl: BASE,
      fetch: vi.fn(async () => {
        throw new TypeError('offline');
      }),
    });

    expect(await api.link('resume-1', '5', PROJECT, 'k', 'c'))
      .toEqual(failed('unavailable'));
  });
});

describe('project linker', () => {
  it('reads the resume revision when the action starts and links with it',
    async () => {
      const { links, resumes, linker } = linkerSetup();
      resumes.list.mockResolvedValue(ready(summary('resume-1', '7')));
      links.link.mockResolvedValue(ok);

      expect(await linker.link('owner-1', 'resume-1', PROJECT)).toEqual(ok);

      expect(resumes.list).toHaveBeenCalledOnce();
      expect(links.link).toHaveBeenCalledWith(
        'resume-1', '7', PROJECT, 'k1', 'csrf-1',
      );
    });

  it('refetches on a revision mismatch and repeats once with a fresh key',
    async () => {
      const { links, resumes, linker } = linkerSetup();
      resumes.list
        .mockResolvedValueOnce(ready(summary('resume-1', '7')))
        .mockResolvedValueOnce(ready(summary('resume-1', '8')))
        .mockResolvedValueOnce(ready(summary('resume-1', '9')));
      links.link.mockResolvedValue(failed('stale'));

      expect(await linker.link('owner-1', 'resume-1', PROJECT))
        .toEqual(failed('stale'));

      expect(links.link.mock.calls.map((call) => [call[1], call[3]]))
        .toEqual([['7', 'k1'], ['8', 'k2']]);
    });

  it('reuses the key only to retry the same request', async () => {
    const { links, resumes, linker } = linkerSetup();
    resumes.list.mockResolvedValue(ready(summary('resume-1', '7')));
    links.link
      .mockResolvedValueOnce(failed('unavailable'))
      .mockResolvedValueOnce(failed('rate-limited'))
      .mockResolvedValueOnce(ok);

    await linker.link('owner-1', 'resume-1', PROJECT);
    await linker.link('owner-1', 'resume-1', PROJECT);
    await linker.link('owner-1', 'resume-1', PROJECT);
    resumes.list.mockResolvedValue(ready(summary('resume-1', '8')));
    links.link.mockResolvedValueOnce(failed('already-linked'));
    await linker.link('owner-1', 'resume-1', PROJECT);

    expect(links.link.mock.calls.map((call) => call[3]))
      .toEqual(['k1', 'k1', 'k1', 'k2']);
  });

  it('resends the same request once with a rotated CSRF token', async () => {
    const { auth, csrfToken, links, resumes, linker } = linkerSetup();
    auth.refresh.mockImplementation(async () => {
      csrfToken.value = 'csrf-2';
    });
    resumes.list.mockResolvedValue(ready(summary('resume-1', '7')));
    links.link
      .mockResolvedValueOnce(failed('csrf-rejected'))
      .mockResolvedValueOnce(ok);

    expect(await linker.link('owner-1', 'resume-1', PROJECT)).toEqual(ok);

    expect(links.link.mock.calls.map((call) => [call[3], call[4]]))
      .toEqual([['k1', 'csrf-1'], ['k1', 'csrf-2']]);
  });

  it('sends nothing for an account other than the intent\'s', async () => {
    const { links, resumes, linker } = linkerSetup();
    resumes.list.mockResolvedValue(ready(summary('resume-1', '7')));

    expect(await linker.link('owner-2', 'resume-1', PROJECT))
      .toEqual(failed('session-lost'));
    expect(links.link).not.toHaveBeenCalled();
  });

  it('answers without a request when the resume is gone or the list fails',
    async () => {
      const { links, resumes, linker } = linkerSetup();
      resumes.list
        .mockResolvedValueOnce(ready(summary('resume-2', '7')))
        .mockResolvedValueOnce({ kind: 'failed', reason: 'network' })
        .mockResolvedValueOnce({ kind: 'rate-limited', retryAfterMs: null })
        .mockResolvedValueOnce({ kind: 'session-lost' });

      const results = [
        await linker.link('owner-1', 'resume-1', PROJECT),
        await linker.link('owner-1', 'resume-1', PROJECT),
        await linker.link('owner-1', 'resume-1', PROJECT),
        await linker.link('owner-1', 'resume-1', PROJECT),
      ];

      expect(results).toEqual([
        failed('resume-not-found'),
        failed('unavailable'),
        failed('rate-limited'),
        failed('session-lost'),
      ]);
      expect(links.link).not.toHaveBeenCalled();
    });
});

describe('resumes a new project could not join', () => {
  it('are handed to the editor once', () => {
    rememberUnlinked(PROJECT, ['Backend', 'Frontend']);

    expect(takeUnlinked(PROJECT)).toEqual(['Backend', 'Frontend']);
    expect(takeUnlinked(PROJECT)).toEqual([]);
  });
});
