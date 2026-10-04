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

// The community showcase page: states, URL filters, count line, filter bar
// and sheet, active-filter chips, tiles, invite card, pager, and the privacy
// rules (docs/design/showcase.md; docs/design/ui/showcase.md; AC-SHOW-002,
// 005, 008, 013, 014).

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

/** `attach` puts the page in the document, which focus checks need. */
async function mountPage(route = '/showcase', attach = false) {
  const wrapper = await mountSuspended(
    ShowcasePage,
    attach ? { route, attachTo: document.body } : { route },
  );
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

/**
 * Stubs `matchMedia`: the 1024 px query follows `set`, every other query
 * does not match. The page's `useMediaQuery` watcher listens for changes.
 */
function stubViewport(initialWide: boolean) {
  let wide = initialWide;
  const listeners = new Set<(event: { matches: boolean }) => void>();
  vi.stubGlobal('matchMedia', (query: string) => {
    const tracked = query.includes('1024px');
    return {
      media: query,
      onchange: null,
      get matches() {
        return tracked && wide;
      },
      addEventListener: (_type: string, listener: never) => {
        if (tracked) listeners.add(listener);
      },
      removeEventListener: (_type: string, listener: never) => {
        listeners.delete(listener);
      },
      addListener: (listener: never) => {
        if (tracked) listeners.add(listener);
      },
      removeListener: (listener: never) => {
        listeners.delete(listener);
      },
      dispatchEvent: () => true,
    };
  });
  return {
    set(next: boolean): void {
      wide = next;
      for (const listener of [...listeners]) listener({ matches: next });
    },
  };
}

let viewport = stubViewport(false);

function sheetElement(): HTMLElement | null {
  return document.body.querySelector<HTMLElement>(
    '[data-testid="showcase-filter-sheet"]',
  );
}

function bodyElement(selector: string): HTMLElement {
  const found = document.body.querySelector<HTMLElement>(selector);
  if (found === null) throw new Error(`missing ${selector}`);
  return found;
}

/** Focuses the Filters button, as a click does, then opens the sheet. */
async function openSheet(wrapper: Awaited<ReturnType<typeof mountPage>>) {
  const button = wrapper.get('[data-action="showcase-filters-open"]');
  (button.element as HTMLElement).focus();
  await button.trigger('click');
  await flushPromises();
  await vi.waitFor(() => expect(sheetElement()).not.toBeNull());
}

async function expectQuery(query: Record<string, string>): Promise<void> {
  await vi.waitFor(() => {
    expect(useRouter().currentRoute.value.query).toEqual(query);
  });
}

function countText(wrapper: Awaited<ReturnType<typeof mountPage>>): string {
  return wrapper.get('[data-testid="showcase-count"]').text();
}

function listingUrls(): string[] {
  return calls.map((call) => call.url);
}

beforeEach(() => {
  setSiteLocale('en');
  viewport = stubViewport(false);
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
  it(
    'renders the heading and lead in both languages, with no band',
    async () => {
      stubListing(page([item('ada-lovelace')]));
      for (const locale of ['en', 'vi'] as const) {
        setSiteLocale(locale);
        const copy = showcaseCopy[locale];
        const wrapper = await mountPage();
        const header = wrapper.get('[data-testid="showcase-header"]');
        expect(header.get('h1').text()).toBe(copy.title);
        expect(header.get('p').text()).toBe(copy.lead);
        expect(header.find('a, button, input, select').exists()).toBe(false);
        expect(header.classes()).not.toContain('bg-surface-blue');
        wrapper.unmount();
      }
    },
  );

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
    // The count line is the live region; the line adds no second one.
    expect(line.attributes('role')).toBeUndefined();
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

  it('builds a second change on the first while the router settles',
    async () => {
      stubListing(page([item('ada-lovelace')]));
      const wrapper = await mountPage('/showcase?role=qa&lang=vi');
      const router = useRouter();
      const spied = router.replace as unknown as {
        getMockImplementation(): ((to: never) => Promise<unknown>) | undefined;
        mockImplementation(impl: (to: never) => Promise<unknown>): void;
      };
      const settle = spied.getMockImplementation()!;
      // Navigation lands late, as it can on a slow device.
      spied.mockImplementation(async (to) => {
        await new Promise((resolve) => setTimeout(resolve, 50));
        return settle(to);
      });
      try {
        await wrapper.get('[data-role="all"]').trigger('click');
        await wrapper.get('[data-lang="all"]').trigger('click');
        await expectQuery({});
      } finally {
        spied.mockImplementation(settle);
      }
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
      const headings = wrapper.get('[data-testid="showcase-filter-rail"]')
        .findAll('.showcase-filters__heading');
      expect(headings.map((heading) => heading.text()))
        .toEqual(['Role', 'Resume language']);
      expect(languages.attributes('aria-labelledby'))
        .toBe(headings[1]!.attributes('id'));
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
    // Role chip and language on one row, then the template, then the footer
    // with the slug as plain text outside the tile link (S14).
    expect(link.get('.showcase-tile__line').text()).toBe('Data/AIVietnamese');
    expect(link.get('[data-showcase-role]').text()).toBe('Data/AI');
    expect(link.get('.showcase-tile__template').text())
      .toBe('Engineer Compact');
    expect(link.text()).not.toContain('ada-lovelace');
    const slug = tile.get('.showcase-tile__slug');
    expect(slug.text()).toBe('aboutme.vn/ada-lovelace');
    expect(slug.element.tagName).toBe('SPAN');
    expect(link.element.contains(slug.element)).toBe(false);
    expect(tile.text())
      .toBe('Data/AIVietnameseEngineer Compactaboutme.vn/ada-lovelace');
    expect(tile.text()).not.toMatch(/@|\+\d|\d{4}/);
  });

  it('names a custom design and omits an absent role chip', async () => {
    stubListing(page([
      item('grace-hopper', { templateId: null, role: null, language: 'other' }),
    ]));
    const tile = (await mountPage()).get('[data-showcase-slug]');
    expect(tile.get('[data-showcase-tile]').text())
      .toBe('Other languageCustom design');
    expect(tile.find('[data-showcase-role]').exists()).toBe(false);
  });

  it('opens the Report link as an email with the interface-language subject',
    async () => {
      stubListing(page([item('ada-lovelace')]));
      const en = await mountPage();
      const report = en.get('[data-action="showcase-report"]');
      // An icon link: the name is its aria-label, with no visible text.
      expect(report.text()).toBe('');
      expect(report.find('svg').attributes('aria-hidden')).toBe('true');
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
      expect(viReport.text()).toBe('');
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
        'components/showcase/ShowcaseActiveFilters.vue',
        'components/showcase/ShowcaseFilterBar.vue',
        'components/showcase/ShowcaseFilterSheet.vue',
        'components/showcase/ShowcaseFilters.vue',
        'components/showcase/ShowcaseInvite.vue',
        'components/showcase/ShowcasePager.vue',
        'components/showcase/ShowcaseTile.vue',
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

describe('showcase count line', () => {
  it.each([
    ['en', 12, '12 resumes · earliest added first'],
    ['en', 1, '1 resume · earliest added first'],
    ['en', 0, '0 resumes · earliest added first'],
    ['en', 1234, '1,234 resumes · earliest added first'],
    ['vi', 12, '12 CV · sớm nhất trước'],
    ['vi', 1, '1 CV · sớm nhất trước'],
    ['vi', 1234, '1.234 CV · sớm nhất trước'],
  ] as const)('reads the listing total in %s for %i', async (
    locale,
    total,
    text,
  ) => {
    setSiteLocale(locale);
    stubListing(page([item('ada-lovelace')], { total }));
    const wrapper = await mountPage();
    expect(countText(wrapper)).toBe(text);
  });

  it('is the polite live region and bolds only the count', async () => {
    stubListing(page([item('ada-lovelace')], { total: 7 }));
    const wrapper = await mountPage();
    const line = wrapper.get('[data-testid="showcase-count"]');
    expect(line.element.tagName).toBe('P');
    expect(line.attributes('role')).toBe('status');
    expect(line.attributes('aria-atomic')).toBe('true');
    expect(line.get('strong').text()).toBe('7 resumes');
  });

  it('shows a skeleton only before the first total', async () => {
    stubListing({ hold: true });
    const wrapper = await mountPage();
    const line = wrapper.get('[data-testid="showcase-count"]');
    expect(line.find('[data-slot="skeleton"]').exists()).toBe(true);
    expect(line.text()).toBe('');
  });

  it('keeps the last text while a new load runs', async () => {
    stubListing(page([item('ada-lovelace')], { total: 5 }), { hold: true });
    const wrapper = await mountPage();
    expect(countText(wrapper)).toBe('5 resumes · earliest added first');
    await wrapper.get('[data-role="qa"]').trigger('click');
    await expectQuery({ role: 'qa' });
    await vi.waitFor(() => expect(wrapper.find('[data-state="loading"]')
      .exists()).toBe(true));
    const line = wrapper.get('[data-testid="showcase-count"]');
    expect(line.text()).toBe('5 resumes · earliest added first');
    expect(line.find('[data-slot="skeleton"]').exists()).toBe(false);
  });

  it('keeps the last text when a later load fails', async () => {
    stubListing(page([item('ada-lovelace')], { total: 5 }), { fail: true });
    const wrapper = await mountPage();
    await wrapper.get('[data-role="qa"]').trigger('click');
    await expectQuery({ role: 'qa' });
    await vi.waitFor(() => expect(wrapper.find('[data-state="failed"]')
      .exists()).toBe(true));
    expect(countText(wrapper)).toBe('5 resumes · earliest added first');
  });

  it('names only the order when the first load fails', async () => {
    stubListing({ fail: true });
    const wrapper = await mountPage();
    expect(countText(wrapper)).toBe(showcaseCopy.en.countNoTotal);
    expect(wrapper.get('[data-testid="showcase-count"]')
      .find('[data-slot="skeleton"]').exists()).toBe(false);
  });

  // A change that keeps the total leaves the text as it was; the keyed node
  // is replaced so the live region announces the same text again
  // (docs/design/ui/showcase.md, Count line).
  describe('announcing a change with the same total', () => {
    function textNode(wrapper: Awaited<ReturnType<typeof mountPage>>) {
      return wrapper.get('[data-testid="showcase-count"] > span').element;
    }

    async function settleLoad(
      wrapper: Awaited<ReturnType<typeof mountPage>>,
    ): Promise<void> {
      await vi.waitFor(() => expect(calls).toHaveLength(2));
      await vi.waitFor(() => expect(wrapper.find('[data-state="loading"]')
        .exists()).toBe(false));
      await flushPromises();
    }

    it('replaces the text node after a filter change settles', async () => {
      const reply = page([item('ada-lovelace')], { total: 5, pageCount: 2 });
      stubListing(reply, reply);
      const wrapper = await mountPage();
      const before = textNode(wrapper);
      await wrapper.get('[data-role="qa"]').trigger('click');
      await settleLoad(wrapper);
      expect(countText(wrapper)).toBe('5 resumes · earliest added first');
      expect(textNode(wrapper)).not.toBe(before);
    });

    it('keeps the text node through a pager move', async () => {
      const reply = page([item('ada-lovelace')], { total: 5, pageCount: 2 });
      stubListing(reply, reply);
      const wrapper = await mountPage();
      const before = textNode(wrapper);
      // The pager link changes only `page` in the route; this harness mocks
      // NuxtLink's navigation, so the test moves the route as the link does.
      await useRouter().replace({ path: '/showcase', query: { page: '2' } });
      await settleLoad(wrapper);
      expect(listingUrls().at(-1)).toBe(`${LISTING_PATH}?page=2`);
      expect(textNode(wrapper)).toBe(before);
    });

    it('replaces the sheet status node the same way', async () => {
      const reply = page([item('ada-lovelace')], { total: 5, pageCount: 2 });
      stubListing(reply, reply);
      const wrapper = await mountPage('/showcase', true);
      await openSheet(wrapper);
      const status = () => bodyElement(
        '[data-testid="showcase-sheet-status"] > span',
      );
      const before = status();
      bodyElement(
        '[data-testid="showcase-filter-sheet"] [data-role="qa"]',
      ).click();
      await settleLoad(wrapper);
      expect(status().textContent!.trim())
        .toBe('5 resumes · earliest added first');
      expect(status()).not.toBe(before);
    });
  });

  it('reads 0 for a filter with no match', async () => {
    stubListing(page([], { pageCount: 0 }));
    const wrapper = await mountPage('/showcase?role=qa');
    expect(countText(wrapper)).toBe('0 resumes · earliest added first');
  });
});

describe('showcase filter bar and rail', () => {
  it('holds the bar, the rail, and the shared fields in the server HTML',
    async () => {
      stubListing({ hold: true });
      const wrapper = await mountPage();
      const bar = wrapper.get('[data-testid="showcase-filter-bar"]');
      expect(bar.find('[data-testid="showcase-count"]').exists()).toBe(true);
      const rail = wrapper.get('[data-testid="showcase-filter-rail"]');
      expect(rail.element.tagName).toBe('ASIDE');
      expect(rail.attributes('aria-label')).toBe('Filters');
      expect(rail.find('[data-testid="showcase-filters"]').exists())
        .toBe(true);
      // No role row outside the rail, and none in the bar.
      expect(bar.find('[data-testid="showcase-roles"]').exists()).toBe(false);
    });

  it('shows the Filters button with no badge while no filter is on',
    async () => {
      stubListing(page([item('ada-lovelace')]));
      const wrapper = await mountPage();
      const button = wrapper.get('[data-action="showcase-filters-open"]');
      expect(button.text()).toBe('Filters');
      expect(button.attributes('aria-label')).toBeUndefined();
      expect(button.attributes('aria-haspopup')).toBe('dialog');
      expect(button.attributes('aria-expanded')).toBe('false');
      expect(wrapper.find('[data-testid="showcase-filters-badge"]').exists())
        .toBe(false);
      expect(wrapper.find('[data-testid="showcase-active-filters"]').exists())
        .toBe(false);
    });

  it.each([
    ['en', '/showcase?role=qa', '1', 'Filters, 1 active'],
    ['en', '/showcase?role=qa&lang=vi', '2', 'Filters, 2 active'],
    ['en', '/showcase?role=qa&lang=vi&template=custom', '3',
      'Filters, 3 active'],
    ['vi', '/showcase?template=custom', '1', 'Bộ lọc, 1 đang bật'],
    ['vi', '/showcase?role=wizard&page=3', undefined, undefined],
  ] as const)('counts the filters on for %s %s', async (
    locale,
    route,
    badge,
    name,
  ) => {
    setSiteLocale(locale);
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage(route);
    const button = wrapper.get('[data-action="showcase-filters-open"]');
    expect(button.attributes('aria-label')).toBe(name);
    const badgeElement = wrapper.find('[data-testid="showcase-filters-badge"]');
    if (badge === undefined) {
      expect(badgeElement.exists()).toBe(false);
      return;
    }
    expect(badgeElement.text()).toBe(badge);
    expect(badgeElement.attributes('aria-hidden')).toBe('true');
    // The name starts with the visible label (WCAG 2.5.3).
    expect(name!.startsWith(button.text().replace(badge, '').trim()))
      .toBe(true);
  });

  it('reads the rail from the URL and changes filters from it', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase?role=qa');
    const rail = wrapper.get('[data-testid="showcase-filter-rail"]');
    expect(rail.get('[data-role="qa"]').attributes('aria-pressed'))
      .toBe('true');
    await rail.get('[data-lang="en"]').trigger('click');
    await expectQuery({ role: 'qa', lang: 'en' });
  });
});

describe('showcase filter sheet', () => {
  it('opens a modal dialog named Filters with every filter', async () => {
    stubListing(page([item('ada-lovelace')], { total: 12 }));
    const wrapper = await mountPage('/showcase', true);
    expect(sheetElement()).toBeNull();
    await openSheet(wrapper);
    const sheet = sheetElement()!;
    expect(sheet.getAttribute('role')).toBe('dialog');
    const title = document.getElementById(
      sheet.getAttribute('aria-labelledby')!,
    );
    expect(title?.textContent?.trim()).toBe('Filters');
    expect(title?.tagName).toBe('H2');
    expect(sheet.querySelectorAll('[data-testid="showcase-roles"] [data-role]'))
      .toHaveLength(11);
    expect(sheet.querySelectorAll(
      '[data-testid="showcase-languages"] [data-lang]',
    )).toHaveLength(3);
    const options = sheet.querySelectorAll('select[name="template"] option');
    expect(options).toHaveLength(22);
    expect(options[0]!.textContent?.trim()).toBe('All templates');
    expect(options[21]!.textContent?.trim()).toBe('Custom design');
    expect(sheet.querySelector('[data-action="showcase-filters-clear"]')
      ?.textContent?.trim()).toBe('Clear filters');
    expect(sheet.querySelector('[data-action="showcase-filters-apply"]')
      ?.textContent?.trim()).toBe('Show 12 resumes');
    expect(wrapper.get('[data-action="showcase-filters-open"]')
      .attributes('aria-expanded')).toBe('true');
  });

  it('draws its own close button and not the built-in one', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase', true);
    await openSheet(wrapper);
    const buttons = [...sheetElement()!.querySelectorAll('button')];
    const closers = buttons.filter(
      (button) => button.getAttribute('aria-label') === 'Close'
        || button.textContent?.trim() === 'Close',
    );
    expect(closers).toHaveLength(1);
    expect(closers[0]!.getAttribute('data-action'))
      .toBe('showcase-filters-close');
    expect(closers[0]!.textContent?.trim()).toBe('');
    await vi.waitFor(() => expect(document.activeElement).toBe(closers[0]));
  });

  it('keeps the sheet ids apart from the rail ids', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase', true);
    await openSheet(wrapper);
    const ids = [...document.body.querySelectorAll(
      '[data-testid="showcase-filters"] [id]',
    )].map((element) => element.id);
    expect(ids.length).toBeGreaterThan(0);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it('closes on Escape and returns focus to the Filters button', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase', true);
    await openSheet(wrapper);
    sheetElement()!.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    );
    await flushPromises();
    expect(sheetElement()).toBeNull();
    await vi.waitFor(() => expect(document.activeElement).toBe(
      wrapper.get('[data-action="showcase-filters-open"]').element,
    ));
  });

  it('closes from its close button and returns focus', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase', true);
    await openSheet(wrapper);
    bodyElement('[data-action="showcase-filters-close"]').click();
    await flushPromises();
    expect(sheetElement()).toBeNull();
    await vi.waitFor(() => expect(document.activeElement).toBe(
      wrapper.get('[data-action="showcase-filters-open"]').element,
    ));
  });

  it('applies a change at once and closes with the filters kept', async () => {
    stubListing(page([item('ada-lovelace')], { total: 12 }));
    const wrapper = await mountPage('/showcase?page=2', true);
    await openSheet(wrapper);
    bodyElement('[data-testid="showcase-filter-sheet"] [data-role="qa"]')
      .click();
    await expectQuery({ role: 'qa' });
    await vi.waitFor(() => expect(listingUrls().at(-1))
      .toBe(`${LISTING_PATH}?role=qa`));
    expect(sheetElement()).not.toBeNull();
    bodyElement('[data-action="showcase-filters-apply"]').click();
    await flushPromises();
    expect(sheetElement()).toBeNull();
    await expectQuery({ role: 'qa' });
    expect(wrapper.get('[data-action="showcase-filters-open"]')
      .attributes('aria-label')).toBe('Filters, 1 active');
  });

  it('labels Show results with the total, one resume, then loading',
    async () => {
      stubListing(page([item('ada-lovelace')], { total: 1 }), { hold: true });
      const wrapper = await mountPage('/showcase', true);
      await openSheet(wrapper);
      const apply = (): string => bodyElement(
        '[data-action="showcase-filters-apply"]',
      ).textContent!.trim();
      expect(apply()).toBe('Show 1 resume');
      bodyElement('[data-testid="showcase-filter-sheet"] [data-lang="vi"]')
        .click();
      await expectQuery({ lang: 'vi' });
      await vi.waitFor(() => expect(apply()).toBe('Show results'));
    });

  it('formats the Show results total in Vietnamese', async () => {
    setSiteLocale('vi');
    stubListing(page([item('ada-lovelace')], { total: 1234 }));
    const wrapper = await mountPage('/showcase', true);
    await openSheet(wrapper);
    expect(bodyElement('[data-action="showcase-filters-apply"]')
      .textContent!.trim()).toBe('Xem 1.234 CV');
    expect(bodyElement('[data-action="showcase-filters-clear"]')
      .textContent!.trim()).toBe('Xóa bộ lọc');
  });

  it('clears every filter, keeps the sheet open, and keeps focus', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage(
      '/showcase?role=qa&lang=vi&template=custom',
      true,
    );
    await openSheet(wrapper);
    const clear = bodyElement('[data-action="showcase-filters-clear"]');
    clear.focus();
    clear.click();
    await expectQuery({});
    await flushPromises();
    expect(sheetElement()).not.toBeNull();
    expect(document.activeElement).toBe(clear);
    expect(wrapper.find('[data-testid="showcase-filters-badge"]').exists())
      .toBe(false);
  });

  it('mirrors the count line in its own status region while open',
    async () => {
      stubListing(page([item('ada-lovelace')], { total: 3 }));
      const wrapper = await mountPage('/showcase', true);
      await openSheet(wrapper);
      const status = bodyElement('[data-testid="showcase-sheet-status"]');
      expect(status.getAttribute('role')).toBe('status');
      expect(status.getAttribute('aria-atomic')).toBe('true');
      expect(status.textContent!.trim()).toBe(countText(wrapper));
      expect(status.textContent!.trim())
        .toBe('3 resumes · earliest added first');
      bodyElement('[data-action="showcase-filters-close"]').click();
      await flushPromises();
      expect(document.body.querySelector(
        '[data-testid="showcase-sheet-status"]',
      )).toBeNull();
    });

  it('closes when the viewport reaches 1024 px and focuses the rail',
    async () => {
      stubListing(page([item('ada-lovelace')]));
      const wrapper = await mountPage('/showcase?role=qa', true);
      await openSheet(wrapper);
      viewport.set(true);
      await flushPromises();
      expect(sheetElement()).toBeNull();
      await vi.waitFor(() => expect(document.activeElement).toBe(
        wrapper.get('[data-testid="showcase-filter-rail"] [data-role="qa"]')
          .element,
      ));
    });
});

