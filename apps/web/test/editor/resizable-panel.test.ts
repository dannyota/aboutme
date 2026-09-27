import { mount } from '@vue/test-utils';
import { defineComponent, ref } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  clampPanelWidthRem,
  createResizablePanelController,
  EDITOR_PANEL_CSS_VAR,
  EDITOR_PANEL_DEFAULT_REM,
  EDITOR_PANEL_MAX_REM,
  EDITOR_PANEL_MIN_REM,
  EDITOR_PANEL_STORAGE_KEY,
  maxPanelWidthRem,
  parseStoredPanelWidthRem,
  useResizablePanel,
} from '../../app/composables/useResizablePanel';

// A viewport wide enough that the preview's 32rem floor never limits the
// panel (needs more than 100.5rem), so tests can assert the flat 48rem
// ceiling.
const WIDE_VIEWPORT_REM = 200;
const originalInnerWidth = window.innerWidth;

function useWideViewport(): void {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: WIDE_VIEWPORT_REM * 16,
  });
}

// useResizablePanel reads window.innerWidth at setup, so every test gets a
// viewport wide enough that the 48rem ceiling, not the preview floor, is
// what a stored or dragged width is checked against.
beforeEach(useWideViewport);

afterEach(() => {
  window.localStorage.removeItem(EDITOR_PANEL_STORAGE_KEY);
  document.documentElement.style.removeProperty(EDITOR_PANEL_CSS_VAR);
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: originalInnerWidth,
  });
  vi.unstubAllGlobals();
});

describe('maxPanelWidthRem', () => {
  it('caps at 48rem on a wide viewport', () => {
    expect(maxPanelWidthRem(WIDE_VIEWPORT_REM)).toBe(48);
  });

  it('keeps the preview at its 32rem floor on a mid-size viewport', () => {
    // 90rem viewport - 20.5rem rail/outline - 32rem preview floor = 37.5rem.
    expect(maxPanelWidthRem(90)).toBe(37.5);
  });

  it('never drops the max below the 22rem minimum', () => {
    // 72.5rem - 20.5rem - 32rem = 20rem of room, less than the minimum.
    expect(maxPanelWidthRem(72.5)).toBe(EDITOR_PANEL_MIN_REM);
  });
});

describe('clampPanelWidthRem', () => {
  it('pins a value above the max down to the max', () => {
    expect(clampPanelWidthRem(90, WIDE_VIEWPORT_REM)).toBe(48);
  });

  it('pins a value below the min up to the min', () => {
    expect(clampPanelWidthRem(1, WIDE_VIEWPORT_REM)).toBe(22);
  });

  it('falls back to the default for a non-finite value', () => {
    expect(clampPanelWidthRem(Number.NaN, WIDE_VIEWPORT_REM))
      .toBe(EDITOR_PANEL_DEFAULT_REM);
    expect(clampPanelWidthRem(Number.POSITIVE_INFINITY, WIDE_VIEWPORT_REM))
      .toBe(EDITOR_PANEL_DEFAULT_REM);
  });
});

describe('parseStoredPanelWidthRem', () => {
  it('uses the default when nothing is stored', () => {
    expect(parseStoredPanelWidthRem(null, WIDE_VIEWPORT_REM))
      .toBe(EDITOR_PANEL_DEFAULT_REM);
  });

  it('uses the default for an unparsable value', () => {
    expect(parseStoredPanelWidthRem('not-a-number', WIDE_VIEWPORT_REM))
      .toBe(EDITOR_PANEL_DEFAULT_REM);
  });

  it('accepts a value inside today\'s range', () => {
    expect(parseStoredPanelWidthRem('30', WIDE_VIEWPORT_REM)).toBe(30);
    expect(parseStoredPanelWidthRem('22', WIDE_VIEWPORT_REM)).toBe(22);
  });

  it(
    'falls back to the default for an out-of-range value, rather than '
    + 'clamping it',
    () => {
      expect(parseStoredPanelWidthRem('5', WIDE_VIEWPORT_REM))
        .toBe(EDITOR_PANEL_DEFAULT_REM);
      expect(parseStoredPanelWidthRem('9999', WIDE_VIEWPORT_REM))
        .toBe(EDITOR_PANEL_DEFAULT_REM);
    },
  );
});

