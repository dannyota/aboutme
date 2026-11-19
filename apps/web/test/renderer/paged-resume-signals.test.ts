import type { Resume } from '@aboutme/schema';
import { flushPromises, mount } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import { h, type VNode } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import PagedResume from '../../app/components/resume/PagedResume.vue';
import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';

const fixture = (name: string): Resume => JSON.parse(
  readFileSync(`../../packages/schema/fixtures/${name}.json`, 'utf8'),
) as Resume;

const layoutRect = (height: number): DOMRect => ({
  x: 0,
  y: 0,
  width: 100,
  height,
  top: 0,
  right: 100,
  bottom: height,
  left: 0,
  toJSON: () => ({}),
});

class FakeResizeObserver {
  observe(): void {}

  unobserve(): void {}

  disconnect(): void {}
}

const originalFonts = Object.getOwnPropertyDescriptor(document, 'fonts');
let frames: FrameRequestCallback[] = [];

const stubBrowser = (load: () => Promise<FontFace[]>): void => {
  const fontEvents = new EventTarget();
  Object.assign(fontEvents, { load: vi.fn(load), ready: Promise.resolve() });
  Object.defineProperty(document, 'fonts', {
    configurable: true,
    value: fontEvents,
  });
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    frames.push(callback);
    return frames.length;
  });
  vi.stubGlobal('cancelAnimationFrame', vi.fn());
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect')
    .mockImplementation(function (this: HTMLElement) {
      return layoutRect(
        this.dataset.paginationBlockIndex === undefined ? 40 : 20,
      );
    });
};

const loaded = async (): Promise<FontFace[]> => [{} as FontFace];

afterEach(() => {
  frames = [];
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.body.replaceChildren();
  if (originalFonts === undefined) {
    Reflect.deleteProperty(document, 'fonts');
  } else {
    Object.defineProperty(document, 'fonts', originalFonts);
  }
});

describe('PagedResume signals', () => {
  it('emits settled with the page count once pagination settles',
    async () => {
      stubBrowser(loaded);
      const wrapper = mount(PagedResume, {
        attachTo: document.body,
        props: {
          document: fixture('minimal'),
          context: { lng: 'en', mode: 'paged' },
        },
      });
      await flushPromises();

      expect(wrapper.attributes('data-pagination-settled')).toBe('true');
      expect(wrapper.emitted('settled')).toEqual([[1]]);
      expect(wrapper.emitted('failed')).toBeUndefined();
      wrapper.unmount();
    });

  it('catches a failed first run, reports it as failed and stays unsettled',
    async () => {
      const warn = vi.spyOn(console, 'warn');
      stubBrowser(async () => []);
      const wrapper = mount(PagedResume, {
        attachTo: document.body,
        props: {
          document: fixture('minimal'),
          context: { lng: 'en', mode: 'paged' },
        },
      });
      await flushPromises();

      const failed = wrapper.emitted('failed');
      expect(failed).toHaveLength(1);
      expect(failed?.[0]?.[0]).toBeInstanceOf(Error);
      expect(String(failed?.[0]?.[0])).toContain('nothing loaded');
      expect(wrapper.emitted('settled')).toBeUndefined();
      expect(wrapper.attributes('data-pagination-settled')).toBeUndefined();
      expect(warn.mock.calls.flat().join('\n'))
        .not.toContain('Unhandled error');
      wrapper.unmount();
    });

  it('reports a failed later run and keeps the last settled pages',
    async () => {
      let fail = false;
      stubBrowser(async () => fail ? [] : [{} as FontFace]);
      const first = fixture('draft-partial');
      const wrapper = mount(PagedResume, {
        attachTo: document.body,
        props: { document: first, context: { lng: 'en', mode: 'paged' } },
      });
      await flushPromises();
      expect(wrapper.emitted('settled')).toEqual([[1]]);

      fail = true;
      const next = structuredClone(first);
      next.personalDetails.fullName = 'Next revision';
      await wrapper.setProps({ document: next });
      expect(frames).toHaveLength(1);
      frames.shift()?.(0);
      await flushPromises();

      expect(wrapper.emitted('failed')).toHaveLength(1);
      expect(wrapper.emitted('settled')).toHaveLength(1);
      expect(wrapper.attributes('data-pagination-settled')).toBeUndefined();
      const visible = wrapper.findAll(
        '.resume-page:not(.pagination-measurement)',
      );
      expect(visible).toHaveLength(1);
      expect(visible[0]!.text()).toContain('Engineer');
      expect(visible[0]!.text()).not.toContain('Next revision');
      wrapper.unmount();
    });

  it('passes the signals through ResumeDocument in paged mode', async () => {
    stubBrowser(async () => []);
    const wrapper = mount(ResumeDocument, {
      attachTo: document.body,
      props: {
        document: fixture('minimal'),
        context: { lng: 'en', mode: 'paged' },
      },
    });
    await flushPromises();
    expect(wrapper.emitted('failed')).toHaveLength(1);
    wrapper.unmount();
  });
});

interface PageSlot {
  readonly index: number;
  readonly count: number;
  readonly widthPx: number;
  readonly heightPx: number;
  readonly page: VNode;
}

describe('PagedResume page slot', () => {
  it('wraps each settled page with its position and its page size',
    async () => {
      stubBrowser(loaded);
      const document = fixture('draft-partial');
      document.customization.pageFormat = 'letter';
      const wrapper = mount(PagedResume, {
        attachTo: globalThis.document.body,
        props: { document, context: { lng: 'en', mode: 'paged' } },
        slots: {
          page: (slot: PageSlot) => h('section', {
            'class': 'probe-page',
            'data-position': `${slot.index + 1} / ${slot.count}`,
            'data-size': `${slot.widthPx}x${slot.heightPx}`,
          }, [slot.page]),
        },
      });
      await flushPromises();

      const wrapped = wrapper.findAll('section.probe-page');
      expect(wrapped.map((section) => [
        section.attributes('data-position'),
        section.attributes('data-size'),
        section.findAll('article.resume-page').length,
      ])).toEqual([['1 / 1', '816x1056', 1]]);
      expect(wrapped[0]!.get('article.resume-page').attributes(
        'data-page-index',
      )).toBe('0');
      expect(wrapper.find('.paged-resume > article.resume-page:not('
        + '.pagination-measurement)').exists()).toBe(false);
      expect(wrapper.find('.paged-resume > .pagination-measurement').exists())
        .toBe(true);
      expect(wrapper.find('section.probe-page .pagination-measurement')
        .exists()).toBe(false);
      wrapper.unmount();
    });

  it('leaves the pages bare without slot content', async () => {
    stubBrowser(loaded);
    const wrapper = mount(PagedResume, {
      attachTo: document.body,
      props: {
        document: fixture('draft-partial'),
        context: { lng: 'en', mode: 'paged' },
      },
    });
    await flushPromises();
    const pages = wrapper.findAll('article.resume-page');
    expect(pages).toHaveLength(2);
    for (const page of pages) {
      expect(page.element.parentElement).toBe(wrapper.element);
    }
    wrapper.unmount();
  });
});