describe('showcase active filters', () => {
  const ROUTE = '/showcase?role=backend&lang=vi&template=custom';

  function chips(wrapper: Awaited<ReturnType<typeof mountPage>>) {
    return wrapper.get('[data-testid="showcase-active-filters"]')
      .findAll('[data-action="showcase-filter-remove"]');
  }

  it('lists one chip per filter in order, then Clear all', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage(ROUTE);
    const group = wrapper.get('[data-testid="showcase-active-filters"]');
    expect(group.attributes('role')).toBe('group');
    expect(group.attributes('aria-label')).toBe('Active filters');
    expect(chips(wrapper).map((chip) => chip.attributes('data-filter')))
      .toEqual(['role', 'lang', 'template']);
    expect(chips(wrapper).map((chip) => chip.attributes('aria-label')))
      .toEqual([
        'Backend, remove filter',
        'Vietnamese, remove filter',
        'Custom design, remove filter',
      ]);
    expect(chips(wrapper).map((chip) => chip.text()))
      .toEqual(['Backend', 'Vietnamese', 'Custom design']);
    expect(group.get('[data-action="showcase-filters-clear-all"]').text())
      .toBe('Clear all');
    // It is the first row of the results area.
    expect(wrapper.get('[data-testid="showcase-results"]').element
      .firstElementChild).toBe(group.element);
  });

  it('names a preset template and speaks Vietnamese', async () => {
    setSiteLocale('vi');
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase?template=engineer-compact');
    const [chip] = chips(wrapper);
    expect(chip!.text()).toBe('Engineer Compact');
    expect(chip!.attributes('aria-label'))
      .toBe('Engineer Compact, gỡ bộ lọc');
    expect(wrapper.get('[data-action="showcase-filters-clear-all"]').text())
      .toBe('Xóa hết');
  });

  it('removes one filter and focuses the next chip', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage(ROUTE, true);
    (chips(wrapper)[0]!.element as HTMLElement).focus();
    await chips(wrapper)[0]!.trigger('click');
    await expectQuery({ lang: 'vi', template: 'custom' });
    await vi.waitFor(() => expect(document.activeElement)
      .toBe(chips(wrapper)[0]!.element));
    expect(chips(wrapper)[0]!.attributes('data-filter')).toBe('lang');
  });

  it('focuses the previous chip after removing the last one', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage(ROUTE, true);
    await chips(wrapper)[2]!.trigger('click');
    await expectQuery({ role: 'backend', lang: 'vi' });
    await vi.waitFor(() => expect(document.activeElement)
      .toBe(chips(wrapper)[1]!.element));
  });

  it('focuses the Filters button below 1024 px after the last chip',
    async () => {
      stubListing(page([item('ada-lovelace')]));
      const wrapper = await mountPage('/showcase?lang=en', true);
      await chips(wrapper)[0]!.trigger('click');
      await expectQuery({});
      await vi.waitFor(() => expect(document.activeElement).toBe(
        wrapper.get('[data-action="showcase-filters-open"]').element,
      ));
      expect(wrapper.find('[data-testid="showcase-active-filters"]').exists())
        .toBe(false);
    });

  it('focuses the rail\'s pressed Role option from 1024 px', async () => {
    viewport = stubViewport(true);
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase?lang=en', true);
    await chips(wrapper)[0]!.trigger('click');
    await expectQuery({});
    await vi.waitFor(() => expect(document.activeElement).toBe(
      wrapper.get('[data-testid="showcase-filter-rail"] [data-role="all"]')
        .element,
    ));
  });

  it('removes a role chip and focuses All roles from 1024 px', async () => {
    viewport = stubViewport(true);
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase?role=qa', true);
    await chips(wrapper)[0]!.trigger('click');
    await expectQuery({});
    await vi.waitFor(() => {
      const all = wrapper.get(
        '[data-testid="showcase-filter-rail"] [data-role="all"]',
      );
      expect(all.attributes('aria-pressed')).toBe('true');
      expect(document.activeElement).toBe(all.element);
    });
  });

  it('clears every filter with Clear all and moves focus', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage(ROUTE, true);
    await wrapper.get('[data-action="showcase-filters-clear-all"]')
      .trigger('click');
    await expectQuery({});
    await vi.waitFor(() => expect(document.activeElement).toBe(
      wrapper.get('[data-action="showcase-filters-open"]').element,
    ));
    expect(wrapper.find('[data-testid="showcase-active-filters"]').exists())
      .toBe(false);
  });
});

