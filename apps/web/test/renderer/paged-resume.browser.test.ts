import type { Resume } from '@aboutme/schema';
import { mount, flushPromises } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import { nextTick } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';

const fixture = JSON.parse(
  readFileSync('../../packages/schema/fixtures/minimal.json', 'utf8'),
) as Resume;

const namedFixture = (name: string): Resume => JSON.parse(
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

let resizeCallback: ResizeObserverCallback | undefined;
let observedElements: Element[] = [];
const originalFonts = Object.getOwnPropertyDescriptor(document, 'fonts');

class FakeResizeObserver {
  constructor(callback: ResizeObserverCallback) {
    resizeCallback = callback;
  }

  observe(element: Element): void {
    observedElements.push(element);
  }

  unobserve(): void {}

  disconnect(): void {}
}

afterEach(() => {
  resizeCallback = undefined;
  observedElements = [];
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.body.replaceChildren();
  if (originalFonts === undefined) {
    Reflect.deleteProperty(document, 'fonts');
  } else {
    Object.defineProperty(document, 'fonts', originalFonts);
  }
});

describe('PagedResume browser measurement', () => {
  it('waits for fonts, settles once, and coalesces invalidations', async () => {
    let releaseFonts!: () => void;
    const fontGate = new Promise<void>((resolve) => {
      releaseFonts = resolve;
    });
    const fontEvents = new EventTarget();
    const load = vi.fn(async () => {
      await fontGate;
      return [{} as FontFace];
    });
    Object.assign(fontEvents, { load, ready: Promise.resolve() });
    Object.defineProperty(document, 'fonts', {
      configurable: true,
      value: fontEvents,
    });
    vi.stubGlobal('ResizeObserver', FakeResizeObserver);
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      frames.push(callback);
      return frames.length;
    });
    vi.stubGlobal('cancelAnimationFrame', vi.fn());

    const wrapper = mount(ResumeDocument, {
      attachTo: document.body,
      props: {
        document: structuredClone(fixture),
        context: { lng: 'en', mode: 'paged' },
      },
    });
    const measurement = wrapper.get('.pagination-measurement');
    const header = measurement.get('[data-pagination-header]');
    const rect = vi.spyOn(header.element, 'getBoundingClientRect');

    await nextTick();
    await Promise.resolve();
    expect(load).toHaveBeenCalledTimes(1);
    expect(rect).not.toHaveBeenCalled();
    expect(wrapper.attributes('data-pagination-settled')).toBeUndefined();

    releaseFonts();
    await flushPromises();
    expect(load).toHaveBeenCalledTimes(2);
    expect(rect).toHaveBeenCalled();
    expect(wrapper.attributes('data-pagination-settled')).toBe('true');

    fontEvents.dispatchEvent(new Event('loadingdone'));
    resizeCallback?.(
      [
        {
          target: header.element,
          contentRect: { width: 1, height: 1 },
        } as ResizeObserverEntry,
      ],
      {} as ResizeObserver,
    );
    expect(frames).toHaveLength(1);
    await nextTick();
    expect(wrapper.attributes('data-pagination-settled')).toBeUndefined();

    frames.shift()?.(0);
    await flushPromises();
    expect(load).toHaveBeenCalledTimes(4);
    expect(wrapper.attributes('data-pagination-settled')).toBe('true');

    wrapper.unmount();
    fontEvents.dispatchEvent(new Event('loadingdone'));
    expect(frames).toHaveLength(0);
  });

  it('discards stale results and runs one queued follow-up', async () => {
    let releaseRemeasure!: () => void;
    const remeasureGate = new Promise<void>((resolve) => {
      releaseRemeasure = resolve;
    });
    const fontEvents = new EventTarget();
    const load = vi.fn(async () => {
      if (load.mock.calls.length > 2) await remeasureGate;
      return [{} as FontFace];
    });
    Object.assign(fontEvents, { load, ready: Promise.resolve() });
    Object.defineProperty(document, 'fonts', {
      configurable: true,
      value: fontEvents,
    });
    vi.stubGlobal('ResizeObserver', FakeResizeObserver);
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      frames.push(callback);
      return frames.length;
    });
    vi.stubGlobal('cancelAnimationFrame', vi.fn());
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect')
      .mockImplementation(function () {
        return layoutRect(
          this.dataset.paginationBlockIndex === undefined ? 40 : 20,
        );
      });

    const initialDocument = namedFixture('draft-partial');
    const wrapper = mount(ResumeDocument, {
      attachTo: document.body,
      props: {
        document: initialDocument,
        context: { lng: 'en', mode: 'paged' },
      },
    });
    await flushPromises();
    expect(wrapper.attributes('data-pagination-settled')).toBe('true');

    const stale = structuredClone(initialDocument);
    stale.content.work!.entries[0]!.jobTitle = 'Stale result';
    await wrapper.setProps({ document: stale });
    expect(frames).toHaveLength(1);
    frames.shift()?.(0);
    await Promise.resolve();

    const latest = structuredClone(initialDocument);
    latest.personalDetails.fullName = 'Latest result';
    latest.content = {};
    latest.customization.layout.sections = { main: [], sidebar: [] };
    await wrapper.setProps({ document: latest });
    expect(frames).toHaveLength(0);
    const visibleText = (): string => wrapper
      .findAll('.resume-page:not(.pagination-measurement)')
      .map((page) => page.text())
      .join('');
    expect(visibleText()).toContain('Engineer');
    expect(visibleText()).not.toContain('Stale result');
    expect(visibleText()).not.toContain('Latest result');

    releaseRemeasure();
    await flushPromises();
    expect(frames).toHaveLength(1);
    frames.shift()?.(0);
    await flushPromises();

    expect(load).toHaveBeenCalledTimes(6);
    expect(visibleText()).toContain('Latest result');
    expect(visibleText()).not.toContain('Engineer');
    expect(visibleText()).not.toContain('Stale result');
    expect(wrapper.attributes('data-pagination-settled')).toBe('true');
    const currentHeader = wrapper.get(
      '.pagination-measurement [data-pagination-header]',
    ).element;
    expect(
      observedElements.filter((element) => element === currentHeader),
    ).toHaveLength(2);
    wrapper.unmount();
  });

  it('spaces a split entry\'s parts as the whole entry lays them out',
    async () => {
      const fontEvents = new EventTarget();
      Object.assign(fontEvents, {
        load: vi.fn(async () => [{} as FontFace]),
        ready: Promise.resolve(),
      });
      Object.defineProperty(document, 'fonts', {
        configurable: true,
        value: fontEvents,
      });
      vi.stubGlobal('ResizeObserver', FakeResizeObserver);
      vi.stubGlobal('requestAnimationFrame', vi.fn(() => 1));
      vi.stubGlobal('cancelAnimationFrame', vi.fn());
      // List items of the whole entry sit 26px apart and are 20px tall, so
      // the whole entry leaves 6px between them.
      vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect')
        .mockImplementation(function () {
          if (this.tagName === 'LI') {
            const index = Array.from(this.parentElement!.children)
              .indexOf(this);
            return {
              ...layoutRect(20),
              top: index * 26,
              bottom: (index * 26) + 20,
            };
          }
          return layoutRect(
            this.dataset.paginationBlockIndex === undefined ? 40 : 20,
          );
        });

      const split = namedFixture('draft-partial');
      split.content.work!.entries[0]!.description
        = '<ul><li>First item</li><li>Second item</li></ul>';
      const wrapper = mount(ResumeDocument, {
        attachTo: document.body,
        props: {
          document: split,
          context: { lng: 'en', mode: 'paged' },
        },
      });
      await flushPromises();
      expect(wrapper.attributes('data-pagination-settled')).toBe('true');

      const whole = wrapper.findAll(
        '.pagination-measurement [data-pagination-whole-entry]',
      );
      expect(whole).toHaveLength(1);
      expect(whole[0]!.attributes('data-pagination-whole-entry')).toBe('1');
      expect(whole[0]!.findAll('li')).toHaveLength(2);

      const parts = wrapper.findAll(
        '.resume-page:not(.pagination-measurement) '
        + '.pagination-atomic[data-block-kind="entry"]',
      );
      expect(parts).toHaveLength(2);
      expect(parts[1]!.text()).toContain('Second item');
      expect((parts[1]!.element as HTMLElement).style.marginBlockStart)
        .toBe('6px');
      expect(wrapper.findAll(
        '.resume-page:not(.pagination-measurement) '
        + '[data-pagination-whole-entry]',
      )).toHaveLength(0);
      wrapper.unmount();
    });

  it('lays out a sidebar column\'s split entry whole for measurement only',
    async () => {
      const fontEvents = new EventTarget();
      Object.assign(fontEvents, {
        load: vi.fn(async () => [{} as FontFace]),
        ready: Promise.resolve(),
      });
      Object.defineProperty(document, 'fonts', {
        configurable: true,
        value: fontEvents,
      });
      vi.stubGlobal('ResizeObserver', FakeResizeObserver);
      vi.stubGlobal('requestAnimationFrame', vi.fn(() => 1));
      vi.stubGlobal('cancelAnimationFrame', vi.fn());
      vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect')
        .mockImplementation(function () {
          if (this.tagName === 'LI') {
            const index = Array.from(this.parentElement!.children)
              .indexOf(this);
            return {
              ...layoutRect(20),
              top: index * 26,
              bottom: (index * 26) + 20,
            };
          }
          return layoutRect(
            this.dataset.paginationBlockIndex === undefined ? 40 : 20,
          );
        });

      // work stays whole in the main column; the split entry lives in the
      // sidebar's certificate section instead.
      const sidebarSplit = namedFixture('draft-partial');
      sidebarSplit.customization.layout.columns = 2;
      sidebarSplit.customization.layout.sections
        = { main: ['work'], sidebar: ['certificate'] };
      sidebarSplit.content.certificate = {
        sectionType: 'certificate',
        displayName: 'Certifications',
        entries: [{
          id: '11111111-1111-1111-1111-111111111111',
          title: 'Certified Analytical Engineer',
          description: '<ul><li>First item</li><li>Second item</li></ul>',
        }],
      };
      const wrapper = mount(ResumeDocument, {
        attachTo: document.body,
        props: {
          document: sidebarSplit,
          context: { lng: 'en', mode: 'paged' },
        },
      });
      await flushPromises();
      expect(wrapper.attributes('data-pagination-settled')).toBe('true');

      const sidebarWhole = wrapper.findAll(
        '.pagination-measurement .resume-sidebar [data-pagination-whole-entry]',
      );
      expect(sidebarWhole).toHaveLength(1);
      // Blocks measure in order: the main heading and its whole work entry,
      // then the sidebar heading and this entry's first part.
      expect(sidebarWhole[0]!.attributes('data-pagination-whole-entry'))
        .toBe('3');
      expect(sidebarWhole[0]!.findAll('li')).toHaveLength(2);
      expect(wrapper.findAll(
        '.pagination-measurement .resume-main [data-pagination-whole-entry]',
      )).toHaveLength(0);

      expect(wrapper.findAll(
        '.resume-page:not(.pagination-measurement) '
        + '[data-pagination-whole-entry]',
      )).toHaveLength(0);
      wrapper.unmount();
    });

  it('settles under zoom and reacts to a real resize', async () => {
    const fontEvents = new EventTarget();
    const load = vi.fn(async () => [{} as FontFace]);
    Object.assign(fontEvents, { load, ready: Promise.resolve() });
    Object.defineProperty(document, 'fonts', {
      configurable: true,
      value: fontEvents,
    });
    vi.stubGlobal('ResizeObserver', FakeResizeObserver);
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal(
      'requestAnimationFrame',
      (callback: FrameRequestCallback) => {
        frames.push(callback);
        return frames.length;
      },
    );
    vi.stubGlobal('cancelAnimationFrame', vi.fn());
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect')
      .mockImplementation(function () {
        return layoutRect(
          this.dataset.paginationBlockIndex === undefined ? 40 : 20,
        );
      });

    const wrapper = mount(ResumeDocument, {
      attachTo: document.body,
      props: {
        document: structuredClone(fixture),
        context: { lng: 'en', mode: 'paged' },
      },
    });
    await flushPromises();
    expect(wrapper.attributes('data-pagination-settled')).toBe('true');

    const header = wrapper.get(
      '.pagination-measurement [data-pagination-header]',
    ).element;
    // A CSS zoom ancestor makes the border box (what bindObserver used to
    // seed from) read differently than the content box the ResizeObserver
    // callback reports. Every newly observed element delivers once
    // regardless of a real change, so this first delivery for `header`
    // carries a "zoomed" content box unlike its own border box below.
    vi.spyOn(header, 'getBoundingClientRect')
      .mockReturnValue(layoutRect(33.6));
    resizeCallback?.(
      [
        {
          target: header,
          contentRect: { width: 100, height: 40 },
        } as ResizeObserverEntry,
      ],
      {} as ResizeObserver,
    );
    await nextTick();
    expect(frames).toHaveLength(0);
    expect(wrapper.attributes('data-pagination-settled')).toBe('true');

    // A later, real size change on the same element still re-paginates.
    resizeCallback?.(
      [
        {
          target: header,
          contentRect: { width: 100, height: 60 },
        } as ResizeObserverEntry,
      ],
      {} as ResizeObserver,
    );
    expect(frames).toHaveLength(1);
    await nextTick();
    expect(wrapper.attributes('data-pagination-settled')).toBeUndefined();

    frames.shift()?.(0);
    await flushPromises();
    expect(wrapper.attributes('data-pagination-settled')).toBe('true');

    wrapper.unmount();
  });
});
