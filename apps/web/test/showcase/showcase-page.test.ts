import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import AppRoot from '../../app/app.vue';
import ShowcasePage from '../../app/pages/showcase.vue';
import { showcaseCopy } from '../../app/i18n/showcase';
import type { ShowcaseItem } from '../../app/lib/showcaseContract';
import { registerCapabilities } from '../support/capabilities';
import { setSiteLocale } from '../support/locale';
import { holdSignedOutRedirects } from '../support/signedOutRedirects';

// The community showcase page: states, URL filters, tiles, pager, and the
// privacy rules (docs/design/showcase.md; AC-SHOW-002, 005, 008, 013, 014).

mockNuxtImport('navigateTo', () => vi.fn());
registerCapabilities({ providerLogin: false, agentAccess: false });
let meStatus = 401;
registerEndpoint('/api/v1/me', (event) => {
  if (meStatus === 200) {
    return {
      data: {
        user: {
          id: 'user-1',
          email: 'dev@aboutme.invalid',
          name: 'Dev User',
          avatarKey: null,
          hasPassword: true,
        },
        csrfToken: 'csrf',
        identities: [],
      },
    };
  }
  setResponseStatus(event, 401);
  return { error: { code: 'session_required', message: 'Sign in.' } };
});

const LISTING_PATH = '/api/v1/public/showcase';
// Raw HTML, browser storage, cookies, and server-side or cached fetching.
const FORBIDDEN = new RegExp([
  'v-html',
  'innerHTML',
  'localStorage',
  'sessionStorage',
  'document\\.cookie',
  'useCookie',
  '\\$fetch',
  'useFetch',
  'useAsyncData',
].join('|'), 'u');

type Reply
  = | { readonly status?: number; readonly body: unknown }
    | { readonly fail: true }
    | { readonly hold: true };

interface Call {
  readonly url: string;
  readonly init: RequestInit | undefined;
}

let calls: Call[] = [];
let replies: Reply[] = [];

function respond(reply: Reply | undefined): Promise<Response> {
  if (reply === undefined) return Promise.reject(new Error('unexpected'));
  if ('hold' in reply) return new Promise(() => {});
  if ('fail' in reply) return Promise.reject(new TypeError('offline'));
  return Promise.resolve(new Response(JSON.stringify(reply.body), {
    status: reply.status ?? 200,
    headers: { 'Content-Type': 'application/json' },
  }));
}

/** Queues replies for the listing; the last one repeats. */
function stubListing(...next: Reply[]): void {
  replies = next;
  calls = [];
  // Everything but the listing, such as the session read, keeps its mock.
  const realFetch = globalThis.fetch;
  const stub = (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : String(input);
    if (!url.startsWith(LISTING_PATH)) return realFetch(input, init);
    calls.push({ url, init });
    return respond(replies.length > 1 ? replies.shift() : replies[0]);
  };
  vi.stubGlobal('fetch', vi.fn(stub));
}

function item(slug: string, extra: Partial<ShowcaseItem> = {}): ShowcaseItem {
  return {
    slug,
    cardVersion: '0123456789abcdef',
    imageText: `Card of ${slug}`,
    language: 'en',
    templateId: 'engineer-compact',
    role: 'backend',
    ...extra,
  };
}

function page(items: ShowcaseItem[], extra: object = {}) {
  return {
    body: {
      items,
      page: 1,
      pageCount: 1,
      total: items.length,
      ...extra,
    },
  };
}

// A page left mounted would keep reacting to later route changes.
const mounted: { unmount(): void }[] = [];

