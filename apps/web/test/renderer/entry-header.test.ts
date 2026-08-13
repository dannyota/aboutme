import { mount } from '@vue/test-utils';
import { defineComponent, h, nextTick } from 'vue';
import { describe, expect, it } from 'vitest';

import EntryHeader from '../../app/components/resume/primitives/EntryHeader.vue'; // eslint-disable-line max-len

const makeHarness = (meta: readonly string[] = []) =>
  defineComponent({
    components: { EntryHeader },
    data() {
      return { meta, showWidget: false };
    },
    render() {
      return h(
        EntryHeader,
        { meta: this.meta },
        {
          'meta-widget': this.showWidget
            ? () => h('span', { class: 'widget' }, 'Widget')
            : undefined,
        },
      );
    },
  });

const state = (wrapper: {
  vm: unknown;
}): { showWidget: boolean } =>
  wrapper.vm as { showWidget: boolean };

describe('EntryHeader meta-widget slot reactivity', () => {
  it('toggles .entry-meta with a dynamic slot', async () => {
    const wrapper = mount(makeHarness());
    const header = wrapper.findComponent(EntryHeader);
    const reactive = state(wrapper);

    expect(header.exists()).toBe(true);
    expect(wrapper.find('.entry-meta').exists()).toBe(false);
    expect(wrapper.find('.widget').exists()).toBe(false);

    reactive.showWidget = true;
    await nextTick();
    expect(wrapper.find('.entry-meta').exists()).toBe(true);
    expect(wrapper.find('.widget').exists()).toBe(true);
    expect(wrapper.findComponent(EntryHeader).element).toBe(header.element);

    reactive.showWidget = false;
    await nextTick();
    expect(wrapper.find('.entry-meta').exists()).toBe(false);
    expect(wrapper.find('.widget').exists()).toBe(false);
  });

  it('keeps ordinary meta visible while the widget slot toggles', async () => {
    const wrapper = mount(makeHarness(['Always']));
    const reactive = state(wrapper);

    expect(wrapper.find('.entry-meta').exists()).toBe(true);
    expect(wrapper.text()).toContain('Always');
    expect(wrapper.find('.widget').exists()).toBe(false);

    reactive.showWidget = true;
    await nextTick();
    expect(wrapper.find('.entry-meta').exists()).toBe(true);
    expect(wrapper.find('.widget').exists()).toBe(true);

    reactive.showWidget = false;
    await nextTick();
    expect(wrapper.find('.entry-meta').exists()).toBe(true);
    expect(wrapper.find('.widget').exists()).toBe(false);
  });
});
