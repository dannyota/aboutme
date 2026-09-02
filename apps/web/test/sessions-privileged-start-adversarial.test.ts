import { describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { createError, readRawBody, setResponseStatus, type H3Event } from 'h3';
import SessionsPage from '../app/pages/app/settings/sessions.vue';
import { registerCapabilities } from './support/capabilities';

registerCapabilities();

mockNuxtImport('navigateTo', () => vi.fn());

type Provider = 'google' | 'github' | 'linkedin';
type Purpose = 'link' | 'reauth';

interface MockEvent {
  node?: { req?: { headers?: Record<string, string>; url?: string } };
}

interface CapturedStart {
  provider: Provider;
  method: string;
  url: string;
  csrfToken: string | undefined;
  contentType: string | undefined;
  body: string | undefined;
}

const providers: Provider[] = ['google', 'github', 'linkedin'];
const authorizeURLs: Record<Provider, string> = {
  google: 'https://accounts.google.com/o/oauth2/v2/auth?state=google-state',
  github: 'https://github.com/login/oauth/authorize?state=github-state',
  linkedin:
    'https://www.linkedin.com/oauth/v2/authorization?state=linkedin-state',
};

let identityProviders: Provider[] = [];
let csrfToken = 'csrf-initial';
let meCalls = 0;
let capturedStarts: CapturedStart[] = [];
let respondToStart: (
  provider: Provider,
  event: H3Event,
) => unknown | Promise<unknown> = (provider) => ({
  data: { authorizeUrl: authorizeURLs[provider] },
});

function requestHeader(event: MockEvent, name: string): string | undefined {
  const headers = event.node?.req?.headers ?? {};
  const key = Object.keys(headers).find(
    (candidate) => candidate.toLowerCase() === name.toLowerCase(),
  );
  return key ? headers[key] : undefined;
}

registerEndpoint('/api/v1/me', {
  method: 'GET',
  handler: () => {
    meCalls += 1;
    return {
      data: {
        user: {
          id: 'user-1',
          email: 'security-test@example.com',
          name: 'Security Test',
          avatarKey: null,
        },
        csrfToken,
        identities: identityProviders.map((provider) => ({ provider })),
      },
    };
  },
});

registerEndpoint('/api/v1/sessions', () => ({ data: [] }));

async function handleStart(
  provider: Provider,
  event: H3Event,
): Promise<unknown> {
  const harnessURL = event.node.req.url ?? '';
  expect(harnessURL).toMatch(/^\/_\//);
  capturedStarts.push({
    provider,
    method: event.method,
    url: harnessURL.slice(2),
    csrfToken: requestHeader(event, 'x-csrf-token'),
    contentType: requestHeader(event, 'content-type'),
    body: await readRawBody(event),
  });
  return await respondToStart(provider, event);
}

for (const provider of providers) {
  registerEndpoint(`/api/v1/auth/${provider}/start`, {
    handler: (event) => handleStart(provider, event),
  });
}

function privilegedAnchors(
  wrapper: Awaited<ReturnType<typeof mountSuspended>>,
) {
  const privilegedStart = new RegExp(
    '^/api/v1/auth/(google|github|linkedin)/start'
    + '\\?purpose=(link|reauth)$',
  );
  return wrapper
    .findAll('[href]')
    .filter((anchor) => privilegedStart.test(anchor.attributes('href') ?? ''));
}

function actionButton(
  wrapper: Awaited<ReturnType<typeof mountSuspended>>,
  label: string,
) {
  const button = wrapper
    .findAll('[data-slot="button"]')
    .find((candidate) => candidate.text().trim() === label);
  expect(
    button,
    `missing action button labelled ${JSON.stringify(label)}`,
  ).toBeDefined();
  return button!;
}

async function settleClick(
  wrapper: Awaited<ReturnType<typeof mountSuspended>>,
  label: string,
): Promise<void> {
  await actionButton(wrapper, label).trigger('click');
  await flushPromises();
  await flushPromises();
}

function expectBodilessCSRFPost(
  request: CapturedStart,
  provider: Provider,
  purpose: Purpose,
  expectedToken: string,
): void {
  expect(request.provider).toBe(provider);
  expect(request.method).toBe('POST');
  expect(request.url).toBe(`/api/v1/auth/${provider}/start?purpose=${purpose}`);
  expect(request.csrfToken).toBe(expectedToken);
  expect(request.contentType).toBeUndefined();
  expect(request.body ?? '').toBe('');
}

describe('settings privileged OAuth starts (adversarial)', () => {
  it(
    'uses provider-bound bodiless CSRF POSTs, retries CSRF once, and '
    + 'navigates only after a valid authorize URL',
    async () => {
      vi.mocked(navigateTo).mockReset();
      vi.mocked(navigateTo).mockResolvedValue(undefined);
      capturedStarts = [];
      identityProviders = [];
      csrfToken = 'csrf-link';
      meCalls = 0;
      respondToStart = (provider) => ({
        data: { authorizeUrl: authorizeURLs[provider] },
      });

      const linkPage = await mountSuspended(SessionsPage);
      await flushPromises();
      await linkPage
        .get('[data-testid="add-provider-button"]')
        .trigger('click');
      await flushPromises();

      expect(
        privilegedAnchors(linkPage),
        'privileged OAuth starts must never be GET anchors',
      ).toHaveLength(0);

      for (const provider of providers) {
        const before = capturedStarts.length;
        await settleClick(linkPage, `Link ${provider}`);

        expect(capturedStarts).toHaveLength(before + 1);
        expectBodilessCSRFPost(
          capturedStarts[before]!,
          provider,
          'link',
          'csrf-link',
        );
        expect(vi.mocked(navigateTo)).toHaveBeenLastCalledWith(
          authorizeURLs[provider],
          { external: true },
        );
      }

      // A rotated session invalidates the cached synchronizer token. One
      // rejection refreshes /me and retries the exact same bodiless request.
      csrfToken = 'csrf-stale';
      await refreshNuxtData();
      const meCallsBeforeRetry = meCalls;
      let attempts = 0;
      respondToStart = (_provider, event) => {
        attempts += 1;
        if (attempts === 1) {
          csrfToken = 'csrf-rotated';
          setResponseStatus(event, 403);
          return { error: { code: 'csrf_rejected', message: 'rejected' } };
        }
        return { data: { authorizeUrl: authorizeURLs.google } };
      };
      const retryStart = capturedStarts.length;
      await settleClick(linkPage, 'Link google');

      await vi.waitFor(() => expect(attempts).toBe(2));
      expect(meCalls).toBe(meCallsBeforeRetry + 1);
      expectBodilessCSRFPost(
        capturedStarts[retryStart]!,
        'google',
        'link',
        'csrf-stale',
      );
      expectBodilessCSRFPost(
        capturedStarts[retryStart + 1]!,
        'google',
        'link',
        'csrf-rotated',
      );

      // A second rejection stops. It must not loop or fall back to GET.
      csrfToken = 'csrf-stale-again';
      await refreshNuxtData();
      attempts = 0;
      respondToStart = (_provider, event) => {
        attempts += 1;
        if (attempts === 1) csrfToken = 'csrf-rotated-again';
        setResponseStatus(event, 403);
        return { error: { code: 'csrf_rejected', message: 'rejected' } };
      };
      const navigationCount = vi.mocked(navigateTo).mock.calls.length;
      await settleClick(linkPage, 'Link google');

      await vi.waitFor(() => expect(attempts).toBe(2));
      expect(vi.mocked(navigateTo)).toHaveBeenCalledTimes(navigationCount);
      expect(linkPage.get('[data-testid="link-error"]').text()).not.toBe('');

      const invalidResponses: Array<[string, unknown]> = [
        ['missing envelope', {}],
        ['malformed URL', { data: { authorizeUrl: 'not a URL' } }],
        ['unsafe scheme', { data: { authorizeUrl: 'javascript:alert(1)' } }],
        [
          'credentials',
          {
            data: {
              authorizeUrl:
                'https://user:pass@accounts.google.com/o/oauth2/v2/auth',
            },
          },
        ],
        [
          'fragment',
          {
            data: {
              authorizeUrl:
                'https://accounts.google.com/o/oauth2/v2/auth#secret',
            },
          },
        ],
        ['wrong provider', { data: { authorizeUrl: authorizeURLs.github } }],
        [
          'wrong provider path',
          {
            data: {
              authorizeUrl: 'https://accounts.google.com/not-oauth',
            },
          },
        ],
      ];

      for (const [name, response] of invalidResponses) {
        respondToStart = () => response;
        vi.mocked(navigateTo).mockClear();
        await settleClick(linkPage, 'Link google');
        expect(vi.mocked(navigateTo), name).not.toHaveBeenCalled();
        expect(
          linkPage.get('[data-testid="link-error"]').text(),
          name,
        ).not.toBe('');
      }

      respondToStart = () => {
        throw createError({ statusCode: 500, message: 'start failed' });
      };
      vi.mocked(navigateTo).mockClear();
      await settleClick(linkPage, 'Link google');
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
      expect(linkPage.get('[data-testid="link-error"]').text()).not.toBe('');

      respondToStart = () => ({
        data: { authorizeUrl: authorizeURLs.google },
      });
      vi.mocked(navigateTo).mockRejectedValueOnce(
        new Error('navigation failed'),
      );
      await settleClick(linkPage, 'Link google');
      expect(linkPage.get('[data-testid="link-error"]').text()).not.toBe('');

      // Reauthentication uses the stable first identity. Refreshing /me lets
      // the same black-box surface exercise every provider without depending
      // on a separate component instance or cached initial fetch.
      const reauthPage = await mountSuspended(SessionsPage, {
        route: '/app/settings/sessions?error=reauth_required',
      });
      await flushPromises();
      respondToStart = (provider) => ({
        data: { authorizeUrl: authorizeURLs[provider] },
      });
      vi.mocked(navigateTo).mockReset();
      vi.mocked(navigateTo).mockResolvedValue(undefined);

      for (const provider of providers) {
        identityProviders = [provider];
        csrfToken = `csrf-reauth-${provider}`;
        await refreshNuxtData();
        await flushPromises();

        expect(
          privilegedAnchors(reauthPage),
          'reauthentication must never expose a privileged GET anchor',
        ).toHaveLength(0);

        const before = capturedStarts.length;
        await settleClick(reauthPage, `Sign in again with ${provider}`);
        expect(capturedStarts).toHaveLength(before + 1);
        expectBodilessCSRFPost(
          capturedStarts[before]!,
          provider,
          'reauth',
          `csrf-reauth-${provider}`,
        );
        expect(vi.mocked(navigateTo)).toHaveBeenLastCalledWith(
          authorizeURLs[provider],
          { external: true },
        );
      }
    },
  );

  it('accepts only the same-origin loopback HTTPS mock path', async () => {
    vi.mocked(navigateTo).mockReset();
    vi.mocked(navigateTo).mockResolvedValue(undefined);
    capturedStarts = [];
    identityProviders = ['google'];
    csrfToken = 'csrf-https-uat';
    authorizeURLs.google
      = 'https://localhost:20443/__uat/oauth/google/authorize';
    respondToStart = (provider) => ({
      data: { authorizeUrl: authorizeURLs[provider] },
    });

    const page = await mountSuspended(SessionsPage, {
      route: '/app/settings/sessions?error=reauth_required',
    });
    await refreshNuxtData();
    await flushPromises();
    window.happyDOM.setURL('https://localhost:20443/app/settings/sessions');
    await settleClick(page, 'Sign in again with google');
    expect(vi.mocked(navigateTo)).toHaveBeenLastCalledWith(
      'https://localhost:20443/__uat/oauth/google/authorize',
      { external: true },
    );

    const invalidAuthorizeURLs = [
      'https://localhost:20444/__uat/oauth/google/authorize',
      'https://127.0.0.1:20443/__uat/oauth/google/authorize',
      'http://localhost:20443/__uat/oauth/google/authorize',
      'https://user:pass@localhost:20443/__uat/oauth/google/authorize',
      'https://localhost:20443/__uat/oauth/google/authorize#fragment',
      'https://localhost:20443/__uat/oauth/github/authorize',
      'https://accounts.google.com/__uat/oauth/google/authorize',
    ];

    for (const authorizeUrl of invalidAuthorizeURLs) {
      respondToStart = () => ({ data: { authorizeUrl } });
      vi.mocked(navigateTo).mockClear();
      await settleClick(page, 'Sign in again with google');
      expect(vi.mocked(navigateTo), authorizeUrl).not.toHaveBeenCalled();
      expect(
        page.get('[data-testid="link-error"]').text(),
        authorizeUrl,
      ).not.toBe('');
    }

    window.happyDOM.setURL('https://accounts.google.com/app/settings/sessions');
    respondToStart = () => ({
      data: {
        authorizeUrl:
          'https://accounts.google.com/__uat/oauth/google/authorize',
      },
    });
    vi.mocked(navigateTo).mockClear();
    await settleClick(page, 'Sign in again with google');
    expect(vi.mocked(navigateTo)).not.toHaveBeenCalled();
    expect(page.get('[data-testid="link-error"]').text()).not.toBe('');
  });
});
