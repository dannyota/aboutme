// @vitest-environment happy-dom

import type { Resume } from '@aboutme/schema';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import { afterEach, describe, expect, it, vi } from 'vitest';

import PublicPages from '../../app/components/public/PublicPages.vue';

const fixture = (name: string): Resume => JSON.parse(
  readFileSync(`../../packages/schema/fixtures/${name}.json`, 'utf8'),
) as Resume;

const rect = (height: number): DOMRect => ({
  x: 0, y: 0, width: 100, height, top: 0, right: 100, bottom: height,
  left: 0, toJSON: () => ({}),
});

class FakeObserver {
  observe(): void {}

  unobserve(): void {}

  disconnect(): void {}
}

let frames: FrameRequestCallback[] = [];
let fontsLoad: () => Promise<FontFace[]> = async () => [{} as FontFace];
let phone = true;
let blockHeight = 20;
const mounted: VueWrapper[] = [];
const originalFonts = Object.getOwnPropertyDescriptor(document, 'fonts');

const stubBrowser = (): void => {
  const fontEvents = new EventTarget();
  Object.assign(fontEvents, {
    load: vi.fn(() => fontsLoad()),
    ready: Promise.resolve(),
  });
  Object.defineProperty(document, 'fonts', {
    configurable: true,
    value: fontEvents,
  });
  vi.stubGlobal('ResizeObserver', FakeObserver);
  vi.stubGlobal('IntersectionObserver', FakeObserver);
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    frames.push(callback);
    return frames.length;
  });
  vi.stubGlobal('cancelAnimationFrame', vi.fn());
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query.includes('600px') ? phone : false,
    media: query,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  Object.defineProperty(document.documentElement, 'clientWidth', {
    configurable: true,
    value: phone ? 390 : 1_440,
  });
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect')
    .mockImplementation(function (this: HTMLElement) {
      return rect(this.dataset.paginationBlockIndex === undefined
        ? 40
        : blockHeight);
    });
};

const runFrames = async (): Promise<void> => {
  for (let round = 0; round < 4; round += 1) {
    await flushPromises();
    const pending = frames.splice(0);
    for (const callback of pending) callback(0);
  }
  await flushPromises();
};

const mountPages = (document: Resume, lng = 'en') => {
  const wrapper = mount(PublicPages, {
    attachTo: window.document.body,
    props: {
      document,
      context: {
        lng,
        mode: 'paged',
        ...(document.personalDetails.photo === undefined
          ? {}
          : { photoUrl: 'data:image/png;base64,AA==' }),
      },
    },
  });
  mounted.push(wrapper);
  return wrapper;
};

const states = (wrapper: VueWrapper) =>
  (wrapper.emitted('state') ?? []).map(([state]) => state);

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
  frames = [];
  fontsLoad = async () => [{} as FontFace];
  phone = true;
  blockHeight = 20;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.body.replaceChildren();
  document.head.replaceChildren();
  if (originalFonts === undefined) {
    Reflect.deleteProperty(document, 'fonts');
  } else {
    Object.defineProperty(document, 'fonts', originalFonts);
  }
});