async function mountPage(route = '/showcase') {
  const wrapper = await mountSuspended(ShowcasePage, { route });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

function listingUrls(): string[] {
  return calls.map((call) => call.url);
}

beforeEach(() => {
  setSiteLocale('en');
  clearNuxtData();
  meStatus = 401;
  holdSignedOutRedirects();
});

afterEach(() => {
  for (const wrapper of mounted.splice(0)) {
    try {
      wrapper.unmount();
    } catch {
      // Already unmounted by the test.
    }
  }
  vi.unstubAllGlobals();
});

describe('showcase page states', () => {
  it('renders the heading, lead, and note in both languages', async () => {
    stubListing(page([item('ada-lovelace')]));
    for (const locale of ['en', 'vi'] as const) {
      setSiteLocale(locale);
      const copy = showcaseCopy[locale];
      const wrapper = await mountPage();
      const header = wrapper.get('[data-testid="showcase-header"]');
      expect(header.get('h1').text()).toBe(copy.title);
      expect(header.text()).toContain(copy.lead);
      expect(header.text()).toContain(copy.orderNote);
      expect(header.find('a, button, input, select').exists()).toBe(false);
      wrapper.unmount();
    }
  });

  it('shows the busy loading grid with six skeletons until items arrive',
    async () => {
      stubListing({ hold: true });
      const wrapper = await mountPage();
      const grid = wrapper.get('[data-state="loading"]');
      expect(grid.element.tagName).toBe('UL');
      expect(grid.attributes('aria-busy')).toBe('true');
      const skeletons = grid.findAll('li');
      expect(skeletons).toHaveLength(6);
      for (const skeleton of skeletons) {
        expect(skeleton.attributes('aria-hidden')).toBe('true');
      }
      expect(wrapper.find('[data-showcase-slug]').exists()).toBe(false);
    });

  it('lists the tiles and clears the busy mark', async () => {
    stubListing(page([item('ada-lovelace'), item('grace-hopper')]));
    const wrapper = await mountPage();
    const grid = wrapper.get('[data-state="list"]');
    expect(grid.attributes('aria-busy')).toBeUndefined();
    expect(wrapper.findAll('[data-showcase-slug]').map(
      (tile) => tile.attributes('data-showcase-slug'),
    )).toEqual(['ada-lovelace', 'grace-hopper']);
    expect(wrapper.find('[data-testid="showcase-pager"]').exists()).toBe(false);
  });

  it('shows the empty state with its action while no filter is set',
    async () => {
      stubListing(page([], { pageCount: 0 }));
      const wrapper = await mountPage();
      const empty = wrapper.get('[data-state="empty"]');
      expect(empty.text()).toContain(showcaseCopy.en.empty);
      const action = empty.get('a');
      expect(action.text()).toBe(showcaseCopy.en.emptyAction);
      expect(action.attributes('href')).toBe('/register');
      // The filter rows stay.
      expect(wrapper.find('[data-testid="showcase-roles"]').exists())
        .toBe(true);
      expect(wrapper.find('[data-testid="showcase-pager"]').exists())
        .toBe(false);
    });

  it('sends the empty action to sign-in when registration is closed',
    async () => {
      registerCapabilities({
        providerLogin: false,
        agentAccess: false,
        passwordRegistration: false,
      });
      try {
        stubListing(page([], { pageCount: 0 }));
        const wrapper = await mountPage();
        await vi.waitFor(() => expect(
          wrapper.get('[data-action="showcase-empty-action"]')
            .attributes('href'),
        ).toBe('/login'));
      } finally {
        registerCapabilities({ providerLogin: false, agentAccess: false });
      }
    });

  it('opens the signed-in visitor\'s resumes from the empty state',
    async () => {
      meStatus = 200;
      stubListing(page([], { pageCount: 0 }));
      const wrapper = await mountPage();
      await vi.waitFor(() => {
        const action = wrapper.get('[data-action="showcase-empty-action"]');
        expect(action.attributes('href')).toBe('/app/resumes');
        expect(action.text()).toBe(showcaseCopy.en.emptyActionSignedIn);
      });
    });

  it('shows the no-match line with a filter set and no pager', async () => {
    stubListing(page([], { pageCount: 0 }));
    const wrapper = await mountPage('/showcase?role=qa');
    const line = wrapper.get('[data-state="no-match"]');
    expect(line.text()).toBe(showcaseCopy.en.noMatch);
    expect(line.attributes('role')).toBe('status');
    expect(wrapper.find('[data-testid="showcase-pager"]').exists()).toBe(false);
  });

  it('shows the no-match line and a pager for a page past the end',
    async () => {
      stubListing(page([], { page: 5, pageCount: 3, total: 30 }));
      const wrapper = await mountPage('/showcase?page=5');
      expect(wrapper.get('[data-state="no-match"]').text())
        .toBe(showcaseCopy.en.noMatch);
      const pager = wrapper.get('[data-testid="showcase-pager"]');
      expect(pager.text()).toContain('Page 5 of 3');
      // Previous lands on the last real page; Next is not a link.
      expect(pager.get('a[data-action="showcase-previous"]')
        .attributes('href')).toBe('/showcase?page=3');
      const next = pager.get('[data-action="showcase-next"]');
      expect(next.element.tagName).toBe('SPAN');
      expect(next.attributes('aria-disabled')).toBe('true');
    });

  it('shows the failed state, and Retry reloads the same query', async () => {
    stubListing({ status: 503, body: {} }, page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase?lang=vi');
    const banner = wrapper.get('[data-state="failed"]');
    expect(banner.attributes('role')).toBe('alert');
    expect(banner.text()).toContain(showcaseCopy.en.loadFailed);
    expect(wrapper.find('[data-showcase-slug]').exists()).toBe(false);

    await wrapper.get('[data-action="showcase-retry"]').trigger('click');
    await flushPromises();
    expect(listingUrls()).toEqual([
      `${LISTING_PATH}?lang=vi`,
      `${LISTING_PATH}?lang=vi`,
    ]);
    expect(wrapper.find('[data-state="failed"]').exists()).toBe(false);
    expect(wrapper.find('[data-showcase-slug="ada-lovelace"]').exists())
      .toBe(true);
  });

  it('keeps the banner and marks Retry busy while it retries', async () => {
    stubListing({ fail: true }, { hold: true });
    const wrapper = await mountPage();
    const retry = wrapper.get('[data-action="showcase-retry"]');
    expect(retry.attributes('aria-disabled')).toBeUndefined();
    await retry.trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-state="failed"]').exists()).toBe(true);
    expect(wrapper.get('[data-action="showcase-retry"]')
      .attributes('aria-disabled')).toBe('true');
    // A second click while busy sends nothing.
    await wrapper.get('[data-action="showcase-retry"]').trigger('click');
    expect(calls).toHaveLength(2);
  });

  it.each([
    ['a rate-limited reply', { status: 429, body: {} }],
    ['a bad request', { status: 400, body: {} }],
    ['an invalid body', { body: { items: 'x' } }],
    ['a network error', { fail: true as const }],
  ])('shows the failed state for %s', async (_name, reply) => {
    stubListing(reply);
    const wrapper = await mountPage();
    expect(wrapper.find('[data-state="failed"]').exists()).toBe(true);
  });
});

describe('showcase filters in the URL', () => {
  it('reads the filters from the URL and sends only known values',
    async () => {
      stubListing(page([item('ada-lovelace')]));
      const wrapper = await mountPage(
        '/showcase?role=backend&lang=vi&template=custom&page=2',
      );
      expect(listingUrls()).toEqual([
        `${LISTING_PATH}?role=backend&lang=vi&template=custom&page=2`,
      ]);
      expect(wrapper.get('[data-role="backend"]').attributes('aria-pressed'))
        .toBe('true');
      expect(wrapper.get('[data-lang="vi"]').attributes('aria-pressed'))
        .toBe('true');
      expect((wrapper.get('select').element as HTMLSelectElement).value)
        .toBe('custom');
    });

  it('counts unknown values as All and does not send them', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage(
      '/showcase?role=wizard&lang=fr&template=nope&page=0&utm_source=x',
    );
    expect(listingUrls()).toEqual([LISTING_PATH]);
    expect(wrapper.get('[data-role="all"]').attributes('aria-pressed'))
      .toBe('true');
    expect(wrapper.get('[data-lang="all"]').attributes('aria-pressed'))
      .toBe('true');
  });

  // Navigation settles asynchronously, so each step waits for the route.
  async function expectQuery(query: Record<string, string>): Promise<void> {
    await vi.waitFor(() => {
      expect(useRouter().currentRoute.value.query).toEqual(query);
    });
  }

  it('writes a role change to the URL, drops page, and reloads', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase?lang=en&page=3');
    await wrapper.get('[data-role="frontend"]').trigger('click');
    await expectQuery({ role: 'frontend', lang: 'en' });
    await vi.waitFor(() => expect(listingUrls().at(-1))
      .toBe(`${LISTING_PATH}?role=frontend&lang=en`));
    // A pressed chip stays pressed when pressed again.
    const before = calls.length;
    await wrapper.get('[data-role="frontend"]').trigger('click');
    await flushPromises();
    await expectQuery({ role: 'frontend', lang: 'en' });
    expect(calls).toHaveLength(before);
  });

  it('clears a filter back to All', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase?role=qa&lang=vi');
    await wrapper.get('[data-role="all"]').trigger('click');
    await expectQuery({ lang: 'vi' });
    await wrapper.get('[data-lang="all"]').trigger('click');
    await expectQuery({});
    await vi.waitFor(() => expect(listingUrls().at(-1)).toBe(LISTING_PATH));
  });

  it('writes language and template changes to the URL', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase?role=qa');
    await wrapper.get('[data-lang="en"]').trigger('click');
    await expectQuery({ role: 'qa', lang: 'en' });
    const select = wrapper.get('select');
    await select.setValue('custom');
    await expectQuery({ role: 'qa', lang: 'en', template: 'custom' });
    await vi.waitFor(() => expect(listingUrls().at(-1))
      .toBe(`${LISTING_PATH}?role=qa&lang=en&template=custom`));
    await select.setValue('');
    await expectQuery({ role: 'qa', lang: 'en' });
  });

  it('offers All, the 20 presets by name, then Custom design', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage();
    const options = wrapper.get('select').findAll('option');
    const labels = options.map((option) => option.text());
    expect(labels[0]).toBe('All templates');
    expect(labels.at(-1)).toBe('Custom design');
    const presets = labels.slice(1, -1);
    expect(presets).toHaveLength(20);
    expect(presets).toEqual(
      [...presets].sort((left, right) => left.localeCompare(right, 'en')),
    );
    expect(options.map((option) => option.attributes('value'))[0]).toBe('');
    expect(options.at(-1)!.attributes('value')).toBe('custom');
    expect(wrapper.get('label[for]').text()).toBe('Template');
  });

  it('offers the role row All, nine roles, then Other, with a group label',
    async () => {
      stubListing(page([item('ada-lovelace')]));
      const wrapper = await mountPage();
      const group = wrapper.get('[data-testid="showcase-roles"]');
      expect(group.attributes('aria-label')).toBe('Filter by role');
      expect(group.findAll('[data-role]').map(
        (chip) => chip.attributes('data-role'),
      )).toEqual([
        'all', 'backend', 'frontend', 'mobile', 'devops', 'data-ai', 'qa',
        'fresher', 'brse', 'security', 'other',
      ]);
      const languages = wrapper.get('[data-testid="showcase-languages"]');
      expect(languages.attributes('aria-label')).toBe('Resume language');
      expect(languages.findAll('[data-lang]').map(
        (chip) => chip.text(),
      )).toEqual(['All languages', 'Vietnamese', 'English']);
    });

  it('keeps the filters and sets ?page= on the pager links', async () => {
    stubListing(page([item('ada-lovelace')], { page: 2, pageCount: 3 }));
    const wrapper = await mountPage('/showcase?role=qa&page=2');
    const pager = wrapper.get('[data-testid="showcase-pager"]');
    expect(pager.get('[data-action="showcase-previous"]').attributes('href'))
      .toBe('/showcase?role=qa');
    expect(pager.get('[data-action="showcase-next"]').attributes('href'))
      .toBe('/showcase?role=qa&page=3');
    expect(pager.get('p').text()).toBe('Page 2 of 3');
    expect(pager.attributes('aria-labelledby'))
      .toBe(pager.get('p').attributes('id'));
  });

  it('disables Previous on the first page and Next on the last', async () => {
    stubListing(page([item('ada-lovelace')], { page: 1, pageCount: 2 }));
    const first = await mountPage();
    const previous = first.get('[data-action="showcase-previous"]');
    expect(previous.element.tagName).toBe('SPAN');
    expect(previous.attributes('aria-disabled')).toBe('true');
    expect(previous.attributes('href')).toBeUndefined();
    expect(first.get('[data-action="showcase-next"]').element.tagName)
      .toBe('A');
    first.unmount();

    stubListing(page([item('ada-lovelace')], { page: 2, pageCount: 2 }));
    const last = await mountPage('/showcase?page=2');
    expect(last.get('[data-action="showcase-next"]').element.tagName)
      .toBe('SPAN');
    expect(last.get('[data-action="showcase-previous"]').element.tagName)
      .toBe('A');
  });
});

