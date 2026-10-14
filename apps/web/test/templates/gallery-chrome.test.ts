import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { isNavigationFailure, NavigationFailureType } from 'vue-router';

import GalleryPage from '../../app/pages/templates/index.vue';
import {
  FILTERS,
  type GalleryFilter,
  GALLERY,
} from '../../app/templates/catalog';
import { setSiteLocale } from '../support/locale';

// The gallery header and filter chips (DESIGN.md, template gallery). The
// dot is decorative: the chip text carries the meaning it names.

function stubViewport() {
  vi.stubGlobal('matchMedia', (query: string) => ({
    media: query,
    matches: false,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => true,
  }));
}

const mounted: { unmount(): void }[] = [];
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

async function mountGallery(route = '/templates') {
  const wrapper = await mountSuspended(GalleryPage, {
    route,
    attachTo: document.body,
  });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

const FORMAT_FILTERS = new Set<GalleryFilter>([
  'sample',
  'ats',
  'one-page',
  'photo',
]);

describe('template gallery chrome', () => {
  beforeEach(() => {
    setSiteLocale('en');
    stubViewport();
  });

  it('wraps the title and lead in a gallery-header', async () => {
    const wrapper = await mountGallery();
    const header = wrapper.get('[data-testid="gallery-header"]');
    expect(header.get('h1').text()).not.toBe('');
    expect(header.get('p').text()).not.toBe('');
  });

  it('gives every chip but All a dot, colored by its group', async () => {
    const wrapper = await mountGallery();

    const all = wrapper.get('[data-filter="all"]');
    expect(all.attributes('data-group')).toBeUndefined();
    expect(all.find('.gallery-chip__dot').exists()).toBe(false);

    for (const filter of FILTERS) {
      const chip = wrapper.get(`[data-filter="${filter}"]`);
      expect(chip.attributes('data-group')).toBe(
        FORMAT_FILTERS.has(filter) ? 'format' : 'audience',
      );
      expect(chip.find('.gallery-chip__dot').exists()).toBe(true);
    }
  });

  it('marks the filtered chip as the current page', async () => {
    const wrapper = await mountGallery('/templates?filter=ats');
    await flushPromises();

    expect(wrapper.get('[data-filter="ats"]').attributes('aria-current')).toBe(
      'page',
    );
    expect(
      wrapper.get('[data-filter="all"]').attributes('aria-current'),
    ).toBeUndefined();
  });

  it('carries a tag kind on every card, both kinds present', async () => {
    const wrapper = await mountGallery();
    await flushPromises();

    const tags = wrapper.findAll('[data-template-tag]');
    const kinds = tags.map((tag) => tag.attributes('data-tag-kind'));
    expect(kinds).toContain('sample');
    expect(kinds).toContain('illustrative');
    for (const kind of kinds) {
      expect(['sample', 'illustrative']).toContain(kind);
    }
  });

  it('presses the backend chip and hides every other card', async () => {
    const wrapper = await mountGallery('/templates?role=backend');
    await flushPromises();

    expect(
      wrapper.get('[data-role="backend"]').attributes('aria-pressed'),
    ).toBe('true');
    expect(wrapper.get('[data-role="all"]').attributes('aria-pressed')).toBe(
      'false',
    );

    const cards = wrapper.findAll('[data-template]');
    expect(cards).toHaveLength(GALLERY.length);
    for (const card of cards) {
      const isBackend = card.attributes('data-template') === 'one-page-tight';
      expect(card.element.closest('li')?.hasAttribute('hidden')).toBe(
        !isBackend,
      );
    }
  });

  it('treats an unknown role as all roles: nothing hidden', async () => {
    const wrapper = await mountGallery('/templates?role=nope');
    await flushPromises();

    expect(wrapper.get('[data-role="all"]').attributes('aria-pressed')).toBe(
      'true',
    );
    for (const card of wrapper.findAll('[data-template]')) {
      expect(card.element.closest('li')?.hasAttribute('hidden')).toBe(false);
    }
  });

  it('names the role group with the accessible label', async () => {
    const wrapper = await mountGallery();
    const group = wrapper.get('[data-testid="gallery-roles"]');
    expect(group.attributes('aria-label')).toBe('Filter by role');
  });

  it('combines the role chip with the filter chip in the URL', async () => {
    const wrapper = await mountGallery('/templates?filter=ats&role=frontend');
    await flushPromises();

    // Navigation settles asynchronously, so assert the replace call, then
    // wait for the route and the pressed chip to follow.
    const router = useRouter();
    const replace = vi.spyOn(router, 'replace');
    const chip = (role: string) => wrapper.get(`[data-role="${role}"]`);

    await chip('mobile').trigger('click');
    expect(replace).toHaveBeenLastCalledWith({
      path: '/templates',
      query: { filter: 'ats', role: 'mobile' },
    });
    await vi.waitFor(() => {
      expect(router.currentRoute.value.query).toEqual({
        filter: 'ats',
        role: 'mobile',
      });
      expect(chip('mobile').attributes('aria-pressed')).toBe('true');
    });

    replace.mockClear();
    await chip('mobile').trigger('click');
    await flushPromises();
    expect(replace).not.toHaveBeenCalled();
    expect(chip('mobile').attributes('aria-pressed')).toBe('true');

    await chip('all').trigger('click');
    expect(replace).toHaveBeenLastCalledWith({
      path: '/templates',
      query: { filter: 'ats' },
    });
    await vi.waitFor(() => {
      expect(router.currentRoute.value.query).toEqual({ filter: 'ats' });
    });
    replace.mockRestore();
  });
});

describe('Library filter navigation', () => {
  beforeEach(() => {
    setSiteLocale('en');
    stubViewport();
  });

  it('keeps Style links, real encoded hrefs and committed count', async () => {
    const wrapper = await mountGallery(
      '/templates?role=backend&source=a%20%26%20b',
    );
    const link = wrapper.get(
      '[data-testid="gallery-styles"] [data-filter="technical"]',
    );
    expect(link.element.tagName).toBe('A');
    expect(link.attributes('aria-pressed')).toBeUndefined();
    expect(link.attributes('href')).toContain('role=backend');
    expect(link.attributes('href')).toContain('filter=technical');
    expect(link.attributes('href')).toContain('source=a');
    expect(wrapper.get('[data-testid="gallery-count"]').text()).toBe(
      '1 template',
    );
    expect(wrapper.get('[data-testid="gallery-filters-badge"]').text()).toBe(
      '1',
    );
    expect(wrapper.find('[data-testid="gallery-filter-sheet"]').exists()).toBe(
      false,
    );
    const replace = vi.spyOn(useRouter(), 'replace');
    await link.trigger('click', { ctrlKey: true });
    expect(replace).not.toHaveBeenCalled();
  });

  it('announces same counts only after latest success', async () => {
    const wrapper = await mountGallery('/templates?role=backend');
    const router = useRouter();
    const before = wrapper.get('[data-testid="gallery-count"] span').element;
    let finish!: () => void;
    const replace = vi.spyOn(router, 'replace').mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    await wrapper.get('[data-role="frontend"]').trigger('click');
    expect(
      wrapper.get('[data-role="backend"]').attributes('aria-pressed'),
    ).toBe('true');
    expect(wrapper.get('[data-testid="gallery-count"] span').element).toBe(
      before,
    );
    finish();
    await flushPromises();
    expect(
      wrapper.get('[data-role="frontend"]').attributes('aria-pressed'),
    ).toBe('true');
    expect(wrapper.get('[data-testid="gallery-count"] span').element).not.toBe(
      before,
    );
    replace.mockRestore();
  });

  it('keeps state on rejected and cancelled navigation', async () => {
    const wrapper = await mountGallery('/templates?role=backend');
    const before = wrapper.get('[data-testid="gallery-count"] span').element;
    const replace = vi
      .spyOn(useRouter(), 'replace')
      .mockRejectedValueOnce(new Error('cancelled'));
    await wrapper.get('[data-role="frontend"]').trigger('click');
    await flushPromises();
    expect(
      wrapper.get('[data-role="backend"]').attributes('aria-pressed'),
    ).toBe('true');
    expect(wrapper.get('[data-testid="gallery-count"] span').element).toBe(
      before,
    );
    replace.mockResolvedValueOnce({ type: 8 } as never);
    await wrapper.get('[data-role="mobile"]').trigger('click');
    await flushPromises();
    expect(
      wrapper.get('[data-role="backend"]').attributes('aria-pressed'),
    ).toBe('true');
  });

  it('combines rapid changes and ignores a superseded completion', async () => {
    const wrapper = await mountGallery();
    let finish!: () => void;
    const replace = vi
      .spyOn(useRouter(), 'replace')
      .mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            finish = resolve;
          }),
      )
      .mockResolvedValueOnce(undefined);
    await wrapper.get('[data-role="backend"]').trigger('click');
    await wrapper
      .get('[data-testid="gallery-styles"] [data-filter="technical"]')
      .trigger('click');
    expect(replace).toHaveBeenLastCalledWith({
      path: '/templates',
      query: { role: 'backend', filter: 'technical' },
    });
    await flushPromises();
    const node = wrapper.get('[data-testid="gallery-count"] span').element;
    finish();
    await flushPromises();
    expect(wrapper.get('[data-testid="gallery-count"] span').element).toBe(
      node,
    );
    expect(
      wrapper.get('[data-filter="technical"]').attributes('aria-current'),
    ).toBe('page');
  });

  it('commits Back to the pending target before delayed navigation cancels',
    async () => {
      const wrapper = await mountGallery('/templates?role=frontend');
      const router = useRouter();
      await router.push('/templates');
      await flushPromises();
      const before = wrapper.get('[data-testid="gallery-count"] span').element;
      let release!: () => void;
      let entered!: () => void;
      const waiting = new Promise<void>((resolve) => {
        entered = resolve;
      });
      const delayed = new Promise<void>((resolve) => {
        release = resolve;
      });
      let delay = true;
      const removeGuard = router.beforeEach(async (to) => {
        if (to.fullPath !== '/templates?role=frontend' || !delay) return;
        delay = false;
        entered();
        await delayed;
      });
      const replace = vi.spyOn(router, 'replace');
      try {
        await wrapper.get('[data-role="frontend"]').trigger('click');
        await waiting;
        expect(wrapper.get('[data-role="all"]').attributes('aria-pressed'))
          .toBe('true');
        router.back();
        await vi.waitFor(() => {
          expect(router.currentRoute.value.fullPath)
            .toBe('/templates?role=frontend');
        });
        await flushPromises();
        expect(wrapper.get('[data-role="frontend"]').attributes('aria-pressed'))
          .toBe('true');
        expect(wrapper.get('[data-testid="gallery-count"]').text())
          .toBe('1 template');
        expect(wrapper.get('[data-testid="gallery-filters-badge"]').text())
          .toBe('1');
        const visible = wrapper.findAll('[data-template]').filter((card) =>
          !card.element.closest('li')?.hasAttribute('hidden'));
        expect(visible.map((card) => card.attributes('data-template')))
          .toEqual(['engineer-compact']);
        const announced = wrapper.get('[data-testid="gallery-count"] span')
          .element;
        expect(announced).not.toBe(before);
        release();
        const failure = await replace.mock.results[0]!.value;
        expect(isNavigationFailure(failure, NavigationFailureType.cancelled))
          .toBe(true);
        await flushPromises();
        expect(wrapper.get('[data-testid="gallery-count"] span').element)
          .toBe(announced);
        expect(wrapper.get('[data-role="frontend"]').attributes('aria-pressed'))
          .toBe('true');
      } finally {
        release();
        removeGuard();
        await replace.mock.results[0]?.value;
        await flushPromises();
      }
    });

  it('does not reannounce an unfinished reverted change', async () => {
    const wrapper = await mountGallery('/templates?role=backend');
    const before = wrapper.get('[data-testid="gallery-count"] span').element;
    let finish!: () => void;
    vi.spyOn(useRouter(), 'replace')
      .mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            finish = resolve;
          }),
      )
      .mockResolvedValueOnce(undefined);
    await wrapper.get('[data-role="frontend"]').trigger('click');
    await wrapper.get('[data-role="backend"]').trigger('click');
    finish();
    await flushPromises();
    expect(wrapper.get('[data-testid="gallery-count"] span').element).toBe(
      before,
    );
  });

  it.each(['en', 'vi'] as const)(
    'uses bilingual zero and count copy in %s',
    async (locale) => {
      setSiteLocale(locale);
      const wrapper = await mountGallery('/templates?role=backend&filter=ats');
      expect(wrapper.get('[data-testid="gallery-count"]').text()).toBe(
        locale === 'en' ? '0 templates' : '0 mẫu',
      );
      expect(wrapper.text()).toContain(
        locale === 'en'
          ? 'No template matches this filter.'
          : 'Không có mẫu nào khớp với bộ lọc này.',
      );
      expect(wrapper.findAll('[role="status"]')).toHaveLength(1);
    },
  );
});