describe('PublicPages', () => {
  it('mounts the paged tree only after the template fonts load', async () => {
    stubBrowser();
    let release!: (faces: FontFace[]) => void;
    fontsLoad = () => new Promise((resolve) => {
      release = resolve;
    });
    const wrapper = mountPages(fixture('minimal'));
    await flushPromises();
    expect(wrapper.find('.paged-resume').exists()).toBe(false);
    release([{} as FontFace]);
    fontsLoad = async () => [{} as FontFace];
    await flushPromises();
    expect(wrapper.find('.paged-resume').exists()).toBe(true);
    expect(states(wrapper)).toContain('staging');
  });

  it('stages hidden, inert and aria-hidden, never display none', async () => {
    stubBrowser();
    const wrapper = mountPages(fixture('minimal'));
    await flushPromises();
    const staged = wrapper.get('.public-pages');
    expect(staged.attributes('data-state')).toBe('staging');
    expect(staged.attributes('inert')).toBeDefined();
    expect(staged.attributes('aria-hidden')).toBe('true');
    expect(staged.attributes('style') ?? '').not.toContain('display');
  });

  it('swaps one frame after settled and names each page', async () => {
    stubBrowser();
    blockHeight = 700;
    const wrapper = mountPages(fixture('full'));
    await runFrames();
    expect(states(wrapper).at(-1)).toBe('shown');
    const pages = wrapper.get('.public-pages');
    expect(pages.attributes('inert')).toBeUndefined();
    expect(pages.attributes('aria-hidden')).toBeUndefined();
    const sections = wrapper.findAll('section.public-page');
    expect(sections.length).toBeGreaterThan(1);
    const count = sections.length;
    expect(sections.map((section) => section.attributes('aria-label')))
      .toEqual(sections.map((_, index) => `Page ${index + 1} of ${count}`));
    const position = sections[0]!.get('.public-page-position');
    expect(position.attributes('aria-hidden')).toBe('true');
    expect(position.text()).toBe(`1 / ${count}`);
    expect(sections[0]!.find('.public-page-sheet .public-page-scale '
      + 'article.resume-page').exists()).toBe(true);
  });

  it('scales each sheet into a box sized to the scaled page', async () => {
    stubBrowser();
    const wrapper = mountPages(fixture('minimal'));
    await runFrames();
    const scale = 358 / 794;
    const sheet = wrapper.get('.public-page-sheet');
    const width = Number.parseFloat(
      (sheet.element as HTMLElement).style.width,
    );
    expect(width).toBeCloseTo(358, 3);
    const inner = wrapper.get('.public-page-scale').element as HTMLElement;
    expect(inner.style.transform).toBe(`scale(${scale})`);
    expect(inner.style.width).toBe('794px');
  });

  it('names the scroller and the pager in English and Vietnamese',
    async () => {
      stubBrowser();
      blockHeight = 700;
      const en = mountPages(fixture('full'));
      await runFrames();
      const scroller = en.get('.paged-resume');
      expect(scroller.attributes('role')).toBe('region');
      expect(scroller.attributes('aria-label')).toBe('Resume pages');
      expect(scroller.attributes('tabindex')).toBe('0');
      const [previous, next] = en.findAll('.public-pager button');
      expect(previous!.attributes('aria-label')).toBe('Previous page');
      expect(previous!.attributes('disabled')).toBeDefined();
      expect(next!.attributes('aria-label')).toBe('Next page');
      expect(next!.attributes('disabled')).toBeUndefined();
      const count = en.findAll('section.public-page').length;
      expect(en.get('[aria-live="polite"]').text())
        .toBe(`Page 1 of ${count}`);
      expect(en.get('.public-pager-status').attributes('aria-hidden'))
        .toBe('true');
      expect(en.findAll('.public-pager-dot')).toHaveLength(count);

      const vi_ = mountPages(fixture('vn-full'), 'vi');
      await runFrames();
      expect(vi_.get('.paged-resume').attributes('aria-label'))
        .toBe('Các trang CV');
      const [back, forward] = vi_.findAll('.public-pager button');
      expect(back!.attributes('aria-label')).toBe('Trang trước');
      expect(forward!.attributes('aria-label')).toBe('Trang sau');
      const total = vi_.findAll('section.public-page').length;
      expect(vi_.findAll('section.public-page')[0]!.attributes('aria-label'))
        .toBe(`Trang 1/${total}`);
    });

  it('shows no pager for a one-page resume', async () => {
    stubBrowser();
    const wrapper = mountPages(fixture('minimal'));
    await runFrames();
    expect(wrapper.findAll('section.public-page')).toHaveLength(1);
    expect(wrapper.find('.public-pager').exists()).toBe(false);
  });

  it('leaves the scroller out of the tab order on a wide screen',
    async () => {
      phone = false;
      stubBrowser();
      const wrapper = mountPages(fixture('minimal'));
      await runFrames();
      expect(wrapper.get('.paged-resume').attributes('tabindex'))
        .toBeUndefined();
      const sheet = wrapper.get('.public-page-sheet').element as HTMLElement;
      expect(sheet.style.width).toBe('794px');
    });

  it('moves with the arrow keys and prevents the browser scroll',
    async () => {
      stubBrowser();
      blockHeight = 700;
      const wrapper = mountPages(fixture('full'));
      await runFrames();
      const scroller = wrapper.get('.paged-resume').element as HTMLElement;
      const scrollTo = vi.fn();
      scroller.scrollTo = scrollTo as typeof scroller.scrollTo;
      const changes = vi.fn();
      window.document.body.addEventListener('public-page-change', changes);
      const right = new KeyboardEvent('keydown', {
        key: 'ArrowRight', bubbles: true, cancelable: true,
      });
      scroller.dispatchEvent(right);
      expect(right.defaultPrevented).toBe(true);
      expect(scrollTo).toHaveBeenCalledTimes(1);
      await flushPromises();
      expect(changes).toHaveBeenCalledTimes(1);
      expect(wrapper.get('.public-pages').attributes('data-page-index'))
        .toBe('1');
      const left = new KeyboardEvent('keydown', {
        key: 'ArrowLeft', bubbles: true, cancelable: true,
      });
      scroller.dispatchEvent(left);
      expect(left.defaultPrevented).toBe(true);
    });

  it('unmounts the paged tree and reports a failed run', async () => {
    stubBrowser();
    fontsLoad = async () => [{} as FontFace];
    const wrapper = mountPages(fixture('minimal'));
    await flushPromises();
    fontsLoad = async () => [];
    await wrapper.setProps({
      document: { ...fixture('minimal'), personalDetails: {
        ...fixture('minimal').personalDetails, fullName: 'Grace Hopper',
      } },
    });
    await runFrames();
    expect(states(wrapper).at(-1)).toBe('continuous');
    expect(wrapper.find('.paged-resume').exists()).toBe(false);
  });

  it('returns focus to the scroller after a live update only when it was lost from inside the pages',
    async () => {
      stubBrowser();
      blockHeight = 700;
      const document = fixture('full');
      const wrapper = mountPages(document);
      await runFrames();
      const scroller = wrapper.get('.paged-resume').element as HTMLElement;

      (window.document.activeElement as HTMLElement | null)?.blur();
      expect(window.document.activeElement).toBe(window.document.body);
      const updated = structuredClone(document);
      updated.personalDetails.fullName = 'Grace Hopper';
      await wrapper.setProps({ document: updated });
      await runFrames();
      expect(states(wrapper).at(-1)).toBe('shown');
      expect(window.document.activeElement).not.toBe(scroller);

      const pagerButton = wrapper.findAll('.public-pager button')[1]!
        .element as HTMLElement;
      pagerButton.focus();
      expect(window.document.activeElement).toBe(pagerButton);
      const again = structuredClone(updated);
      again.personalDetails.fullName = 'Ada Lovelace';
      await wrapper.setProps({ document: again });
      await runFrames();
      expect(states(wrapper).at(-1)).toBe('shown');
      expect(window.document.activeElement).toBe(pagerButton);
      expect(window.document.activeElement).not.toBe(scroller);

      pagerButton.focus();
      const lost = structuredClone(again);
      lost.personalDetails.fullName = 'Hedy Lamarr';
      await wrapper.setProps({ document: lost });
      pagerButton.blur();
      expect(window.document.activeElement).toBe(window.document.body);
      await runFrames();
      expect(states(wrapper).at(-1)).toBe('shown');
      expect(window.document.activeElement).toBe(scroller);
    });

  it('keeps the page index across a live update, clamped to the new count',
    async () => {
      stubBrowser();
      blockHeight = 700;
      const document = fixture('full');
      const wrapper = mountPages(document);
      await runFrames();
      const count = wrapper.findAll('section.public-page').length;
      const next = wrapper.findAll('.public-pager button')[1]!;
      await next.trigger('click');
      await runFrames();
      expect(wrapper.get('.public-pager-position').text())
        .toBe(`2 / ${count}`);
      const updated = structuredClone(document);
      updated.personalDetails.fullName = 'Grace Hopper';
      await wrapper.setProps({ document: updated });
      expect(states(wrapper).at(-1)).toBe('updating');
      await runFrames();
      expect(states(wrapper).at(-1)).toBe('shown');
      expect(wrapper.get('.public-pager-position').text())
        .toBe(`2 / ${count}`);
      expect(wrapper.get('.public-page .resume-name').text())
        .toBe('Grace Hopper');

      blockHeight = 20;
      const shorter = structuredClone(updated);
      shorter.personalDetails.fullName = 'Short';
      await wrapper.setProps({ document: shorter });
      await runFrames();
      expect(wrapper.findAll('section.public-page')).toHaveLength(1);
      expect(wrapper.find('.public-pager').exists()).toBe(false);
    });

  it('inserts the @page rule and replaces it when the format changes',
    async () => {
      stubBrowser();
      const document = fixture('minimal');
      document.customization.pageFormat = 'a4';
      const wrapper = mountPages(document);
      await flushPromises();
      const rule = () => window.document.head
        .querySelector('style[data-public-page-rule]')?.textContent;
      expect(rule()).toContain('size: 210mm 297mm;');
      await wrapper.setProps({
        document: {
          ...document,
          customization: { ...document.customization, pageFormat: 'letter' },
        },
      });
      await flushPromises();
      expect(rule()).toContain('size: 8.5in 11in;');
      expect(window.document.head
        .querySelectorAll('style[data-public-page-rule]')).toHaveLength(1);
      wrapper.unmount();
      mounted.splice(mounted.indexOf(wrapper), 1);
      expect(rule()).toBeUndefined();
    });
});
