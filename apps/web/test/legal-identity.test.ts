import { mountSuspended } from '@nuxt/test-utils/runtime';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import PrivacyPage from '../app/pages/privacy.vue';
import TermsPage from '../app/pages/terms.vue';
import { setSiteLocale } from './support/locale';
import { RETIRED_STYLE, webSource } from './support/sfcStyles';

type Wrapper = Awaited<ReturnType<typeof mountSuspended>>;

const mounted: Wrapper[] = [];

beforeEach(() => {
  setSiteLocale(undefined);
  clearNuxtData();
});

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

async function mountLegal(
  page: Parameters<typeof mountSuspended>[0],
): Promise<Wrapper> {
  const wrapper = await mountSuspended(page);
  mounted.push(wrapper);
  return wrapper;
}

function expectClasses(classes: readonly string[], names: readonly string[]) {
  for (const name of names) expect(classes, name).toContain(name);
}

describe.each([
  ['terms', TermsPage, 'terms-page'],
  ['privacy', PrivacyPage, 'privacy-page'],
] as const)('%s page identity', (_name, page, testid) => {
  it('holds the text to a 44 rem measure on the plain ground', async () => {
    const wrapper = await mountLegal(page);
    const main = wrapper.get(`[data-testid="${testid}"]`);
    expectClasses(main.classes(), ['mx-auto', 'box-content', 'max-w-[44rem]']);
    expect(main.classes().join(' ')).not.toMatch(
      /max-w-\[42rem\]|(?:^|\s)bg-|shadow-|rounded-/u,
    );
  });

  it('sets the h1 at 32/1.3 and section headings at 24/1.35', async () => {
    const wrapper = await mountLegal(page);
    const title = wrapper.get('h1');
    expectClasses(title.classes(), [
      'text-2xl', 'font-extrabold', 'tracking-[-0.02em]',
    ]);
    expect(title.classes()).not.toContain('font-semibold');
    const headings = wrapper.findAll('h2');
    expect(headings.length).toBeGreaterThan(3);
    for (const heading of headings) {
      expectClasses(heading.classes(), [
        'text-xl', 'font-extrabold', 'tracking-[-0.01em]',
      ]);
      expect(heading.classes()).not.toContain('text-lg');
    }
  });

  it('sets body text at 16/1.6', async () => {
    const wrapper = await mountLegal(page);
    const body = wrapper.findAll('main > p:not([data-testid="legal-updated"]), '
      + 'section > p, section > ul');
    expect(body.length).toBeGreaterThan(5);
    for (const block of body) {
      expect(block.classes(), block.text().slice(0, 40)).toContain('text-md');
      expect(block.classes().join(' ')).not.toMatch(
        /(?:^|\s)(?:text-base|leading-7)(?:\s|$)/u,
      );
    }
  });

  it('underlines every link in the link color, 1 px at a 4 px offset',
    async () => {
      const wrapper = await mountLegal(page);
      const links = wrapper.findAll('a');
      expect(links.length).toBeGreaterThan(0);
      for (const link of links) {
        expectClasses(link.classes(), [
          'text-link', 'underline', 'decoration-1', 'underline-offset-4',
        ]);
      }
    });
});

describe('legal sources', () => {
  it('keep hex and retired tokens out of the legal markup', () => {
    for (const path of [
      'app/components/legal/LegalDocument.vue',
      'app/pages/terms.vue',
      'app/pages/privacy.vue',
    ]) {
      expect(webSource(path), path).not.toMatch(RETIRED_STYLE);
    }
  });
});