describe('Library sheet and focus', () => {
  beforeEach(() => {
    setSiteLocale('en');
    stubViewport();
  });

  it('mirrors count in a sheet with unique heading IDs', async () => {
    const wrapper = await mountGallery('/templates?role=backend');
    await wrapper.get('[data-action="gallery-filters-open"]').trigger('click');
    await flushPromises();
    const sheet = document.querySelector<HTMLElement>(
      '[data-testid="gallery-filter-sheet"]',
    )!;
    expect(sheet).not.toBeNull();
    expect(
      sheet.querySelector('[data-testid="gallery-sheet-status"]')?.textContent,
    ).toBe('1 template');
    expect(
      sheet.querySelector('[data-action="gallery-filters-apply"]')?.textContent,
    ).toContain('Show 1 template');
    const ids = [...document.querySelectorAll('[id]')].map((node) => node.id);
    expect(new Set(ids).size).toBe(ids.length);
    const link = sheet.querySelector<HTMLAnchorElement>(
      '[data-filter="technical"]',
    )!;
    const pageNode = wrapper.get('[data-testid="gallery-count"] span').element;
    const sheetNode = sheet.querySelector(
      '[data-testid="gallery-sheet-status"] span',
    );
    const replace = vi.spyOn(useRouter(), 'replace');
    link.focus();
    link.click();
    expect(replace).toHaveBeenLastCalledWith({
      path: '/templates',
      query: { role: 'backend', filter: 'technical' },
    });
    await replace.mock.results[0]!.value;
    await flushPromises();
    expect(document.activeElement).toBe(link);
    expect(document.querySelector('[data-testid="gallery-filter-sheet"]')).toBe(
      sheet,
    );
    expect(link.getAttribute('aria-current')).toBe('page');
    expect(wrapper.get('[data-testid="gallery-count"] span').element)
      .not.toBe(pageNode);
    expect(sheet.querySelector('[data-testid="gallery-sheet-status"] span'))
      .not.toBe(sheetNode);
    const clear = sheet.querySelector<HTMLButtonElement>(
      '[data-action="gallery-filters-clear"]',
    )!;
    clear.focus();
    clear.click();
    expect(replace).toHaveBeenLastCalledWith({
      path: '/templates',
      query: {},
    });
    await replace.mock.results[1]!.value;
    await flushPromises();
    expect(document.activeElement).toBe(clear);
    expect(
      sheet.querySelector('[data-testid="gallery-sheet-status"]')?.textContent,
    ).toBe('20 templates');
  });

  it.each(['remove', 'clear'] as const)(
    'restores failed %s focus only before later focus activity',
    async (action) => {
      const wrapper = await mountGallery(
        '/templates?role=backend&filter=technical',
      );
      const control = wrapper.get(
        action === 'remove'
          ? '[data-action="gallery-filter-remove"][data-filter="role"]'
          : '[data-action="gallery-filters-clear-all"]',
      ).element as HTMLElement;
      vi.spyOn(control, 'getClientRects').mockReturnValue([
        {},
      ] as unknown as DOMRectList);
      let reject!: (error: Error) => void;
      vi.spyOn(useRouter(), 'replace').mockImplementationOnce(
        () =>
          new Promise<void>((_resolve, fail) => {
            reject = fail;
          }),
      );
      control.focus();
      control.click();
      await flushPromises();
      expect(document.activeElement).not.toBe(control);
      reject(new Error('cancelled'));
      await flushPromises();
      expect(document.activeElement).toBe(control);
      let failAgain!: (error: Error) => void;
      vi.spyOn(useRouter(), 'replace').mockImplementationOnce(
        () =>
          new Promise<void>((_resolve, fail) => {
            failAgain = fail;
          }),
      );
      control.click();
      await flushPromises();
      const card = wrapper.get('[data-template]').element as HTMLElement;
      card.focus();
      failAgain(new Error('cancelled'));
      await flushPromises();
      expect(document.activeElement).toBe(card);
    },
  );

  it('does not announce unrelated query changes or a no-op clear', async () => {
    const wrapper = await mountGallery();
    const before = wrapper.get('[data-testid="gallery-count"] span').element;
    await useRouter().replace('/templates?source=landing');
    await flushPromises();
    expect(wrapper.get('[data-testid="gallery-count"] span').element).toBe(
      before,
    );
    const replace = vi.spyOn(useRouter(), 'replace');
    await wrapper.get('[data-role="all"]').trigger('click');
    expect(replace).not.toHaveBeenCalled();
  });
});

