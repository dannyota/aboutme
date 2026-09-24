import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import AppLogo from '../../app/components/app/AppLogo.vue';

/**
 * Reads the accessible name a role="img" svg exposes, however the mark
 * supplies it (an `aria-label`, an `aria-labelledby` reference, or a nested
 * `<title>`), per the AppLogo contract in ADR 0050.
 */
function accessibleName(svg: Element): string | null {
  const label = svg.getAttribute('aria-label');
  if (label !== null) return label;
  const labelledby = svg.getAttribute('aria-labelledby');
  if (labelledby !== null) {
    const referenced = svg.ownerDocument.getElementById(labelledby);
    return referenced?.textContent?.trim() ?? null;
  }
  const title = svg.querySelector(':scope > title');
  return title?.textContent?.trim() ?? null;
}

function svgOf(wrapper: ReturnType<typeof mount>): Element {
  // The component root is the svg itself; querySelector never matches it.
  const root = wrapper.element as Element;
  const svg = root.tagName.toLowerCase() === 'svg'
    ? root
    : root.querySelector('svg');
  if (svg === null) throw new Error('AppLogo did not render an svg root.');
  return svg;
}

describe('AppLogo', () => {
  it('exposes an accessible name of aboutme for the full mark', () => {
    const wrapper = mount(AppLogo);
    const svg = svgOf(wrapper);

    expect(svg.getAttribute('role')).toBe('img');
    expect(accessibleName(svg)).toBe('aboutme');
  });

  it('exposes an accessible name of aboutme for the mark-only variant', () => {
    const wrapper = mount(AppLogo, { props: { markOnly: true } });
    const svg = svgOf(wrapper);

    expect(svg.getAttribute('role')).toBe('img');
    expect(accessibleName(svg)).toBe('aboutme');
  });

  it('renders no wordmark group when mark-only', () => {
    const full = svgOf(mount(AppLogo));
    const markOnly = svgOf(mount(AppLogo, { props: { markOnly: true } }));

    expect(full.querySelector('[data-logo-part="mark"]')).not.toBeNull();
    expect(full.querySelector('[data-logo-part="wordmark"]')).not.toBeNull();
    expect(markOnly.querySelector('[data-logo-part="mark"]')).not.toBeNull();
    expect(markOnly.querySelector('[data-logo-part="wordmark"]'))
      .toBeNull();
  });

  it('gives two instances on one page disjoint gradient ids that each '
    + 'resolve inside their own svg', () => {
    const wrapper = mount({
      components: { AppLogo },
      template: '<div><AppLogo /><AppLogo /></div>',
    });
    const svgs = wrapper.element.querySelectorAll('svg');
    expect(svgs).toHaveLength(2);

    const idSets = Array.from(svgs).map((svg) =>
      new Set(Array.from(svg.querySelectorAll('[id]'))
        .map((element) => element.id)));
    expect(idSets[0].size).toBeGreaterThan(0);
    for (const id of idSets[0]) expect(idSets[1].has(id)).toBe(false);

    for (const svg of Array.from(svgs)) {
      const ids = new Set(Array.from(svg.querySelectorAll('[id]'))
        .map((element) => element.id));
      const referenced = Array.from(svg.querySelectorAll('[fill]'))
        .map((element) => element.getAttribute('fill'))
        .filter((fill): fill is string => fill !== null
          && fill.startsWith('url(#'));
      expect(referenced.length).toBeGreaterThan(0);
      for (const fill of referenced) {
        const id = fill.slice('url(#'.length, -1);
        expect(ids.has(id)).toBe(true);
      }
    }
  });

  it('sets no style attribute or style element anywhere in the mark', () => {
    const svg = svgOf(mount(AppLogo));
    expect(svg.querySelector('style')).toBeNull();
    expect(svg.hasAttribute('style')).toBe(false);
    for (const element of Array.from(svg.querySelectorAll('*'))) {
      expect(element.hasAttribute('style')).toBe(false);
    }
  });

  it('gives each size a distinct height class or attribute', () => {
    const sizes = ['sm', 'md', 'lg'] as const;
    const markers = sizes.map((size) => {
      const svg = svgOf(mount(AppLogo, { props: { size } }));
      return `${svg.getAttribute('class') ?? ''}|${
        svg.getAttribute('data-logo-size') ?? ''}|${
        svg.getAttribute('height') ?? ''}`;
    });
    expect(new Set(markers).size).toBe(sizes.length);
  });

  it('defaults to the md size', () => {
    const svg = svgOf(mount(AppLogo));
    const defaultMarker = `${svg.getAttribute('class') ?? ''}|${
      svg.getAttribute('data-logo-size') ?? ''}|${
      svg.getAttribute('height') ?? ''}`;
    const mdSvg = svgOf(mount(AppLogo, { props: { size: 'md' } }));
    const mdMarker = `${mdSvg.getAttribute('class') ?? ''}|${
      mdSvg.getAttribute('data-logo-size') ?? ''}|${
      mdSvg.getAttribute('height') ?? ''}`;
    expect(defaultMarker).toBe(mdMarker);
  });
});
