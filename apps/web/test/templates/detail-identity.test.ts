import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { beforeEach, describe, expect, it } from 'vitest';

import TemplateDetailPage from '../../app/pages/templates/[id].vue';
import { setSiteLocale } from '../support/locale';
import { declared, sfcRules, webSource } from '../support/sfcStyles';

const PAGE = 'app/pages/templates/[id].vue';

mockNuxtImport(
  'useRoute',
  () => () => ({ params: { id: 'engineer-compact' } }),
);

async function mountDetail() {
  const wrapper = await mountSuspended(TemplateDetailPage);
  await flushPromises();
  return wrapper;
}

describe('template page identity', () => {
  beforeEach(() => setSiteLocale('en'));

  it('makes Use this sample a main pill and the blank link an outline pill',
    async () => {
      const wrapper = await mountDetail();
      const primary = wrapper.get('[data-action="use-sample"]');
      for (const name of ['rounded-full', 'bg-primary', 'h-12']) {
        expect(primary.classes(), name).toContain(name);
      }
      const blank = wrapper.get('[data-action="use-blank"]');
      expect(blank.text()).toBe('Use this template with a blank resume');
      for (const name of [
        'rounded-full', 'border-[1.5px]', 'border-foreground',
        'bg-transparent', 'justify-self-start',
      ]) {
        expect(blank.classes(), name).toContain(name);
      }
      expect(blank.classes().join(' ')).not.toMatch(
        /(?:^|\s)(?:underline|text-link|bg-primary)(?:\s|$)/u,
      );
    });

  it('sets the template name as a page heading at line height 1.3',
    async () => {
      const wrapper = await mountDetail();
      const title = wrapper.get('h1');
      for (const name of ['text-2xl', 'font-extrabold', 'tracking-[-0.02em]']) {
        expect(title.classes(), name).toContain(name);
      }
      expect(title.classes().join(' ')).not.toMatch(
        /leading-tight|text-\[34px\]/u,
      );
      const crumb = wrapper.get('nav[aria-label="Breadcrumb"] a');
      expect(crumb.text()).toBe('Library');
      for (const name of [
        'font-semibold', 'text-link', 'hover:underline',
        'focus-visible:underline',
      ]) {
        expect(crumb.classes(), name).toContain(name);
      }
    });

  it('shows the selected sample language as an ink pill', async () => {
    const wrapper = await mountDetail();
    const group = wrapper.get('[role="group"]');
    for (const name of ['rounded-full', 'bg-muted']) {
      expect(group.classes(), name).toContain(name);
    }
    const options = group.findAll('[data-sample-language]');
    expect(options.map((option) => option.attributes('aria-pressed')))
      .toEqual(['false', 'true']);
    for (const option of options) {
      const pressed = option.attributes('aria-pressed') === 'true';
      const classes = option.classes();
      expect(classes).toContain('rounded-full');
      expect(classes).not.toContain('bg-primary');
      expect(classes.includes('bg-foreground'), 'ink fill').toBe(pressed);
      expect(classes.includes('text-background'), 'ink text').toBe(pressed);
    }
  });

  it('keeps the tabs on the ink tab primitive', async () => {
    const wrapper = await mountDetail();
    const list = wrapper.get('[role="tablist"]');
    expect(list.classes()).toContain('bg-muted');
    expect(list.classes()).toContain('rounded-full');
    for (const tab of wrapper.findAll('[role="tab"]')) {
      expect(tab.classes()).toContain('data-[state=active]:bg-foreground');
      expect(tab.classes()).toContain('data-[state=active]:text-background');
    }
  });

  it('puts the two-column note on mist with the 10 px radius', async () => {
    const wrapper = await mountDetail();
    const note = wrapper.get('[data-two-column-note]');
    for (const name of ['bg-surface-mist', 'rounded-[var(--radius)]']) {
      expect(note.classes(), name).toContain(name);
    }
    expect(note.classes()).not.toContain('bg-surface-blue');
    const link = note.get('a');
    expect(link.classes()).toContain('text-link');
    expect(link.classes()).toContain('underline');
  });

  it('floats the phone action bar on the page ground with a top rule', () => {
    expect(declared(sfcRules(PAGE), '.template-detail__actions',
      ['@media (width < 900px)'])).toMatchObject({
      'position': 'fixed',
      'border-top': '1px solid var(--border)',
      'background': 'var(--background)',
      'box-shadow': 'var(--shadow-float)',
    });
  });

  it('paints the sheet on the paper token, with no hex or removed token',
    () => {
      expect(declared(sfcRules(PAGE), '.template-paper')).toMatchObject({
        'background': 'var(--paper)',
        'border-radius': 'var(--radius-sheet)',
        'box-shadow': 'var(--shadow-paper)',
      });
      expect(webSource(PAGE)).not.toMatch(
        /surface-(?:blue|indigo|sand)|radius-feature|#[\da-f]{3,8}\b/iu,
      );
    });
});