it('lets incoming URLs supersede pending navigation', async () => {
  setSiteLocale('en');
  stubViewport();
  const wrapper = await mountGallery('/templates?role=backend');
  const router = useRouter();
  let finish!: () => void;
  vi.spyOn(router, 'replace').mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  await wrapper.get('[data-role="frontend"]').trigger('click');
  await router.push('/templates?role=qa&filter=ats');
  await flushPromises();
  expect(wrapper.get('[data-role="qa"]').attributes('aria-pressed')).toBe(
    'true',
  );
  const before = wrapper.get('[data-testid="gallery-count"] span').element;
  finish();
  await flushPromises();
  expect(wrapper.get('[data-testid="gallery-count"] span').element).toBe(
    before,
  );
  expect(wrapper.get('[data-role="qa"]').attributes('aria-pressed')).toBe(
    'true',
  );
});

it('normalizes hostile values without rewriting the URL', async () => {
  setSiteLocale('en');
  stubViewport();
  const wrapper = await mountGallery(
    '/templates?role=qa&role=backend&filter=%3Cscript%3E&source=landing',
  );
  expect(wrapper.get('[data-role="all"]').attributes('aria-pressed')).toBe(
    'true',
  );
  expect(wrapper.get('[data-filter="all"]').attributes('aria-current')).toBe(
    'page',
  );
  expect(wrapper.find('[data-testid="gallery-active-filters"]').exists()).toBe(
    false,
  );
  expect(useRouter().currentRoute.value.query.role).toEqual(['qa', 'backend']);
  const replace = vi.spyOn(useRouter(), 'replace');
  await wrapper.get('[data-filter="ats"]').trigger('click');
  expect(replace).toHaveBeenLastCalledWith({
    path: '/templates',
    query: { source: 'landing', filter: 'ats' },
  });
  await replace.mock.results[0]!.value;
  await flushPromises();
  expect(useRouter().currentRoute.value.query).toEqual({
    source: 'landing',
    filter: 'ats',
  });
});

it('updates locale copy without replacing the announcement node', async () => {
  setSiteLocale('en');
  stubViewport();
  const wrapper = await mountGallery('/templates?role=backend');
  const before = wrapper.get('[data-testid="gallery-count"] span').element;
  useLocale().setLocale('vi');
  await flushPromises();
  expect(wrapper.get('[data-testid="gallery-count"]').text()).toBe('1 mẫu');
  expect(wrapper.get('[data-testid="gallery-count"] span').element)
    .toBe(before);
});
