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

describe('template detail page: tabs', () => {
  it('orders the tabs Page, PDF, What an ATS reads in English', async () => {
    setSiteLocale('en');
    const wrapper = await mountSuspended(TemplateDetailPage);
    await flushPromises();

    const triggers = wrapper.findAll('[role="tab"]');
    expect(triggers.map((tab) => tab.text()))
      .toEqual(['Page', 'PDF', 'What an ATS reads']);
  });

  it('orders the tabs Trang CV, PDF, ATS đọc được gì in Vietnamese',
    async () => {
      setSiteLocale('vi');
      const wrapper = await mountSuspended(TemplateDetailPage);
      await flushPromises();

      const triggers = wrapper.findAll('[role="tab"]');
      expect(triggers.map((tab) => tab.text()))
        .toEqual(['Trang CV', 'PDF', 'ATS đọc được gì']);
    });

  it('scopes the ATS sheet to paper so its text stays dark ink', async () => {
    setSiteLocale('en');
    const wrapper = await mountSuspended(TemplateDetailPage);
    await flushPromises();

    const triggers = wrapper.findAll('[role="tab"]');
    // reka-ui's TabsTrigger switches on mousedown, not click.
    await triggers[2]!.trigger('mousedown', { button: 0 });
    await flushPromises();

    const panel = wrapper.get('[data-ats-text]');
    expect(panel.classes()).toContain('paper-surface');
    expect(panel.text()).toContain('Đỗ Hoàng Nam');
  });

  it('shows every stored page with alt text and a caption in the PDF tab',
    async () => {
      setSiteLocale('en');
      const wrapper = await mountSuspended(TemplateDetailPage);
      await flushPromises();

      const triggers = wrapper.findAll('[role="tab"]');
      // reka-ui's TabsTrigger switches on mousedown, not click.
      await triggers[1]!.trigger('mousedown', { button: 0 });
      await flushPromises();

      const images = wrapper.findAll('img[data-pdf-page]');
      expect(images).toHaveLength(1);
      expect(images[0]!.attributes('alt')).toBe(
        'Page 1 of 1 of the Engineer Compact sample resume',
      );
      expect(images[0]!.attributes('loading')).toBe('eager');
      expect(wrapper.text()).toContain('Page 1 of 1');
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
