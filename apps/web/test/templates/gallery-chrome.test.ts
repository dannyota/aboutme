import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import GalleryPage from '../../app/pages/templates/index.vue';
import {
  FILTERS,
  type GalleryFilter,
  GALLERY,
} from '../../app/templates/catalog';
import { setSiteLocale } from '../support/locale';

// The gallery header and filter chips (DESIGN.md, template gallery). The
// dot is decorative: the chip text carries the meaning it names.

const FORMAT_FILTERS = new Set<GalleryFilter>([
  'sample',
  'ats',
  'one-page',
  'photo',
]);

describe('template gallery chrome', () => {
  beforeEach(() => setSiteLocale('en'));

  it('wraps the title and lead in a gallery-header', async () => {
    const wrapper = await mountSuspended(GalleryPage);
    const header = wrapper.get('[data-testid="gallery-header"]');
    expect(header.get('h1').text()).not.toBe('');
    expect(header.get('p').text()).not.toBe('');
  });

  it('gives every chip but All a dot, colored by its group', async () => {
    const wrapper = await mountSuspended(GalleryPage);

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
    const wrapper = await mountSuspended(GalleryPage, {
      route: '/templates?filter=ats',
    });
    await flushPromises();

    expect(wrapper.get('[data-filter="ats"]').attributes('aria-current'))
      .toBe('page');
    expect(wrapper.get('[data-filter="all"]').attributes('aria-current'))
      .toBeUndefined();
  });

  it('carries a tag kind on every card, both kinds present', async () => {
    const wrapper = await mountSuspended(GalleryPage);
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
    const wrapper = await mountSuspended(GalleryPage, {
      route: '/templates?role=backend',
    });
    await flushPromises();

    expect(wrapper.get('[data-role="backend"]').attributes('aria-pressed'))
      .toBe('true');
    expect(wrapper.get('[data-role="all"]').attributes('aria-pressed'))
      .toBe('false');

    const cards = wrapper.findAll('[data-template]');
    expect(cards).toHaveLength(GALLERY.length);
    for (const card of cards) {
      const isBackend = card.attributes('data-template') === 'one-page-tight';
      expect(card.element.closest('li')?.hasAttribute('hidden'))
        .toBe(!isBackend);
    }
  });

  it('treats an unknown role as all roles: nothing hidden', async () => {
    const wrapper = await mountSuspended(GalleryPage, {
      route: '/templates?role=nope',
    });
    await flushPromises();

    expect(wrapper.get('[data-role="all"]').attributes('aria-pressed'))
      .toBe('true');
    for (const card of wrapper.findAll('[data-template]')) {
      expect(card.element.closest('li')?.hasAttribute('hidden')).toBe(false);
    }
  });

  it('names the role group with the accessible label', async () => {
    const wrapper = await mountSuspended(GalleryPage);
    const group = wrapper.get('[data-testid="gallery-roles"]');
    expect(group.attributes('aria-label')).toBe('Filter by role');
  });

  it('combines the role chip with the filter chip in the URL', async () => {
    const wrapper = await mountSuspended(GalleryPage, {
      route: '/templates?filter=ats&role=frontend',
    });
    await flushPromises();

    // Navigation settles asynchronously, so assert the replace call, then
    // wait for the route and the pressed chip to follow.
    const router = useRouter();
    const replace = vi.spyOn(router, 'replace');
    const chip = (role: string) => wrapper.get(`[data-role="${role}"]`);

    await chip('mobile').trigger('click');
    expect(replace).toHaveBeenLastCalledWith({
      query: { filter: 'ats', role: 'mobile' },
    });
    await vi.waitFor(() => {
      expect(router.currentRoute.value.query)
        .toEqual({ filter: 'ats', role: 'mobile' });
      expect(chip('mobile').attributes('aria-pressed')).toBe('true');
    });

    replace.mockClear();
    await chip('mobile').trigger('click');
    await flushPromises();
    expect(replace).not.toHaveBeenCalled();
    expect(chip('mobile').attributes('aria-pressed')).toBe('true');

    await chip('all').trigger('click');
    expect(replace).toHaveBeenLastCalledWith({ query: { filter: 'ats' } });
    await vi.waitFor(() => {
      expect(router.currentRoute.value.query).toEqual({ filter: 'ats' });
    });
    replace.mockRestore();
  });
});
