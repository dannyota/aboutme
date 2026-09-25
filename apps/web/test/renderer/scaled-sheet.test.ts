import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import ScaledSheet from '../../app/components/resume/ScaledSheet.vue';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('ScaledSheet', () => {
  it('sizes the outer box from the ResizeObserver content size and scales '
    + 'the inner content, never with CSS zoom', async () => {
    let deliver: ResizeObserverCallback | undefined;
    class FakeResizeObserver {
      constructor(callback: ResizeObserverCallback) {
        deliver = callback;
      }

      observe = vi.fn();
      disconnect = vi.fn();
      unobserve = vi.fn();
    }
    vi.stubGlobal('ResizeObserver', FakeResizeObserver);

    const wrapper = mount(ScaledSheet, {
      props: { scale: 0.5 },
      slots: { default: '<p>content</p>' },
    });
    await nextTick();

    deliver?.(
      [{ contentRect: { width: 794, height: 2300 } } as ResizeObserverEntry],
      {} as ResizeObserver,
    );
    await nextTick();

    const outer = wrapper.get('.scaled-sheet');
    expect(outer.attributes('style')).toContain('width: 397px');
    expect(outer.attributes('style')).toContain('height: 1150px');

    const inner = wrapper.get('.scaled-sheet-content');
    expect(inner.attributes('style')).toContain('transform: scale(0.5)');

    expect(outer.attributes('style')).not.toContain('zoom');
    expect(inner.attributes('style')).not.toContain('zoom');
  });

  it('keeps the offsetWidth/offsetHeight seed when ResizeObserver is '
    + 'unavailable', async () => {
    vi.stubGlobal('ResizeObserver', undefined);
    const widthGetter = vi.spyOn(
      HTMLElement.prototype,
      'offsetWidth',
      'get',
    ).mockReturnValue(794);
    const heightGetter = vi.spyOn(
      HTMLElement.prototype,
      'offsetHeight',
      'get',
    ).mockReturnValue(2300);

    const wrapper = mount(ScaledSheet, {
      props: { scale: 0.5 },
      slots: { default: '<p>content</p>' },
    });
    await nextTick();

    const outer = wrapper.get('.scaled-sheet');
    expect(outer.attributes('style')).toContain('width: 397px');
    expect(outer.attributes('style')).toContain('height: 1150px');
    expect(outer.attributes('style')).not.toContain('zoom');

    widthGetter.mockRestore();
    heightGetter.mockRestore();
    wrapper.unmount();
  });
});
