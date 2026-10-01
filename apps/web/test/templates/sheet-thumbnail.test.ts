// @vitest-environment jsdom

import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { nextTick } from 'vue';

import SheetThumbnail from
  '../../app/components/templates/SheetThumbnail.vue';
import { sampleResume } from '../../app/landing/sampleResume';

// A thumbnail scales its A4 page with ScaledSheet's transform, never CSS
// `zoom`, which WebKit renders with enlarged text (print.md §1).

type Callback = (entries: { isIntersecting: boolean }[]) => void;

let callback: Callback | undefined;

afterEach(() => {
  callback = undefined;
  vi.unstubAllGlobals();
});

describe('SheetThumbnail', () => {
  it('scales the page with a transform and no zoom', async () => {
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        constructor(cb: Callback) {
          callback = cb;
        }

        observe(): void {}
        disconnect(): void {}
      },
    );
    const wrapper = mount(SheetThumbnail, {
      props: { document: sampleResume, lng: 'en', width: 397 },
    });
    callback!([{ isIntersecting: true }]);
    await nextTick();

    const render = wrapper.get('[data-sheet-thumbnail-render]');
    expect(render.attributes('style') ?? '').not.toContain('zoom');
    const content = wrapper.get('.scaled-sheet-content');
    expect(content.attributes('style')).toContain('scale(0.5');
    expect(content.element.contains(render.element)).toBe(true);
  });
});
