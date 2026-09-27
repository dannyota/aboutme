import { mount } from '@vue/test-utils';
import { defineComponent, ref } from 'vue';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import EditorRegionTabs from
  '../../app/components/editor/EditorRegionTabs.vue';
import { setSiteLocale } from '../support/locale';

beforeEach(() => {
  setSiteLocale('en');
});

afterEach(() => {
  setSiteLocale(undefined);
});

describe('EditorRegionTabs', () => {
  it('names the tab list and marks the active tab for screen readers', () => {
    const wrapper = mount(EditorRegionTabs, {
      props: { modelValue: 'editor' },
    });
    const list = wrapper.get('[role="tablist"]');
    const edit = list.get('[data-action="show-editor"]');
    const preview = list.get('[data-action="show-preview"]');

    expect(list.attributes('aria-label')).toBe('Editor view');
    expect(edit.attributes('role')).toBe('tab');
    expect(edit.text()).toBe('Edit');
    expect(edit.attributes('aria-selected')).toBe('true');
    expect(edit.attributes('aria-pressed')).toBe('true');
    expect(preview.text()).toBe('Preview');
    expect(preview.attributes('aria-selected')).toBe('false');
    expect(preview.attributes('aria-pressed')).toBe('false');
  });

  it('hides only from 72 rem, where the preview has its own column', () => {
    const wrapper = mount(EditorRegionTabs, {
      props: { modelValue: 'editor' },
    });
    const classes = wrapper.get('[role="tablist"]').classes();

    expect(classes).toContain('min-[72rem]:hidden');
    expect(classes).not.toContain('min-[42rem]:hidden');
  });

  it('switches to the preview and back with focusable tabs', async () => {
    const Host = defineComponent({
      components: { EditorRegionTabs },
      setup: () => ({ region: ref<'editor' | 'preview'>('editor') }),
      template: [
        '<EditorRegionTabs v-model="region" />',
        '<output data-testid="region">{{ region }}</output>',
      ].join(''),
    });
    const wrapper = mount(Host, { attachTo: document.body });
    const preview = wrapper.get('[data-action="show-preview"]');

    expect(preview.element.tagName).toBe('BUTTON');
    (preview.element as HTMLButtonElement).focus();
    expect(document.activeElement).toBe(preview.element);
    await preview.trigger('click');
    expect(wrapper.get('[data-testid="region"]').text()).toBe('preview');
    expect(preview.attributes('aria-selected')).toBe('true');

    const edit = wrapper.get('[data-action="show-editor"]');
    await edit.trigger('click');
    expect(wrapper.get('[data-testid="region"]').text()).toBe('editor');
    expect(edit.attributes('aria-selected')).toBe('true');
    expect(preview.attributes('aria-selected')).toBe('false');
    wrapper.unmount();
  });

  it('follows the site locale', async () => {
    setSiteLocale('vi');
    const wrapper = mount(EditorRegionTabs, {
      props: { modelValue: 'preview' },
    });

    expect(wrapper.get('[role="tablist"]').attributes('aria-label'))
      .toBe('Chế độ chỉnh sửa');
    expect(wrapper.get('[data-action="show-editor"]').text())
      .toBe('Chỉnh sửa');
    expect(wrapper.get('[data-action="show-preview"]').text())
      .toBe('Xem trước');
  });
});
