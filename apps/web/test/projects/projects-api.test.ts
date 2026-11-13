import { describe, expect, it, vi } from 'vitest';

import {
  createProjectsApi,
  sendWithCsrf,
} from '../../app/composables/projectsApi';
import { failed, fakeAuth, ok, project } from './fixtures';

const BASE = 'http://localhost/api/v1';

// The test environment's Request drops headers given as a Headers object.
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

function problem(status: number, code: string, details?: unknown): Response {
  return respond(status, {
    error: { code, message: 'fixed', ...(details ? { details } : {}) },
  });
}

function recorder(...responses: Response[]) {
  const requests: Request[] = [];
  const fetch = vi.fn(async (request: Request) => {
    requests.push(request.clone());
    return responses.shift() ?? respond(500);
  });
  return {
    requests,
    api: createProjectsApi({
      baseUrl: BASE, fetch, Request: PlainHeadersRequest,
    }),
  };
}

const body = {
  slug: 'shop-site',
  defaultLanguage: 'en',
  text: { en: { name: 'Shop site' } },
};

describe('projects API', () => {
  it('replaces with If-Match, Idempotency-Key and the CSRF token', async () => {
    const saved = project({ revision: '4' });
    const { requests, api } = recorder(
      respond(200, { data: { project: saved, changedResumes: [] } }),
    );

    const result = await api.replace('project-1', '3', body, 'key-1', 'csrf');

    expect(result).toEqual(ok(saved));
    const [request] = requests;
    expect(request?.method).toBe('PUT');
    expect(new URL(request!.url).pathname).toBe('/api/v1/projects/project-1');
    expect(request?.headers.get('If-Match')).toBe('"r3"');
    expect(request?.headers.get('Idempotency-Key')).toBe('key-1');
    expect(request?.headers.get('X-CSRF-Token')).toBe('csrf');
    expect(await request?.json()).toEqual(body);
  });

  it('creates without If-Match and deletes without a body', async () => {
    const { requests, api } = recorder(
      respond(201, { data: project() }),
      respond(200, { data: { changedResumes: [] } }),
    );

    await api.create(
      { defaultLanguage: 'en', text: { en: { name: 'A' } } }, 'k1', 'c',
    );
    expect(await api.remove('project-1', '3', 'k2', 'c')).toEqual(ok(null));

    expect(requests[0]?.headers.get('If-Match')).toBeNull();
    expect(requests[0]?.headers.get('Idempotency-Key')).toBe('k1');
    expect(requests[1]?.method).toBe('DELETE');
    expect(requests[1]?.headers.get('If-Match')).toBe('"r3"');
    expect(await requests[1]?.text()).toBe('');
  });

  it('returns the winning project on a revision mismatch', async () => {
    const winner = project({ revision: '7' });
    const { api } = recorder(problem(412, 'revision_mismatch', {
      revision: '7', project: winner,
    }));

    expect(await api.replace('project-1', '3', body, 'k', 'c'))
      .toEqual({ kind: 'stale', project: winner });
  });

  it.each([
    [409, 'project_limit', 'limit'],
    [409, 'project_slug_taken', 'slug-taken'],
    [409, 'project_slug_held', 'slug-held'],
    [403, 'reauth_required', 'reauth-required'],
    [403, 'csrf_rejected', 'csrf-rejected'],
    [404, 'project_not_found', 'not-found'],
    [422, 'document_invalid', 'invalid'],
    [429, 'rate_limited', 'rate-limited'],
    [401, 'session_required', 'session-lost'],
    [503, 'public_state_busy', 'unavailable'],
  ] as const)('maps %i %s', async (status, code, failure) => {
    const { api } = recorder(problem(status, code));

    expect(await api.replace('project-1', '3', body, 'k', 'c'))
      .toEqual(failed(failure));
  });

  it('treats a network failure and a malformed body as unavailable',
    async () => {
      const broken = createProjectsApi({
        baseUrl: BASE,
        fetch: vi.fn(async () => {
          throw new TypeError('offline');
        }),
      });
      expect(await broken.list()).toEqual(failed('unavailable'));
      const { api } = recorder(respond(200, { data: { projects: [{}] } }));
      expect(await api.list()).toEqual(failed('unavailable'));
    });

  it('refreshes a rotated CSRF token and resends the same request once',
    async () => {
      const { auth, csrfToken } = fakeAuth();
      auth.refresh.mockImplementation(async () => {
        csrfToken.value = 'csrf-2';
      });
      const send = vi.fn()
        .mockResolvedValueOnce(failed('csrf-rejected'))
        .mockResolvedValueOnce(ok(null));

      expect(await sendWithCsrf(auth, 'owner-1', send)).toEqual(ok(null));
      expect(send.mock.calls).toEqual([['csrf-1'], ['csrf-2']]);
    });

  it('does not resend after a refresh that changed the account', async () => {
    const { auth, user } = fakeAuth();
    auth.refresh.mockImplementation(async () => {
      user.value = { ...user.value, id: 'owner-2' };
    });
    const send = vi.fn().mockResolvedValue(failed('csrf-rejected'));

    expect(await sendWithCsrf(auth, 'owner-1', send))
      .toEqual(failed('session-lost'));
    expect(send).toHaveBeenCalledTimes(1);
  });

  it('sends nothing for an account other than the intent\'s', async () => {
    const { auth } = fakeAuth();
    const send = vi.fn();

    expect(await sendWithCsrf(auth, 'owner-2', send))
      .toEqual(failed('session-lost'));
    expect(send).not.toHaveBeenCalled();
  });
});
