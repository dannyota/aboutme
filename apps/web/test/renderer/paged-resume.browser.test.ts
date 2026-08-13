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
});
