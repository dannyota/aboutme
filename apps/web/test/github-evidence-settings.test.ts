import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import GitHubEvidence from '../app/components/settings/GitHubEvidence.vue';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import {
  githubEvidenceSettingsCopy,
} from '../app/i18n/github-evidence-settings';
import { designCopy } from './support/evidenceCopy';
import { setSiteLocale } from './support/locale';
import { literalGuard, parityViolations } from './support/localizationSource';

const net = vi.hoisted(() => ({
  requests: [] as Request[],
  handle: null as ((request: Request) => Response | Promise<Response>) | null,
}));

vi.mock('../app/api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../app/api/client')>();
  class PlainHeadersRequest extends Request {
    constructor(input: RequestInfo | URL, init?: RequestInit) {
      const headers = Object.fromEntries(new Headers(init?.headers).entries());
      super(input, { ...init, headers });
    }
  }
  return {
    ...actual,
    createApiClient: (
      options: Parameters<typeof actual.createApiClient>[0] = {},
    ) => actual.createApiClient({
      ...options,
      baseUrl: 'http://localhost/api/v1',
      Request: PlainHeadersRequest,
      fetch: async (request: Request) => {
        net.requests.push(request.clone());
        if (net.handle === null) throw new TypeError('offline');
        return net.handle(request);
      },
    }),
  };
});

mockNuxtImport('navigateTo', () => vi.fn());

const authorizeUrl = 'https://github.com/login/oauth/authorize'
  + '?client_id=evidence&state=s&code_challenge=c&code_challenge_method=S256';
const connection = {
  githubUserId: 583231,
  login: 'octocat',
  connectedAt: '2026-11-15T08:30:00Z',
};
const expiresAt = '2026-11-15T09:00:00Z';
const notConnected = {
  generation: 0,
  hasEvidence: false,
  connection: null,
  session: null,
};
const connectedIdle = {
  generation: 3,
  hasEvidence: false,
  connection,
  session: null,
};
const connectedLive = {
  generation: 3,
  hasEvidence: false,
  connection,
  session: { expiresAt },
};
const disconnected = {
  generation: 4,
  hasEvidence: false,
  connection: null,
  session: null,
};
const disconnectRoute = 'DELETE /api/v1/github-evidence?keepLinks=true'
  + '&generation=3';
const disconnectedBody = {
  data: {
    generation: 4,
    projectRevisions: [],
    resumeRevisions: [
      { id: '0f6e7d4c-1b2a-4c3d-8e9f-0a1b2c3d4e5f', revision: '8' },
    ],
  },
};
const uuidPattern
  = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u;

let state: unknown = notConnected;
let csrfTokens: string[] = [];
let meRequests = 0;
const mounted: { unmount: () => void }[] = [];

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function noContent(): Response {
  return new Response(null, { status: 204 });
}

function problem(status: number, code: string): Response {
  return json(status, { error: { code, message: 'raw server detail' } });
}

function route(request: Request): string {
  const url = new URL(request.url);
  return `${request.method} ${url.pathname}${url.search}`;
}

function routes(): string[] {
  return net.requests.map(route);
}

function evidenceReads(): number {
  return routes().filter((entry) => entry === 'GET /api/v1/github-evidence')
    .length;
}

type Replies = Readonly<Record<string, () => Response>>;

function serve(replies: Replies = {}): void {
  net.handle = (request) => {
    const key = route(request);
    const reply = replies[key];
    if (reply !== undefined) return reply();
    if (key === 'GET /api/v1/github-evidence') {
      return json(200, { data: state });
    }
    return problem(500, 'unexpected');
  };
}

function registerCapability(githubEvidence: boolean): void {
  registerEndpoint('/api/v1/capabilities', () => ({
    data: {
      providerLogin: false,
      providers: [],
      agentAccess: false,
      passwordRegistration: true,
      passkeyEnrollment: false,
      totpEnrollment: false,
      signInToView: false,
      githubEvidence,
    },
  }));
}

registerEndpoint('/api/v1/me', () => {
  meRequests += 1;
  const csrfToken = csrfTokens.length > 1
    ? csrfTokens.shift()!
    : csrfTokens[0] ?? 'csrf-1';
  return {
    data: {
      user: {
        id: 'user-1',
        email: 'ada@example.com',
        name: 'Ada',
        avatarKey: null,
        hasPassword: true,
      },
      csrfToken,
      identities: [],
    },
  };
});
registerEndpoint('/api/v1/sessions', () => ({ data: [] }));
registerEndpoint('/api/v1/me/second-factor', () => ({
  data: { enabled: false, passkeys: [], recoveryCodesRemaining: 0 },
}));