describe('showcase tiles', () => {
  it('shows only the closed fields, as text', async () => {
    stubListing(page([
      item('ada-lovelace', {
        imageText: '<b>Ada</b> Lovelace',
        templateId: 'engineer-compact',
        role: 'data-ai',
        language: 'vi',
      }),
    ]));
    const wrapper = await mountPage();
    const tile = wrapper.get('[data-showcase-slug="ada-lovelace"]');
    const link = tile.get('[data-showcase-tile]');
    expect(link.attributes('href')).toBe('/ada-lovelace');
    expect(link.attributes('rel')).toBe('nofollow');
    expect(link.attributes('target')).toBeUndefined();
    const image = tile.get('img');
    expect(image.attributes('src')).toBe(
      '/api/v1/public/resumes/ada-lovelace/og/0123456789abcdef.png',
    );
    expect(image.attributes('alt')).toBe('<b>Ada</b> Lovelace');
    expect(image.attributes('loading')).toBe('lazy');
    expect(image.attributes('decoding')).toBe('async');
    expect(image.attributes('width')).toBe('1200');
    expect(image.attributes('height')).toBe('630');
    expect(tile.find('b').exists()).toBe(false);
    // Meta line, role chip, Report: nothing else, and no slug as text.
    expect(link.get('.showcase-tile__line').text())
      .toBe('Engineer Compact·Vietnamese');
    expect(link.get('[data-showcase-role]').text()).toBe('Data/AI');
    expect(tile.text()).toBe('Engineer Compact·VietnameseData/AIReport');
    expect(tile.text()).not.toContain('ada-lovelace');
    expect(tile.text()).not.toMatch(/@|\+\d|\d{4}/);
  });

  it('names a custom design and omits an absent role chip', async () => {
    stubListing(page([
      item('grace-hopper', { templateId: null, role: null, language: 'other' }),
    ]));
    const tile = (await mountPage()).get('[data-showcase-slug]');
    expect(tile.get('[data-showcase-tile]').text())
      .toBe('Custom design·Other language');
    expect(tile.find('[data-showcase-role]').exists()).toBe(false);
  });

  it('opens the Report link as an email with the interface-language subject',
    async () => {
      stubListing(page([item('ada-lovelace')]));
      const en = await mountPage();
      const report = en.get('[data-action="showcase-report"]');
      expect(report.text()).toBe('Report');
      expect(report.attributes('aria-label'))
        .toBe('Report resume ada-lovelace by email');
      expect(report.attributes('href')).toBe(
        'mailto:danny@aboutme.vn?subject='
        + encodeURIComponent('Report showcase: ada-lovelace'),
      );
      // Report is never inside the tile link.
      expect(en.get('[data-showcase-tile]')
        .find('[data-action="showcase-report"]').exists()).toBe(false);
      en.unmount();

      setSiteLocale('vi');
      const vi = await mountPage();
      const viReport = vi.get('[data-action="showcase-report"]');
      expect(viReport.text()).toBe('Báo cáo');
      expect(viReport.attributes('aria-label'))
        .toBe('Báo cáo CV ada-lovelace qua email');
      expect(viReport.attributes('href')).toBe(
        'mailto:danny@aboutme.vn?subject='
        + encodeURIComponent('Báo cáo trang Cộng đồng: ada-lovelace'),
      );
    });

  it('keeps the tile when its card fails to load', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage();
    const image = wrapper.get('img');
    await image.trigger('error');
    expect(image.classes()).toContain('is-failed');
    expect(image.attributes('alt')).toBe('Card of ada-lovelace');
  });
});

