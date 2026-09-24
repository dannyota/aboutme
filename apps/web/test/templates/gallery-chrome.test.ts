import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { beforeEach, describe, expect, it } from 'vitest';

import GalleryPage from '../../app/pages/templates/index.vue';
import { FILTERS, type GalleryFilter } from '../../app/templates/catalog';
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
});