describe('createResizablePanelController', () => {
  function controller(viewportRemValue = WIDE_VIEWPORT_REM) {
    const widthRem = ref(EDITOR_PANEL_DEFAULT_REM);
    const viewportRem = ref(viewportRemValue);
    return {
      panel: createResizablePanelController(widthRem, viewportRem),
      viewportRem,
    };
  }

  it('steps by 1rem and clamps at the bounds', () => {
    const { panel } = controller();
    panel.stepBy(1);
    expect(panel.widthRem.value).toBe(23);
    panel.stepBy(-1);
    expect(panel.widthRem.value).toBe(22);
    panel.stepBy(-1);
    expect(panel.widthRem.value).toBe(EDITOR_PANEL_MIN_REM);
  });

  it('steps by the large amount', () => {
    const { panel } = controller();
    panel.stepBy(4);
    expect(panel.widthRem.value).toBe(26);
  });

  it('resets to the default after moving away from it', () => {
    const { panel } = controller();
    panel.setWidthRem(35);
    panel.reset();
    expect(panel.widthRem.value).toBe(EDITOR_PANEL_DEFAULT_REM);
  });

  it('goes to the min and max bounds', () => {
    const { panel } = controller();
    panel.goToMax();
    expect(panel.widthRem.value).toBe(EDITOR_PANEL_MAX_REM);
    panel.goToMin();
    expect(panel.widthRem.value).toBe(EDITOR_PANEL_MIN_REM);
  });

  it('re-clamps against a viewport that shrank', () => {
    const { panel, viewportRem } = controller();
    panel.setWidthRem(40);
    expect(panel.widthRem.value).toBe(40);

    viewportRem.value = 72.5;
    expect(panel.maxRem.value).toBe(EDITOR_PANEL_MIN_REM);
    panel.setWidthRem(panel.widthRem.value);
    expect(panel.widthRem.value).toBe(EDITOR_PANEL_MIN_REM);
  });

  it.each([
    ['ArrowLeft', false, 22, 23],
    ['ArrowLeft', true, 22, 26],
    // Right/decrease starts away from the min, or it would clamp straight
    // back to the floor and not show a change.
    ['ArrowRight', false, 30, 29],
    ['ArrowRight', true, 30, 26],
  ] as const)(
    'handles %s (shift=%s) from %irem',
    (key, shiftKey, start, expected) => {
      const { panel } = controller();
      panel.setWidthRem(start);
      const preventDefault = vi.fn();
      panel.handleKeyDown({ key, shiftKey, preventDefault } as never);
      expect(preventDefault).toHaveBeenCalledOnce();
      expect(panel.widthRem.value).toBe(expected);
    },
  );

  it.each(['Home', 'End', 'Enter'] as const)(
    '%s is handled and prevents the default action',
    (key) => {
      const { panel } = controller();
      panel.setWidthRem(30);
      const preventDefault = vi.fn();
      panel.handleKeyDown({ key, shiftKey: false, preventDefault } as never);
      expect(preventDefault).toHaveBeenCalledOnce();
    },
  );

  it('Home goes to the min and End goes to the max', () => {
    const { panel } = controller();
    panel.setWidthRem(30);
    panel.handleKeyDown({
      key: 'Home',
      shiftKey: false,
      preventDefault: vi.fn(),
    } as never);
    expect(panel.widthRem.value).toBe(EDITOR_PANEL_MIN_REM);

    panel.handleKeyDown({
      key: 'End',
      shiftKey: false,
      preventDefault: vi.fn(),
    } as never);
    expect(panel.widthRem.value).toBe(EDITOR_PANEL_MAX_REM);
  });

  it('Enter resets to the default', () => {
    const { panel } = controller();
    panel.setWidthRem(30);
    panel.handleKeyDown({
      key: 'Enter',
      shiftKey: false,
      preventDefault: vi.fn(),
    } as never);
    expect(panel.widthRem.value).toBe(EDITOR_PANEL_DEFAULT_REM);
  });

  it('ignores an unrelated key without changing width', () => {
    const { panel } = controller();
    const preventDefault = vi.fn();
    panel.handleKeyDown({
      key: 'a',
      shiftKey: false,
      preventDefault,
    } as never);
    expect(preventDefault).not.toHaveBeenCalled();
    expect(panel.widthRem.value).toBe(EDITOR_PANEL_DEFAULT_REM);
  });
});