describe('showcase privacy', () => {
  it('requests the listing without credentials or caching', async () => {
    stubListing(page([item('ada-lovelace')]));
    await mountPage();
    expect(calls).toHaveLength(1);
    expect(calls[0]!.init).toMatchObject({
      cache: 'no-store',
      credentials: 'omit',
    });
  });

  it('loads the listing again on every visit, keeping nothing', async () => {
    stubListing(page([item('ada-lovelace')]));
    (await mountPage()).unmount();
    (await mountPage()).unmount();
    expect(calls).toHaveLength(2);
  });

  it('touches no cookie or browser storage', async () => {
    const before = document.cookie;
    const set = vi.spyOn(Storage.prototype, 'setItem');
    const get = vi.spyOn(Storage.prototype, 'getItem');
    stubListing(page([item('ada-lovelace')], { page: 1, pageCount: 2 }));
    const wrapper = await mountPage('/showcase?role=qa');
    await wrapper.get('[data-role="backend"]').trigger('click');
    await flushPromises();
    expect(document.cookie).toBe(before);
    expect(set).not.toHaveBeenCalled();
    expect(get).not.toHaveBeenCalled();
  });

  it('sends noindex and nofollow, once, with the page description',
    async () => {
      stubListing(page([item('ada-lovelace')]));
      const app = await mountSuspended(AppRoot, { route: '/showcase' });
      mounted.push(app);
      await flushPromises();
      // unhead writes the DOM on a zero-delay timer.
      await vi.waitFor(() => {
        const robots = document.head.querySelectorAll('meta[name="robots"]');
        expect(robots).toHaveLength(1);
        expect(robots[0]!.getAttribute('content')).toBe('noindex, nofollow');
        expect(document.head.querySelector('meta[name="description"]')
          ?.getAttribute('content')).toBe(showcaseCopy.en.description);
        expect(document.title).toBe('Community resumes · aboutme.vn');
      });
      expect(document.head.querySelector('link[rel="canonical"]')).toBeNull();
      app.unmount();
    });

  it('has no raw-HTML rendering, storage, or cookie code in its sources',
    () => {
      const root = join(process.cwd(), 'app');
      const files = [
        'pages/showcase.vue',
        'components/showcase/ShowcaseTile.vue',
        'components/showcase/ShowcaseFilters.vue',
        'components/showcase/ShowcasePager.vue',
        'composables/useShowcase.ts',
        'lib/showcaseContract.ts',
        'lib/showcaseQuery.ts',
      ];
      for (const file of files) {
        const text = readFileSync(join(root, file), 'utf8');
        expect(text, file).not.toMatch(FORBIDDEN);
      }
    });
});