async function mountSection(
  path = '/app/settings/sessions',
  attach = false,
) {
  const wrapper = await mountSuspended(GitHubEvidence, {
    route: path,
    ...(attach ? { attachTo: document.body } : {}),
  });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

async function click(
  wrapper: Awaited<ReturnType<typeof mountSection>>,
  testid: string,
): Promise<void> {
  await wrapper.get(`[data-testid="${testid}"]`).trigger('click');
  await flushPromises();
}

function buttonLabels(
  wrapper: Awaited<ReturnType<typeof mountSection>>,
): string[] {
  return wrapper.findAll('[data-slot="button"]').map((button) =>
    button.text());
}

function clockTime(locale: string, value: string): string {
  return new Intl.DateTimeFormat(locale, {
    hour: 'numeric',
    minute: '2-digit',
  }).format(new Date(value));
}

describe('GitHub evidence settings', () => {
  beforeEach(() => {
    setSiteLocale('en');
    clearNuxtData();
    registerCapability(true);
    state = notConnected;
    csrfTokens = ['csrf-1'];
    meRequests = 0;
    net.requests = [];
    serve();
    vi.mocked(navigateTo).mockClear();
  });

  afterEach(async () => {
    for (const wrapper of mounted.splice(0)) wrapper.unmount();
    await vi.waitFor(() => expect(useRouter().currentRoute.value.query)
      .not.toHaveProperty('evidence'));
  });

  it('shows the section only when the capability is on', async () => {
    registerCapability(false);
    const off = await mountSuspended(SessionsPage, {
      route: '/app/settings/sessions',
    });
    mounted.push(off);
    await flushPromises();
    expect(off.find('[data-testid="github-evidence"]').exists()).toBe(false);
    expect(evidenceReads()).toBe(0);
    off.unmount();
    mounted.pop();

    clearNuxtData();
    registerCapability(true);
    const on = await mountSuspended(SessionsPage, {
      route: '/app/settings/sessions',
    });
    mounted.push(on);
    await flushPromises();
    await vi.waitFor(() =>
      expect(on.find('[data-testid="github-evidence"]').exists()).toBe(true));
    expect(
      on.get('[aria-labelledby="github-evidence-title"]')
        .get('#github-evidence-title').text(),
    ).toBe('GitHub evidence');
  });

  it('offers Connect when no GitHub account is connected', async () => {
    const wrapper = await mountSection();

    expect(wrapper.get('h2').text()).toBe('GitHub evidence');
    expect(wrapper.text()).toContain('No GitHub account connected.');
    expect(wrapper.text()).toContain(
      'Connecting asks GitHub for no access beyond your public information. '
      + 'aboutme never stores your GitHub token.',
    );
    expect(buttonLabels(wrapper)).toEqual(['Connect GitHub']);
    expect(routes()).toEqual(['GET /api/v1/github-evidence']);
  });

  it(
    'shows the login and connected date with Continue when no session is live',
    async () => {
      state = connectedIdle;
      const wrapper = await mountSection();

      const login = wrapper.get('[data-testid="evidence-login"]');
      expect(login.text()).toBe('octocat');
      expect(login.element.tagName).toBe('BDI');
      expect(login.attributes('dir')).toBe('ltr');
      expect(wrapper.text()).toContain('Connected as octocat');
      expect(wrapper.text()).toContain('Connected on November 15, 2026');
      expect(wrapper.get('[datetime]').attributes('datetime'))
        .toBe(connection.connectedAt);
      expect(wrapper.get('[data-testid="evidence-session"]').text())
        .toBe('No GitHub session is open.');
      expect(buttonLabels(wrapper))
        .toEqual(['Continue with GitHub', 'Disconnect']);
      expect(wrapper.text()).not.toContain('583231');
    },
  );

  it('shows a live session with its expiry, Done and Disconnect', async () => {
    state = connectedLive;
    const wrapper = await mountSection();

    const session = wrapper.get('[data-testid="evidence-session"]');
    expect(session.text()).toBe(
      `GitHub session open until ${clockTime('en-US', expiresAt)}`,
    );
    expect(session.get('time').attributes('datetime')).toBe(expiresAt);
    expect(buttonLabels(wrapper)).toEqual(['Done', 'Disconnect']);
    expect(wrapper.text()).not.toContain('583231');
  });

  it('titles the section at 20 px, weight 700, and draws Disconnect as a '
    + 'destructive outline pill', async () => {
    state = connectedIdle;
    const wrapper = await mountSection();

    const title = wrapper.get('#github-evidence-title');
    expect(title.classes()).toEqual(
      expect.arrayContaining(['text-lg', 'font-bold']),
    );
    expect(title.classes()).not.toContain('font-semibold');
    expect(wrapper.get('[data-testid="evidence-disconnect"]')
      .attributes('data-variant')).toBe('destructive');
  });

  it(
    'Connect posts start with the CSRF token and opens the GitHub URL',
    async () => {
      serve({
        'POST /api/v1/github-evidence/start': () =>
          json(200, { data: { authorizeUrl } }),
      });
      const wrapper = await mountSection();

      await click(wrapper, 'evidence-connect');

      const start = net.requests.find((request) =>
        route(request) === 'POST /api/v1/github-evidence/start');
      expect(start?.headers.get('X-CSRF-Token')).toBe('csrf-1');
      expect(navigateTo).toHaveBeenCalledTimes(1);
      expect(navigateTo).toHaveBeenCalledWith(authorizeUrl, { external: true });
    },
  );

  it('Continue posts start and opens the validated GitHub URL', async () => {
    state = connectedIdle;
    serve({
      'POST /api/v1/github-evidence/start': () =>
        json(200, { data: { authorizeUrl } }),
    });
    const wrapper = await mountSection();

    await click(wrapper, 'evidence-continue');

    expect(routes()).toEqual([
      'GET /api/v1/github-evidence',
      'POST /api/v1/github-evidence/start',
    ]);
    expect(navigateTo).toHaveBeenCalledWith(authorizeUrl, { external: true });
  });

  it.each([
    ['another host', 'https://github.com.example.net/login/oauth/authorize'],
    ['plain http', 'http://github.com/login/oauth/authorize?state=s'],
    ['another path', 'https://github.com/login/oauth/authorized?state=s'],
    ['credentials', 'https://user@github.com/login/oauth/authorize?state=s'],
    ['a fragment', 'https://github.com/login/oauth/authorize?state=s#x'],
    ['a script URL', 'javascript:alert(1)'],
    ['a missing URL', undefined],
  ])('never navigates to %s', async (_name, value) => {
    serve({
      'POST /api/v1/github-evidence/start': () =>
        json(200, { data: { authorizeUrl: value } }),
    });
    const wrapper = await mountSection();

    await click(wrapper, 'evidence-connect');

    expect(navigateTo).not.toHaveBeenCalled();
    expect(wrapper.get('[data-testid="evidence-error"]').text())
      .toBe('Something went wrong. Please try again.');
  });

  it('Done posts session/end and reads the state again', async () => {
    state = connectedLive;
    serve({
      'POST /api/v1/github-evidence/session/end': () => {
        state = connectedIdle;
        return noContent();
      },
    });
    const wrapper = await mountSection();

    await click(wrapper, 'evidence-done');

    expect(routes()).toEqual([
      'GET /api/v1/github-evidence',
      'POST /api/v1/github-evidence/session/end',
      'GET /api/v1/github-evidence',
    ]);
    expect(net.requests[1]?.headers.get('X-CSRF-Token')).toBe('csrf-1');
    expect(buttonLabels(wrapper))
      .toEqual(['Continue with GitHub', 'Disconnect']);
  });

  it(
    'Disconnect sends keepLinks=true, the generation it read and a fresh '
    + 'Idempotency-Key, accepts the 200 and reads the state again',
    async () => {
      state = connectedLive;
      serve({
        [disconnectRoute]: () => {
          state = disconnected;
          return json(200, disconnectedBody);
        },
      });
      const wrapper = await mountSection();

      await click(wrapper, 'evidence-disconnect');

      expect(routes()).toEqual([
        'GET /api/v1/github-evidence',
        disconnectRoute,
        'GET /api/v1/github-evidence',
      ]);
      expect(net.requests[1]?.headers.get('X-CSRF-Token')).toBe('csrf-1');
      expect(net.requests[1]?.headers.get('Idempotency-Key'))
        .toMatch(uuidPattern);
      expect(wrapper.find('[data-testid="evidence-error"]').exists())
        .toBe(false);
      expect(buttonLabels(wrapper)).toEqual(['Connect GitHub']);
    },
  );

  it('sends the generation of the last status read', async () => {
    state = { ...connectedIdle, generation: 12 };
    serve({
      'DELETE /api/v1/github-evidence?keepLinks=true&generation=12': () => {
        state = { ...disconnected, generation: 13 };
        return json(200, {
          data: { ...disconnectedBody.data, generation: 13 },
        });
      },
    });
    const wrapper = await mountSection();

    await click(wrapper, 'evidence-disconnect');

    expect(routes()).toEqual([
      'GET /api/v1/github-evidence',
      'DELETE /api/v1/github-evidence?keepLinks=true&generation=12',
      'GET /api/v1/github-evidence',
    ]);
    expect(buttonLabels(wrapper)).toEqual(['Connect GitHub']);
  });

  it(
    'keeps one Idempotency-Key for the same choice and generation until a '
    + 'definitive answer',
    async () => {
      state = connectedIdle;
      csrfTokens = ['csrf-1', 'csrf-2'];
      let attempts = 0;
      serve({
        [disconnectRoute]: () => {
          attempts += 1;
          if (attempts === 1) return problem(403, 'csrf_rejected');
          if (attempts === 2) return problem(500, 'internal');
          if (attempts === 3) throw new TypeError('lost');
          state = disconnected;
          return json(200, disconnectedBody);
        },
      });
      const wrapper = await mountSection();

      await click(wrapper, 'evidence-disconnect');
      await vi.waitFor(() =>
        expect(wrapper.find('[data-testid="evidence-error"]').exists())
          .toBe(true));
      await click(wrapper, 'evidence-disconnect');
      await click(wrapper, 'evidence-disconnect');
      await vi.waitFor(() => expect(attempts).toBe(4));

      const sent = net.requests.filter((request) =>
        route(request) === disconnectRoute);
      expect(sent.map((request) => request.headers.get('X-CSRF-Token')))
        .toEqual(['csrf-1', 'csrf-2', 'csrf-2', 'csrf-2']);
      const keys = sent.map((request) =>
        request.headers.get('Idempotency-Key'));
      expect(keys[0]).toMatch(uuidPattern);
      expect(new Set(keys)).toEqual(new Set([keys[0]]));
      expect(buttonLabels(wrapper)).toEqual(['Connect GitHub']);
    },
  );

  it('asks to keep or remove the items when the account has evidence, '
    + 'focusing Keep links', async () => {
    state = { ...connectedIdle, hasEvidence: true };
    const wrapper = await mountSection('/app/settings/sessions', true);

    await click(wrapper, 'evidence-disconnect');
    await vi.waitFor(() => expect(document.activeElement?.textContent?.trim())
      .toBe('Keep links'));

    const dialog = document.body.querySelector('[role="alertdialog"]')!;
    expect(dialog.querySelector('h2')?.textContent?.trim())
      .toBe('Keep the attached items as plain links?');
    expect([...dialog.querySelectorAll('button')].map((button) =>
      button.textContent?.trim()))
      .toEqual(['Cancel', 'Remove from every project', 'Keep links']);
    expect(dialog.querySelector('[data-action="evidence-keep-links"]')
      ?.getAttribute('data-variant')).toBe('default');
    expect(dialog.querySelector('[data-action="evidence-remove-links"]')
      ?.getAttribute('data-variant')).toBe('destructive');
    expect(routes()).toEqual(['GET /api/v1/github-evidence']);
  });

  it.each([
    ['evidence-keep-links', 'true'],
    ['evidence-remove-links', 'false'],
  ])('%s sends keepLinks=%s with the generation it read', async (
    action, keepLinks,
  ) => {
    state = { ...connectedIdle, hasEvidence: true, generation: 7 };
    const target = 'DELETE /api/v1/github-evidence'
      + `?keepLinks=${keepLinks}&generation=7`;
    serve({
      [target]: () => {
        state = { ...disconnected, generation: 8 };
        return json(200, disconnectedBody);
      },
    });
    const wrapper = await mountSection('/app/settings/sessions', true);

    await click(wrapper, 'evidence-disconnect');
    document.body.querySelector<HTMLElement>(`[data-action="${action}"]`)!
      .click();
    await flushPromises();

    expect(routes()).toEqual([
      'GET /api/v1/github-evidence',
      target,
      'GET /api/v1/github-evidence',
    ]);
    expect(net.requests[1]?.headers.get('Idempotency-Key'))
      .toMatch(uuidPattern);
    expect(document.body.querySelector('[role="alertdialog"]')).toBeNull();
    expect(buttonLabels(wrapper)).toEqual(['Connect GitHub']);
  });

  it.each([
    ['Cancel', () => {
      document.body.querySelector<HTMLElement>(
        '[data-action="evidence-disconnect-cancel"]',
      )!.click();
    }],
    ['Escape', () => {
      document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', {
        key: 'Escape', bubbles: true,
      }));
    }],
  ])('%s sends nothing and returns focus to Disconnect', async (
    _name, dismiss,
  ) => {
    state = { ...connectedIdle, hasEvidence: true };
    const wrapper = await mountSection('/app/settings/sessions', true);
    const disconnect = wrapper.get('[data-testid="evidence-disconnect"]');
    (disconnect.element as HTMLElement).focus();

    await click(wrapper, 'evidence-disconnect');
    await vi.waitFor(() => expect(document.activeElement?.textContent?.trim())
      .toBe('Keep links'));
    dismiss();
    await flushPromises();

    await vi.waitFor(() => expect(document.body
      .querySelector('[role="alertdialog"]')).toBeNull());
    await vi.waitFor(() => expect(document.activeElement)
      .toBe(disconnect.element));
    expect(routes()).toEqual(['GET /api/v1/github-evidence']);
  });

  it('focuses Connect after Keep links disconnects', async () => {
    state = { ...connectedIdle, hasEvidence: true };
    serve({
      'DELETE /api/v1/github-evidence?keepLinks=true&generation=3': () => {
        state = disconnected;
        return json(200, disconnectedBody);
      },
    });
    const wrapper = await mountSection('/app/settings/sessions', true);

    await click(wrapper, 'evidence-disconnect');
    document.body.querySelector<HTMLElement>(
      '[data-action="evidence-keep-links"]',
    )!.click();
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));
    await flushPromises();

    await vi.waitFor(() => expect(document.activeElement)
      .toBe(wrapper.get('[data-testid="evidence-connect"]').element));
  });

  it('focuses the error after Remove from every project fails', async () => {
    state = { ...connectedIdle, hasEvidence: true };
    serve({
      'DELETE /api/v1/github-evidence?keepLinks=false&generation=3': () =>
        problem(500, 'internal'),
    });
    const wrapper = await mountSection('/app/settings/sessions', true);

    await click(wrapper, 'evidence-disconnect');
    document.body.querySelector<HTMLElement>(
      '[data-action="evidence-remove-links"]',
    )!.click();
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 0));
    await flushPromises();

    await vi.waitFor(() => expect(document.activeElement)
      .toBe(wrapper.get('[data-testid="evidence-error"]').element));
    expect(wrapper.get('[data-testid="evidence-error"]').text())
      .toBe('Something went wrong. Please try again.');
  });

  it('says to try again when the published resumes are busy', async () => {
    state = connectedIdle;
    serve({
      [disconnectRoute]: () => problem(503, 'public_state_busy'),
    });
    const wrapper = await mountSection();

    await click(wrapper, 'evidence-disconnect');

    expect(wrapper.get('[data-testid="evidence-error"]').text())
      .toBe('That could not finish right now. Try again.');
  });

  it('uses a new key for a different choice', async () => {
    state = { ...connectedIdle, hasEvidence: true };
    const keep = 'DELETE /api/v1/github-evidence?keepLinks=true&generation=3';
    const remove = 'DELETE /api/v1/github-evidence?keepLinks=false'
      + '&generation=3';
    serve({
      [keep]: () => problem(500, 'internal'),
      [remove]: () => problem(500, 'internal'),
    });
    const wrapper = await mountSection('/app/settings/sessions', true);

    for (const action of [
      'evidence-keep-links', 'evidence-keep-links', 'evidence-remove-links',
    ]) {
      await click(wrapper, 'evidence-disconnect');
      document.body.querySelector<HTMLElement>(`[data-action="${action}"]`)!
        .click();
      await flushPromises();
    }

    const sent = net.requests.filter((request) =>
      route(request).startsWith('DELETE '));
    expect(sent.map(route)).toEqual([keep, keep, remove]);
    const keys = sent.map((request) =>
      request.headers.get('Idempotency-Key'));
    expect(keys[1]).toBe(keys[0]);
    expect(keys[2]).not.toBe(keys[0]);
    expect(keys[2]).toMatch(uuidPattern);
  });

  it(
    'reads the state again when the connection changed and asks again with '
    + 'the new generation',
    async () => {
      state = { ...connectedIdle, hasEvidence: true };
      serve({
        'DELETE /api/v1/github-evidence?keepLinks=true&generation=3': () => {
          state = { ...connectedIdle, hasEvidence: true, generation: 5 };
          return problem(409, 'evidence_connection_changed');
        },
      });
      const wrapper = await mountSection('/app/settings/sessions', true);

      await click(wrapper, 'evidence-disconnect');
      document.body.querySelector<HTMLElement>(
        '[data-action="evidence-keep-links"]',
      )!.click();
      await flushPromises();

      expect(routes()).toEqual([
        'GET /api/v1/github-evidence',
        'DELETE /api/v1/github-evidence?keepLinks=true&generation=3',
        'GET /api/v1/github-evidence',
      ]);
      expect(wrapper.get('[data-testid="evidence-error"]').text()).toBe(
        'The GitHub connection just changed. The status has been reloaded; '
        + 'try again.',
      );
      expect(wrapper.text()).not.toContain('raw server detail');

      serve({
        'DELETE /api/v1/github-evidence?keepLinks=false&generation=5': () => {
          state = disconnected;
          return json(200, disconnectedBody);
        },
      });
      await click(wrapper, 'evidence-disconnect');
      expect(document.body.querySelector('[role="alertdialog"]'))
        .not.toBeNull();
      document.body.querySelector<HTMLElement>(
        '[data-action="evidence-remove-links"]',
      )!.click();
      await flushPromises();

      expect(routes()).toContain(
        'DELETE /api/v1/github-evidence?keepLinks=false&generation=5',
      );
      expect(buttonLabels(wrapper)).toEqual(['Connect GitHub']);
    },
  );

  it.each([
    ['no generation', { connection: null, session: null }],
    ['a negative generation', { ...notConnected, generation: -1 }],
    ['a fractional generation', { ...notConnected, generation: 1.5 }],
    ['a string generation', { ...notConnected, generation: '3' }],
    ['no hasEvidence', { generation: 0, connection: null, session: null }],
    ['a string hasEvidence', { ...notConnected, hasEvidence: 'false' }],
  ])('treats a status with %s as a failed load', async (_name, body) => {
    state = body;
    const wrapper = await mountSection();

    expect(wrapper.get('[data-testid="evidence-load-error"]').text())
      .toBe('GitHub evidence could not load. Try again.');
  });

  it(
    'shows the connected notice and removes the parameter in place',
    async () => {
      state = connectedLive;
      const router = useRouter();
      const replace = vi.spyOn(router, 'replace');
      const push = vi.spyOn(router, 'push');
      try {
        const wrapper = await mountSection(
          '/app/settings/sessions?evidence=connected',
        );

        expect(wrapper.get('[data-testid="evidence-connected"]').text())
          .toBe('GitHub is connected.');
        expect(wrapper.find('[data-testid="evidence-failed"]').exists())
          .toBe(false);
        expect(replace).toHaveBeenLastCalledWith({
          path: '/app/settings/sessions',
          query: {},
          hash: '',
        });
        await vi.waitFor(() => expect(router.currentRoute.value.fullPath)
          .toBe('/app/settings/sessions'));
        expect(push).not.toHaveBeenCalled();
      } finally {
        replace.mockRestore();
        push.mockRestore();
      }
    },
  );

  it('shows the failure notice with no reason', async () => {
    const wrapper = await mountSection(
      '/app/settings/sessions?evidence=failed&reason=denied',
    );

    const notice = wrapper.get('[data-testid="evidence-failed"]');
    expect(notice.text()).toBe('GitHub was not connected. Try again.');
    expect(notice.attributes('role')).toBe('alert');
    expect(wrapper.text()).not.toContain('denied');
    expect(wrapper.find('[data-testid="evidence-connected"]').exists())
      .toBe(false);
    await vi.waitFor(() => expect(useRouter().currentRoute.value.query)
      .toEqual({ reason: 'denied' }));
  });

  it('shows the different account alert and clears it', async () => {
    state = connectedIdle;
    const router = useRouter();
    const push = vi.spyOn(router, 'push');
    try {
      const wrapper = await mountSection(
        '/app/settings/sessions?evidence=different_account&error=cancelled',
      );

      const notice = wrapper.get('[data-testid="evidence-different-account"]');
      expect(notice.text()).toBe(
        'This GitHub account differs from the connected one. Disconnect first.',
      );
      expect(notice.attributes('role')).toBe('alert');
      expect(wrapper.find('[data-testid="evidence-connected"]').exists())
        .toBe(false);
      expect(wrapper.find('[data-testid="evidence-failed"]').exists())
        .toBe(false);
      await vi.waitFor(() => expect(router.currentRoute.value.query)
        .toEqual({ error: 'cancelled' }));
      expect(push).not.toHaveBeenCalled();
    } finally {
      push.mockRestore();
    }
  });

  it('shows the different account notice in Vietnamese', async () => {
    setSiteLocale('vi');
    state = connectedIdle;
    const wrapper = await mountSection(
      '/app/settings/sessions?evidence=different_account',
    );

    expect(wrapper.get('[data-testid="evidence-different-account"]').text())
      .toBe('Tài khoản GitHub này khác tài khoản đã kết nối. '
        + 'Hãy ngắt kết nối trước.');
    await vi.waitFor(() => expect(useRouter().currentRoute.value.fullPath)
      .toBe('/app/settings/sessions'));
  });

  it.each([
    ['another value', '?evidence=Connected'],
    ['an empty value', '?evidence='],
    ['a repeated value', '?evidence=connected&evidence=connected'],
    ['another case', '?evidence=Different_Account'],
  ])('ignores %s and removes the parameter', async (_name, search) => {
    const wrapper = await mountSection(`/app/settings/sessions${search}`);

    expect(wrapper.find('[data-testid="evidence-connected"]').exists())
      .toBe(false);
    expect(wrapper.find('[data-testid="evidence-failed"]').exists())
      .toBe(false);
    expect(wrapper.find('[data-testid="evidence-different-account"]').exists())
      .toBe(false);
    await vi.waitFor(() => expect(useRouter().currentRoute.value.fullPath)
      .toBe('/app/settings/sessions'));
  });

  it('keeps other query parameters when it removes evidence', async () => {
    await mountSection(
      '/app/settings/sessions?error=cancelled&evidence=failed',
    );

    await vi.waitFor(() => expect(useRouter().currentRoute.value.query)
      .toEqual({ error: 'cancelled' }));
  });

  it.each([
    ['evidence_unavailable at 404', () => problem(404, 'evidence_unavailable'),
      'GitHub evidence is not available right now. Try again later.'],
    ['evidence_unavailable at 503', () => problem(503, 'evidence_unavailable'),
      'GitHub evidence is not available right now. Try again later.'],
    ['evidence_upstream', () => problem(502, 'evidence_upstream'),
      'GitHub is not available right now. Try again later.'],
    ['an evidence_upstream limit', () => problem(429, 'evidence_upstream'),
      'GitHub is not available right now. Try again later.'],
    ['rate_limited', () => problem(429, 'rate_limited'),
      'Too many attempts. Try again later.'],
    ['csrf_rejected twice', () => problem(403, 'csrf_rejected'),
      'We could not confirm this request. Refresh the page and try again.'],
    ['an unknown error', () => problem(500, 'internal'),
      'Something went wrong. Please try again.'],
    ['a network failure', () => {
      throw new TypeError('raw server detail');
    }, 'Something went wrong. Please try again.'],
  ])('maps %s on start to fixed copy', async (_name, reply, message) => {
    serve({ 'POST /api/v1/github-evidence/start': reply });
    const wrapper = await mountSection();

    await click(wrapper, 'evidence-connect');
    await vi.waitFor(() =>
      expect(wrapper.find('[data-testid="evidence-error"]').exists())
        .toBe(true));

    expect(wrapper.get('[data-testid="evidence-error"]').text()).toBe(message);
    expect(wrapper.text()).not.toContain('raw server detail');
    expect(navigateTo).not.toHaveBeenCalled();
  });

  it('retries a CSRF rejection once with the refreshed token', async () => {
    csrfTokens = ['csrf-1', 'csrf-2'];
    let starts = 0;
    serve({
      'POST /api/v1/github-evidence/start': () => {
        starts += 1;
        return starts === 1
          ? problem(403, 'csrf_rejected')
          : json(200, { data: { authorizeUrl } });
      },
    });
    const wrapper = await mountSection();

    await click(wrapper, 'evidence-connect');
    await vi.waitFor(() => expect(navigateTo).toHaveBeenCalled());

    const tokens = net.requests
      .filter((request) =>
        route(request) === 'POST /api/v1/github-evidence/start')
      .map((request) => request.headers.get('X-CSRF-Token'));
    expect(tokens).toEqual(['csrf-1', 'csrf-2']);
    expect(meRequests).toBeGreaterThanOrEqual(2);
    expect(navigateTo).toHaveBeenCalledWith(authorizeUrl, { external: true });
  });

  it(
    'says the session had ended when Done finds none, then reads the state',
    async () => {
      state = connectedLive;
      serve({
        'POST /api/v1/github-evidence/session/end': () => {
          state = connectedIdle;
          return problem(409, 'evidence_session_required');
        },
      });
      const wrapper = await mountSection();

      await click(wrapper, 'evidence-done');

      expect(wrapper.get('[data-testid="evidence-error"]').text())
        .toBe('The GitHub session had already ended.');
      expect(evidenceReads()).toBe(2);
      expect(buttonLabels(wrapper))
        .toEqual(['Continue with GitHub', 'Disconnect']);
    },
  );

  it('maps a Disconnect failure to fixed copy', async () => {
    state = connectedIdle;
    serve({
      [disconnectRoute]: () => problem(404, 'evidence_unavailable'),
    });
    const wrapper = await mountSection();

    await click(wrapper, 'evidence-disconnect');

    expect(wrapper.get('[data-testid="evidence-error"]').text())
      .toBe('GitHub evidence is not available right now. Try again later.');
    expect(wrapper.text()).not.toContain('raw server detail');
  });

  it('shows fixed copy and Retry when the state cannot load', async () => {
    serve({
      'GET /api/v1/github-evidence': () => problem(500, 'internal'),
    });
    const wrapper = await mountSection();

    expect(wrapper.get('[data-testid="evidence-load-error"]').text())
      .toBe('GitHub evidence could not load. Try again.');
    expect(wrapper.text()).not.toContain('raw server detail');
    expect(wrapper.find('[data-testid="evidence-connect"]').exists())
      .toBe(false);

    serve();
    await click(wrapper, 'evidence-retry');

    expect(wrapper.find('[data-testid="evidence-load-error"]').exists())
      .toBe(false);
    expect(buttonLabels(wrapper)).toEqual(['Connect GitHub']);
  });

  it('sends a lost session to sign-in', async () => {
    serve({
      'GET /api/v1/github-evidence': () => problem(401, 'session_required'),
    });
    await mountSection();

    expect(navigateTo).toHaveBeenCalledWith('/login');
  });

  it('speaks Vietnamese and switches language without a new read', async () => {
    setSiteLocale('vi');
    state = connectedLive;
    const wrapper = await mountSection(
      '/app/settings/sessions?evidence=connected',
    );

    expect(wrapper.get('h2').text()).toBe('Bằng chứng GitHub');
    expect(wrapper.text()).toContain('Kết nối ngày 15 tháng 11, 2026');
    expect(wrapper.get('[data-testid="evidence-session"]').text()).toBe(
      `Phiên GitHub mở đến ${clockTime('vi-VN', expiresAt)}`,
    );
    expect(wrapper.get('[data-testid="evidence-connected"]').text())
      .toBe('Đã kết nối GitHub.');
    expect(buttonLabels(wrapper)).toEqual(['Xong', 'Ngắt kết nối']);

    const reads = evidenceReads();
    useState<'vi' | 'en'>('aboutme-locale').value = 'en';
    await wrapper.vm.$nextTick();

    expect(wrapper.get('h2').text()).toBe('GitHub evidence');
    expect(wrapper.get('[data-testid="evidence-connected"]').text())
      .toBe('GitHub is connected.');
    expect(buttonLabels(wrapper)).toEqual(['Done', 'Disconnect']);
    expect(evidenceReads()).toBe(reads);
  });

  it('has every key in both languages', () => {
    const keys = [
      'connect', 'connectedAs', 'connectedNotice', 'connectedOn', 'continue',
      'differentAccountNotice', 'disconnect', 'disconnectAsk',
      'disconnectKeep', 'disconnectRemove', 'done', 'errors', 'failedNotice',
      'loadFailed', 'loading', 'noSession', 'notConnected', 'privacy', 'retry',
      'sessionOpenUntil', 'title',
    ];
    const errors = [
      'connectionChanged', 'csrfRejected', 'failed', 'rateLimited',
      'sessionEnded', 'unavailable', 'upstream',
    ];
    expect(Object.keys(githubEvidenceSettingsCopy).sort())
      .toEqual(['en', 'vi']);
    for (const locale of ['vi', 'en'] as const) {
      const copy = githubEvidenceSettingsCopy[locale];
      expect(Object.keys(copy).sort()).toEqual(keys);
      expect(Object.keys(copy.errors).sort()).toEqual(errors);
    }
    expect(parityViolations(
      githubEvidenceSettingsCopy.vi,
      githubEvidenceSettingsCopy.en,
      'github-evidence-settings',
    )).toEqual([]);
  });

  it('uses the design copy for the disconnect ask and a changed connection',
    () => {
      const rows = designCopy();
      type Copy = typeof githubEvidenceSettingsCopy.en;
      const pairs: [string, (copy: Copy) => string][] = [
        ['Different account', (copy) => copy.differentAccountNotice],
        ['Disconnect ask', (copy) => copy.disconnectAsk],
        ['Disconnect keep', (copy) => copy.disconnectKeep],
        ['Disconnect remove', (copy) => copy.disconnectRemove],
        ['Connection changed', (copy) => copy.errors.connectionChanged],
      ];
      for (const [key, pick] of pairs) {
        expect([
          pick(githubEvidenceSettingsCopy.vi),
          pick(githubEvidenceSettingsCopy.en),
        ], key).toEqual(rows.get(key));
      }
    });

  it('keeps display text in the catalog', () => {
    const { sourceViolations } = literalGuard({});
    expect(sourceViolations('components/settings/GitHubEvidence.vue'))
      .toEqual([]);
    expect(sourceViolations('components/settings/EvidenceDisconnectDialog.vue'))
      .toEqual([]);
  });
});
