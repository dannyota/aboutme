// @vitest-environment jsdom

import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { nextTick } from 'vue';

import { TEMPLATES } from '@aboutme/schema/templates';

import TemplateThumbnail from
  '../../app/components/editor/templates/TemplateThumbnail.vue';

type Callback = (entries: { isIntersecting: boolean }[]) => void;

const observers: { callback: Callback; disconnect: () => void }[] = [];

function stubObserver(): void {
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      readonly disconnect = vi.fn();
      constructor(callback: Callback) {
        observers.push({ callback, disconnect: this.disconnect });
      }

      observe(): void {}
    },
  );
}

afterEach(() => {
  observers.length = 0;
  vi.unstubAllGlobals();
});

describe('TemplateThumbnail', () => {
  it('renders the sample only while its card is near view', async () => {
    stubObserver();
    const wrapper = mount(TemplateThumbnail, {
      props: { preset: TEMPLATES[0]! },
    });
    const thumbnail = wrapper.get('[data-template-thumbnail]');
    expect(thumbnail.attributes('aria-hidden')).toBe('true');
    expect(thumbnail.attributes('inert')).toBeDefined();
    expect(wrapper.find('[data-template-thumbnail-render]').exists()).toBe(
      false,
    );

    observers[0]!.callback([{ isIntersecting: true }]);
    await nextTick();
    expect(wrapper.get('[data-template-thumbnail-render]').text()).toContain(
      'Danny',
    );

    observers[0]!.callback([{ isIntersecting: false }]);
    await nextTick();
    expect(wrapper.find('[data-template-thumbnail-render]').exists()).toBe(
      false,
    );

    wrapper.unmount();
    expect(observers[0]!.disconnect).toHaveBeenCalled();
  });

  it('scales the sample with a transform and no zoom', async () => {
    stubObserver();
    const wrapper = mount(TemplateThumbnail, {
      props: { preset: TEMPLATES[0]! },
    });
    observers[0]!.callback([{ isIntersecting: true }]);
    await nextTick();
    const render = wrapper.get('[data-template-thumbnail-render]');
    expect(render.attributes('style') ?? '').not.toContain('zoom');
    const content = wrapper.get('.scaled-sheet-content');
    expect(content.attributes('style')).toContain('scale(0.18)');
    expect(content.element.contains(render.element)).toBe(true);
  });

  it('applies each template to the sample', async () => {
    stubObserver();
    const renders = new Set<string>();
    for (const preset of TEMPLATES.slice(0, 4)) {
      const wrapper = mount(TemplateThumbnail, { props: { preset } });
      observers.at(-1)!.callback([{ isIntersecting: true }]);
      await nextTick();
      renders.add(wrapper.get('[data-template-thumbnail-render]').html());
      wrapper.unmount();
    }
    expect(renders.size).toBe(4);
  });

  it('keeps a blank page when the browser cannot observe', () => {
    vi.stubGlobal('IntersectionObserver', undefined);
    const wrapper = mount(TemplateThumbnail, {
      props: { preset: TEMPLATES[0]! },
    });
    expect(wrapper.find('[data-template-thumbnail]').exists()).toBe(true);
    expect(wrapper.find('[data-template-thumbnail-render]').exists()).toBe(
      false,
    );
  });
});
