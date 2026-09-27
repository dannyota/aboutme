import { mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import PanelResizeHandle from
  '../../app/components/editor/PanelResizeHandle.vue';
import {
  EDITOR_PANEL_CSS_VAR,
  EDITOR_PANEL_STORAGE_KEY,
} from '../../app/composables/useResizablePanel';
import { setSiteLocale } from '../support/locale';

const originalClientWidth = document.documentElement.clientWidth;

// A viewport wide enough that the preview's 29.5rem floor never limits the
// panel (needs more than 98rem), so the default max reads as the flat 48rem
// ceiling.
function useWideViewport(): void {
  Object.defineProperty(document.documentElement, 'clientWidth', {
    configurable: true,
    value: 200 * 16,
  });
}

beforeEach(() => {
  setSiteLocale('en');
  useWideViewport();
});

afterEach(() => {
  setSiteLocale(undefined);
  window.localStorage.removeItem(EDITOR_PANEL_STORAGE_KEY);
  document.documentElement.style.removeProperty(EDITOR_PANEL_CSS_VAR);
  // A test that fails mid-drag can leave these set; later tests must not
  // inherit them.
  document.body.style.userSelect = '';
  document.body.style.cursor = '';
  Object.defineProperty(document.documentElement, 'clientWidth', {
    configurable: true,
    value: originalClientWidth,
  });
});

function handle(wrapper: ReturnType<typeof mount>) {
  return wrapper.get('[data-testid="panel-resize-handle"]');
}

describe('PanelResizeHandle', () => {
  it(
    'renders as a focusable separator with the English accessible name',
    async () => {
      const wrapper = mount(PanelResizeHandle);
      await wrapper.vm.$nextTick();
      const element = handle(wrapper);

      expect(element.attributes('role')).toBe('separator');
      expect(element.attributes('aria-orientation')).toBe('vertical');
      expect(element.attributes('aria-label')).toBe('Resize editor panel');
      expect(element.attributes('tabindex')).toBe('0');
      expect(element.attributes('aria-valuemin')).toBe('22');
      expect(element.attributes('aria-valuemax')).toBe('48');
      expect(element.attributes('aria-valuenow')).toBe('22');
      wrapper.unmount();
    },
  );

  it('carries the Vietnamese accessible name', async () => {
    setSiteLocale('vi');
    const wrapper = mount(PanelResizeHandle);
    await wrapper.vm.$nextTick();

    expect(handle(wrapper).attributes('aria-label')).toBe(
      'Đổi độ rộng khung chỉnh sửa',
    );
    wrapper.unmount();
  });

  it(
    'grows on ArrowLeft and shrinks on ArrowRight, in 1rem steps',
    async () => {
      const wrapper = mount(PanelResizeHandle);
      await wrapper.vm.$nextTick();
      const element = handle(wrapper);

      await element.trigger('keydown', { key: 'ArrowLeft' });
      await element.trigger('keydown', { key: 'ArrowLeft' });
      expect(element.attributes('aria-valuenow')).toBe('24');

      await element.trigger('keydown', { key: 'ArrowRight' });
      expect(element.attributes('aria-valuenow')).toBe('23');
      wrapper.unmount();
    },
  );

  it('steps by 4rem with Shift held', async () => {
    const wrapper = mount(PanelResizeHandle);
    await wrapper.vm.$nextTick();
    const element = handle(wrapper);

    await element.trigger('keydown', { key: 'ArrowLeft', shiftKey: true });
    expect(element.attributes('aria-valuenow')).toBe('26');
    wrapper.unmount();
  });

  it('Home and End jump to the min and max', async () => {
    const wrapper = mount(PanelResizeHandle);
    await wrapper.vm.$nextTick();
    const element = handle(wrapper);

    await element.trigger('keydown', { key: 'End' });
    expect(element.attributes('aria-valuenow')).toBe('48');

    await element.trigger('keydown', { key: 'Home' });
    expect(element.attributes('aria-valuenow')).toBe('22');
    wrapper.unmount();
  });

  it('Enter and double-click reset to the default width', async () => {
    const wrapper = mount(PanelResizeHandle);
    await wrapper.vm.$nextTick();
    const element = handle(wrapper);

    await element.trigger('keydown', { key: 'End' });
    expect(element.attributes('aria-valuenow')).toBe('48');

    await element.trigger('keydown', { key: 'Enter' });
    expect(element.attributes('aria-valuenow')).toBe('22');

    await element.trigger('keydown', { key: 'End' });
    await element.trigger('dblclick');
    expect(element.attributes('aria-valuenow')).toBe('22');
    wrapper.unmount();
  });

  it('resizes by dragging, growing when the pointer moves left', async () => {
    const wrapper = mount(PanelResizeHandle);
    await wrapper.vm.$nextTick();
    const element = handle(wrapper);

    await element.trigger('pointerdown', { clientX: 200, pointerId: 1 });
    await element.trigger('pointermove', { clientX: 184, pointerId: 1 });
    // 16px left at a 16px root font size is exactly 1rem.
    expect(element.attributes('aria-valuenow')).toBe('23');

    await element.trigger('pointerup', { clientX: 184, pointerId: 1 });
    await element.trigger('pointermove', { clientX: 100, pointerId: 1 });
    // The drag ended, so a later move with no new pointerdown is ignored.
    expect(element.attributes('aria-valuenow')).toBe('23');
    wrapper.unmount();
  });

  it('stops the drag on pointercancel', async () => {
    const wrapper = mount(PanelResizeHandle);
    await wrapper.vm.$nextTick();
    const element = handle(wrapper);

    await element.trigger('pointerdown', { clientX: 200, pointerId: 1 });
    await element.trigger('pointercancel', { clientX: 200, pointerId: 1 });
    await element.trigger('pointermove', { clientX: 100, pointerId: 1 });

    expect(element.attributes('aria-valuenow')).toBe('22');
    wrapper.unmount();
  });

  it('blocks text selection only while dragging', async () => {
    const wrapper = mount(PanelResizeHandle);
    await wrapper.vm.$nextTick();
    const element = handle(wrapper);

    await element.trigger('pointerdown', { clientX: 200, pointerId: 1 });
    expect(document.body.style.userSelect).toBe('none');
    expect(document.body.style.cursor).toBe('col-resize');

    await element.trigger('pointerup', { clientX: 200, pointerId: 1 });
    expect(document.body.style.userSelect).not.toBe('none');
    wrapper.unmount();
  });

  it('restores the body when pointer capture is lost mid-drag', async () => {
    const wrapper = mount(PanelResizeHandle);
    await wrapper.vm.$nextTick();
    const element = handle(wrapper);

    await element.trigger('pointerdown', { clientX: 200, pointerId: 1 });
    await element.trigger('lostpointercapture', { pointerId: 1 });
    expect(document.body.style.userSelect).not.toBe('none');
    await element.trigger('pointermove', { clientX: 100, pointerId: 1 });
    expect(element.attributes('aria-valuenow')).toBe('22');
    wrapper.unmount();
  });

  it('restores the body when the editor unmounts mid-drag', async () => {
    const wrapper = mount(PanelResizeHandle);
    await wrapper.vm.$nextTick();
    await handle(wrapper).trigger('pointerdown', {
      clientX: 200,
      pointerId: 1,
    });
    wrapper.unmount();
    expect(document.body.style.userSelect).not.toBe('none');
    expect(document.body.style.cursor).not.toBe('col-resize');
  });

  it('ignores a secondary button', async () => {
    const wrapper = mount(PanelResizeHandle);
    await wrapper.vm.$nextTick();
    const element = handle(wrapper);

    await element.trigger('pointerdown', {
      button: 2,
      clientX: 200,
      pointerId: 1,
    });
    expect(document.body.style.userSelect).not.toBe('none');
    wrapper.unmount();
  });

  it(
    'announces a whole rem while a fractional drag is still live',
    async () => {
      const wrapper = mount(PanelResizeHandle);
      await wrapper.vm.$nextTick();
      const element = handle(wrapper);

      await element.trigger('pointerdown', { clientX: 200, pointerId: 1 });
      // 19px left at a 16px root font size is 22 + 1.1875 = 23.1875rem.
      await element.trigger('pointermove', { clientX: 181, pointerId: 1 });

      expect(element.attributes('aria-valuenow')).toBe('23');
      expect(element.attributes('aria-valuetext')).toBe('23rem');
      wrapper.unmount();
    },
  );

  it(
    'snaps the width a drag commits to the nearest quarter rem',
    async () => {
      const wrapper = mount(PanelResizeHandle);
      await wrapper.vm.$nextTick();
      const element = handle(wrapper);

      await element.trigger('pointerdown', { clientX: 200, pointerId: 1 });
      // 19px left is 23.1875rem live; committed, it snaps to 23.25rem.
      await element.trigger('pointermove', { clientX: 181, pointerId: 1 });
      await element.trigger('pointerup', { clientX: 181, pointerId: 1 });

      expect(window.localStorage.getItem(EDITOR_PANEL_STORAGE_KEY))
        .toBe('23.25');
      wrapper.unmount();
    },
  );

  it('keeps the 1px border line at rest', async () => {
    const wrapper = mount(PanelResizeHandle);
    await wrapper.vm.$nextTick();
    const bar = wrapper.get('[data-testid="panel-resize-handle"] span');

    expect(bar.classes()).toContain('w-px');
    expect(bar.classes()).toContain('bg-border');
    wrapper.unmount();
  });

  it(
    'widens to a 3px primary bar on hover, drag, and focus-visible',
    async () => {
      const wrapper = mount(PanelResizeHandle);
      await wrapper.vm.$nextTick();
      const bar = wrapper.get('[data-testid="panel-resize-handle"] span');

      expect(bar.classes()).toEqual(expect.arrayContaining([
        'group-hover:w-[3px]',
        'group-hover:bg-primary',
        'group-active:w-[3px]',
        'group-active:bg-primary',
        'group-focus-visible:w-[3px]',
        'group-focus-visible:bg-primary',
      ]));
      wrapper.unmount();
    },
  );

  it(
    'pulls the focus outline inward instead of a second ring',
    async () => {
      const wrapper = mount(PanelResizeHandle);
      await wrapper.vm.$nextTick();

      expect(handle(wrapper).classes())
        .toContain('focus-visible:-outline-offset-2');
      wrapper.unmount();
    },
  );
});
