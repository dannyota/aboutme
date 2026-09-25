import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import TemplateDetailPage from '../../app/pages/templates/[id].vue';
import { setSiteLocale } from '../support/locale';

// The "What an ATS reads" panel is a white sheet in both themes, like the
// page view next to it (DESIGN.md, template gallery). It needs the paper
// scope so its text stays dark ink instead of the dark-theme foreground,
// which would read as near-white text on the white sheet.
mockNuxtImport(
  'useRoute',
  () => () => ({ params: { id: 'engineer-compact' } }),
);

describe('template detail page: what an ATS reads', () => {
  it('keeps the page tab and the ATS tab, and scopes the ATS sheet to paper',
    async () => {
      setSiteLocale('en');
      const wrapper = await mountSuspended(TemplateDetailPage);
      await flushPromises();

      const triggers = wrapper.findAll('[role="tab"]');
      expect(triggers.map((tab) => tab.text()))
        .toEqual(['Page', 'What an ATS reads']);

      // reka-ui's TabsTrigger switches on mousedown, not click.
      await triggers[1]!.trigger('mousedown', { button: 0 });
      await flushPromises();

      const panel = wrapper.get('[data-ats-text]');
      expect(panel.classes()).toContain('paper-surface');
      expect(panel.text()).toContain('Khoa Vu');
    });

  it(
    'keeps a single h1: the template name, not the embedded sample name',
    async () => {
      setSiteLocale('en');
      const wrapper = await mountSuspended(TemplateDetailPage);
      await flushPromises();

      expect(wrapper.get('h1').text()).toBe('Engineer Compact');
      expect(wrapper.findAll('h1')).toHaveLength(1);
      expect(wrapper.get('.resume-name').element.tagName).toBe('P');
    },
  );
});
