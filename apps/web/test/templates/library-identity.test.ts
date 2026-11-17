import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import TemplateCard from '../../app/components/templates/TemplateCard.vue';
import { galleryCopy } from '../../app/i18n/templates';
import GalleryPage from '../../app/pages/templates/index.vue';
import { galleryTemplate } from '../../app/templates/catalog';
import { setSiteLocale } from '../support/locale';
import {
  declared, RETIRED_STYLE, sfcRules, webSource,
} from '../support/sfcStyles';

const FILTERS = 'app/components/templates/LibraryFilters.vue';
const CHIP = '.library-filters :deep(.gallery-chip)';

const mounted: { unmount(): void }[] = [];
afterEach(() => {
  vi.unstubAllGlobals();
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

function stubNarrowViewport(): void {
  vi.stubGlobal('matchMedia', (media: string) => ({
    media,
    matches: false,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => true,
  }));
}

async function mountGallery(route = '/templates') {
  const wrapper = await mountSuspended(GalleryPage, { route });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

describe('Library header', () => {
  beforeEach(() => {
    setSiteLocale('en');
    stubNarrowViewport();
  });

  it('sits on a blush panel with the panel radius and no border', async () => {
    const wrapper = await mountGallery();
    const header = wrapper.get('[data-testid="gallery-header"]');
    for (const name of [
      'relative', 'rounded-[var(--radius-panel)]', 'bg-surface-blush',
    ]) {
      expect(header.classes(), name).toContain(name);
    }
    expect(header.classes().join(' ')).not.toMatch(
      /(?:^|\s)(?:border|border-border|bg-surface-blue)(?:\s|$)/u,
    );
    expect(header.classes().filter((name) => /overflow/u.test(name)))
      .toEqual([]);
  });

  it('sets the h1 at 32 px, 40 px from 48rem, weight 800', async () => {
    const wrapper = await mountGallery();
    const title = wrapper.get('[data-testid="gallery-header"] h1');
    expect(title.text()).toBe('Library');
    for (const name of [
      'text-2xl', 'md:text-3xl', 'font-extrabold', 'tracking-[-0.02em]',
    ]) {
      expect(title.classes(), name).toContain(name);
    }
    expect(title.classes().join(' ')).not.toMatch(/sm:text-5xl|font-bold/u);
  });

  it('clips the fan in its own layer, not in the header', async () => {
    const wrapper = await mountGallery();
    const header = wrapper.get('[data-testid="gallery-header"]');
    const layer = header.get('[data-testid="gallery-fan-layer"]');
    expect(layer.attributes('aria-hidden')).toBe('true');
    for (const name of [
      'pointer-events-none', 'absolute', 'inset-0', 'overflow-hidden',
      'rounded-[inherit]',
    ]) {
      expect(layer.classes(), name).toContain(name);
    }
    expect(layer.element.parentElement).toBe(header.element);
    expect(layer.element.previousElementSibling?.tagName).toBe('P');
    expect(layer.element.nextElementSibling).toBeNull();
    const fan = layer.get('[data-testid="gallery-fan"]');
    expect(fan.element.parentElement).toBe(layer.element);
    const title = header.get('h1').element;
    expect(title.closest('[data-testid="gallery-fan-layer"]')).toBeNull();
  });

  it('adds a decorative fan of three paper sheets from 64rem', async () => {
    const wrapper = await mountGallery();
    const header = wrapper.get('[data-testid="gallery-header"]');
    const fan = header.get('[data-testid="gallery-fan"]');
    expect(fan.text()).toBe('');
    for (const name of ['absolute', 'hidden', 'lg:flex']) {
      expect(fan.classes(), name).toContain(name);
    }
    expect(fan.classes().some((name) => /^-right-/u.test(name))).toBe(true);
    const sheets = fan.findAll('[data-fan-sheet]');
    expect(sheets).toHaveLength(3);
    for (const sheet of sheets) {
      for (const name of [
        'bg-paper', 'rounded-[var(--radius-sheet)]',
        'shadow-[var(--shadow-paper)]', 'aspect-[210/297]',
      ]) {
        expect(sheet.classes(), name).toContain(name);
      }
    }
    expect(header.findAll('a, button, [tabindex]')).toHaveLength(0);
  });

  it('keeps removed tokens, the main fill and hex out of its markup', () => {
    for (const path of [
      'app/pages/templates/index.vue',
      'app/components/templates/LibraryFan.vue',
      'app/components/templates/TemplateCard.vue',
      FILTERS,
    ]) {
      expect(webSource(path), path).not.toMatch(RETIRED_STYLE);
    }
  });
});

describe('Library chips', () => {
  const rules = () => sfcRules(FILTERS);

  it('draws idle chips transparent on the control edge', () => {
    expect(declared(rules(), CHIP)).toMatchObject({
      'border': '1px solid var(--control)',
      'background': 'transparent',
      'color': 'var(--foreground)',
      'font-weight': '500',
      'border-radius': '9999px',
      'height': '36px',
    });
    const hover = '.library-filters :deep(.gallery-chip'
      + ':not([aria-current=\'page\']):not([aria-pressed=\'true\']):hover)';
    expect(declared(rules(), hover)).toEqual({ background: 'var(--accent)' });
  });

  it('paints the selected chip as an ink pill at weight 600', () => {
    const ink = {
      'border-color': 'var(--foreground)',
      'background': 'var(--foreground)',
      'color': 'var(--background)',
      'font-weight': '600',
    };
    for (const state of [
      '[aria-current=\'page\']', '[aria-pressed=\'true\']',
    ]) {
      expect(declared(rules(), `.library-filters :deep(.gallery-chip${state})`),
        state).toEqual(ink);
    }
  });

  it('gives both style groups a muted dot that follows an ink pill', () => {
    for (const group of ['format', 'audience']) {
      expect(declared(rules(), `[data-group='${group}'] .gallery-chip__dot`))
        .toEqual({ background: 'var(--muted-foreground)' });
    }
    expect(declared(rules(), '[aria-current=\'page\'] .gallery-chip__dot'))
      .toEqual({ background: 'currentColor' });
  });
});

describe('Library card', () => {
  const copy = galleryCopy.en;

  async function mountCard(id: string) {
    const wrapper = await mountSuspended(TemplateCard, {
      props: {
        illustrative: copy.illustrative,
        locale: 'en',
        pageImageAlt: copy.pageImageAlt,
        template: galleryTemplate(id)!,
      },
    });
    mounted.push(wrapper);
    await flushPromises();
    return wrapper;
  }

  it('sets the name in bold card-title type', async () => {
    const wrapper = await mountCard('ats-plain');
    const name = wrapper.get('a > span > span:first-child');
    expect(name.text()).toBe('ATS Plain');
    for (const value of ['text-md', 'font-bold', 'text-foreground']) {
      expect(name.classes(), value).toContain(value);
    }
  });

  it('names the sample on a mist pill', async () => {
    const wrapper = await mountCard('ats-plain');
    const tag = wrapper.get('[data-template-tag]');
    expect(tag.attributes('data-tag-kind')).toBe('sample');
    for (const value of [
      'rounded-full', 'bg-surface-mist', 'text-foreground', 'text-xs',
      'font-semibold',
    ]) {
      expect(tag.classes(), value).toContain(value);
    }
    expect(tag.classes()).not.toContain('bg-surface-blue');
  });

  it('outlines the illustrative tag in the border color', async () => {
    const wrapper = await mountCard('classic-serif');
    const tag = wrapper.get('[data-template-tag]');
    expect(tag.attributes('data-tag-kind')).toBe('illustrative');
    expect(tag.text()).toBe('Illustrative content');
    for (const value of [
      'rounded-full', 'border', 'border-border', 'bg-transparent',
      'text-muted-foreground', 'text-xs', 'font-semibold',
    ]) {
      expect(tag.classes(), value).toContain(value);
    }
    expect(tag.classes()).not.toContain('bg-muted');
  });
});
