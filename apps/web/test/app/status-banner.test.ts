import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { describe, expect, it } from 'vitest';

import StatusBanner from '../../app/components/app/StatusBanner.vue';

describe('StatusBanner', () => {
  it.each([
    ['error', 'alert'],
    ['success', 'status'],
    ['info', 'status'],
  ] as const)('%s renders role %s', (kind, role) => {
    const wrapper = mount(StatusBanner, {
      props: { kind, testid: 'x' },
      slots: { default: 'Saved.' },
    });
    expect(wrapper.attributes('role')).toBe(role);
    expect(wrapper.attributes('data-testid')).toBe('x');
    expect(wrapper.text()).toContain('Saved.');
  });
  it('focuses itself on mount when asked', async () => {
    const wrapper = mount(StatusBanner, {
      attachTo: document.body,
      props: { kind: 'error', focusOnMount: true },
      slots: { default: 'Fix this.' },
    });
    await nextTick();
    expect(document.activeElement).toBe(wrapper.element);
    wrapper.unmount();
  });

  it('renders success as a neutral pencil mark', () => {
    const wrapper = mount(StatusBanner, {
      props: { kind: 'success', testid: 'success' },
      slots: { default: 'Saved.' },
    });

    const banner = wrapper.get('[data-testid="success"]');
    expect(banner.classes()).toEqual(
      expect.arrayContaining(['border-border', 'text-foreground']),
    );
    expect(banner.classes().join(' ')).not.toContain('positive');
    expect(wrapper.get('[data-status-glyph="success"]').exists()).toBe(true);
  });
});