describe('showcase report link', () => {
  it('shows the Report tooltip on focus', async () => {
    stubListing(page([item('ada-lovelace')]));
    const wrapper = await mountPage('/showcase', true);
    const report = wrapper.get('[data-action="showcase-report"]');
    expect(report.element.closest('[data-showcase-tile]')).toBeNull();
    (report.element as HTMLElement).focus();
    await vi.waitFor(() => expect(
      document.body.querySelector('[data-slot="tooltip-content"]')
        ?.textContent,
    ).toContain('Report'));
  });

  it('puts the tile link and Report in order as separate tab stops',
    async () => {
      stubListing(page([item('ada-lovelace')]));
      const wrapper = await mountPage();
      const stops = wrapper.get('[data-showcase-slug]')
        .findAll('a[href]').map((link) => (
          link.attributes('data-showcase-tile') !== undefined
            ? 'tile'
            : link.attributes('data-action')
        ));
      expect(stops).toEqual(['tile', 'showcase-report']);
    });
});

describe('showcase invite card', () => {
  async function listWith(route = '/showcase') {
    stubListing(page([item('ada-lovelace'), item('grace-hopper')]));
    return mountPage(route);
  }

  it('ends the grid in the list state and is not a tile', async () => {
    const wrapper = await listWith();
    const grid = wrapper.get('[data-state="list"]');
    const invite = grid.get('[data-testid="showcase-invite"]');
    expect(grid.element.lastElementChild).toBe(invite.element);
    expect(invite.element.tagName).toBe('LI');
    expect(invite.attributes('data-showcase-slug')).toBeUndefined();
    expect(invite.find('[data-showcase-tile]').exists()).toBe(false);
    expect(wrapper.findAll('[data-showcase-tile]')).toHaveLength(2);
    expect(invite.get('h2').text()).toBe('Want your resume here?');
    expect(invite.get('p').text()).toBe(
      'When you publish, turn on Show in the community showcase. You can '
      + 'turn it off any time.',
    );
  });

  it('shows on every page of the list', async () => {
    stubListing(page(
      [item('ada-lovelace')],
      { page: 2, pageCount: 3, total: 30 },
    ));
    const wrapper = await mountPage('/showcase?page=2');
    expect(wrapper.find('[data-testid="showcase-invite"]').exists())
      .toBe(true);
  });

  it('links signed-out visitors to registration', async () => {
    const wrapper = await listWith();
    const button = wrapper.get('[data-action="showcase-invite-create"]');
    expect(button.attributes('href')).toBe('/register');
    expect(button.text()).toBe('Create a free resume');
  });

  it('links signed-out visitors to sign-in when registration is closed',
    async () => {
      registerCapabilities({
        providerLogin: false,
        agentAccess: false,
        passwordRegistration: false,
      });
      try {
        const wrapper = await listWith();
        await vi.waitFor(() => expect(
          wrapper.get('[data-action="showcase-invite-create"]')
            .attributes('href'),
        ).toBe('/login'));
      } finally {
        registerCapabilities({ providerLogin: false, agentAccess: false });
      }
    });

  it('links a signed-in visitor to their resumes, with the same label',
    async () => {
      meStatus = 200;
      const wrapper = await listWith();
      await vi.waitFor(() => {
        const button = wrapper.get('[data-action="showcase-invite-create"]');
        expect(button.attributes('href')).toBe('/app/resumes');
        expect(button.text()).toBe('Create a free resume');
      });
    });

  it('speaks Vietnamese', async () => {
    setSiteLocale('vi');
    const wrapper = await listWith();
    const invite = wrapper.get('[data-testid="showcase-invite"]');
    expect(invite.get('h2').text()).toBe('Muốn CV của bạn ở đây?');
    expect(invite.get('a').text()).toBe('Tạo CV miễn phí');
  });

  const OTHER_STATES: [string, Reply[], string][] = [
    ['loading', [{ hold: true }], '/showcase'],
    ['empty', [page([], { pageCount: 0 })], '/showcase'],
    ['no-match', [page([], { pageCount: 0 })], '/showcase?role=qa'],
    ['failed', [{ fail: true }], '/showcase'],
  ];

  it.each(OTHER_STATES)('is absent in the %s state', async (
    _name,
    replies,
    route,
  ) => {
    stubListing(...replies);
    const wrapper = await mountPage(route);
    expect(wrapper.find('[data-testid="showcase-invite"]').exists())
      .toBe(false);
  });
});

describe('showcase page frame', () => {
  it('pads the root scroller below the sticky bar while mounted', async () => {
    stubListing(page([item('ada-lovelace')]));
    const app = await mountSuspended(AppRoot, { route: '/showcase' });
    mounted.push(app);
    await flushPromises();
    await vi.waitFor(() => expect(document.documentElement.classList
      .contains('showcase-scroll-padding')).toBe(true));
    app.unmount();
    await vi.waitFor(() => expect(document.documentElement.classList
      .contains('showcase-scroll-padding')).toBe(false));
  });

  it('sets the 68 px scroll padding only below 1024 px', () => {
    const source = readFileSync(
      join(process.cwd(), 'app/pages/showcase.vue'),
      'utf8',
    );
    const rule = new RegExp([
      '@media \\(width < 1024px\\) \\{\\s+',
      'html\\.showcase-scroll-padding \\{\\s+',
      'scroll-padding-top: 68px;',
    ].join(''), 'u');
    expect(source).toMatch(rule);
  });
});