const Harness = defineComponent({
  setup() {
    const panel = useResizablePanel();
    return { panel };
  },
  template: '<div />',
});

describe('useResizablePanel', () => {
  it('starts at the default before mount, matching the CSS fallback', () => {
    const wrapper = mount(Harness);
    expect(wrapper.vm.panel.widthRem.value).toBe(EDITOR_PANEL_DEFAULT_REM);
    wrapper.unmount();
  });

  it('restores a valid stored width on mount', async () => {
    window.localStorage.setItem(EDITOR_PANEL_STORAGE_KEY, '30');
    const wrapper = mount(Harness);
    await wrapper.vm.$nextTick();
    expect(wrapper.vm.panel.widthRem.value).toBe(30);
    wrapper.unmount();
  });

  it('falls back to the default for a stored value out of range', async () => {
    window.localStorage.setItem(EDITOR_PANEL_STORAGE_KEY, '9999');
    const wrapper = mount(Harness);
    await wrapper.vm.$nextTick();
    expect(wrapper.vm.panel.widthRem.value).toBe(EDITOR_PANEL_DEFAULT_REM);
    wrapper.unmount();
  });

  it('keeps the default when reading storage throws', async () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem')
      .mockImplementation(() => {
        throw new Error('blocked');
      });
    const wrapper = mount(Harness);
    await wrapper.vm.$nextTick();
    expect(wrapper.vm.panel.widthRem.value).toBe(EDITOR_PANEL_DEFAULT_REM);
    wrapper.unmount();
    getItem.mockRestore();
  });

  it('persists a changed width to localStorage', async () => {
    const wrapper = mount(Harness);
    await wrapper.vm.$nextTick();
    wrapper.vm.panel.setWidthRem(28);
    await wrapper.vm.$nextTick();
    expect(window.localStorage.getItem(EDITOR_PANEL_STORAGE_KEY)).toBe('28');
    wrapper.unmount();
  });

  it('does not throw when writing storage fails', async () => {
    const setItem = vi.spyOn(Storage.prototype, 'setItem')
      .mockImplementation(() => {
        throw new Error('full');
      });
    const wrapper = mount(Harness);
    await wrapper.vm.$nextTick();
    expect(() => wrapper.vm.panel.setWidthRem(28)).not.toThrow();
    wrapper.unmount();
    setItem.mockRestore();
  });

  it('writes the width to the shared CSS custom property', async () => {
    const wrapper = mount(Harness);
    await wrapper.vm.$nextTick();
    wrapper.vm.panel.setWidthRem(30);
    await wrapper.vm.$nextTick();
    expect(
      document.documentElement.style.getPropertyValue(EDITOR_PANEL_CSS_VAR),
    ).toBe('30rem');
    wrapper.unmount();
  });

  it('re-clamps on window resize', async () => {
    const wrapper = mount(Harness);
    await wrapper.vm.$nextTick();
    wrapper.vm.panel.setWidthRem(40);
    expect(wrapper.vm.panel.widthRem.value).toBe(40);

    Object.defineProperty(window, 'innerWidth', {
      configurable: true,
      value: 72.5 * 16,
    });
    window.dispatchEvent(new Event('resize'));
    await wrapper.vm.$nextTick();

    expect(wrapper.vm.panel.widthRem.value).toBe(EDITOR_PANEL_MIN_REM);
    wrapper.unmount();
  });
});
