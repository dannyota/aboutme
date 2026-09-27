import { mount } from '@vue/test-utils';
import { defineComponent, nextTick } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import EditorPreview from '../../app/components/editor/EditorPreview.vue';
import { PREVIEW_ZOOM_STORAGE_KEY } from '../../app/editor/previewZoom';
import { editorShellCopy } from '../../app/i18n/editor-shell';
import { acceptedFixture } from './fixture';
import { setSiteLocale } from '../support/locale';

const WIDE_LAYOUT_WIDTH = 1440;
const PHONE_LAYOUT_WIDTH = 390;

function setInnerWidth(width: number): void {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: width,
  });
}

// This environment's WheelEvent constructor ignores the MouseEvent-derived
// ctrlKey/clientX/clientY init fields, so they are set directly afterward.
function ctrlWheelEvent(
  deltaY: number,
  point: { x: number; y: number },
): WheelEvent {
  const event = new WheelEvent('wheel', {
    bubbles: true, cancelable: true, deltaY,
  });
  Object.assign(event, {
    clientX: point.x, clientY: point.y, ctrlKey: true,
  });
  return event;
}

async function mountPreviewAtWidth(width: number) {
  setInnerWidth(width);
  const accepted = acceptedFixture();
  const wrapper = mount(EditorPreview, {
    props: { document: accepted.document, lng: accepted.metadata.lng },
    global: { stubs: { ResumeDocument: true } },
  });
  await nextTick();
  return wrapper;
}

const resize = vi.hoisted(() => ({
  callback: null as ResizeObserverCallback | null,
}));
const initialWindowWidth = window.innerWidth;

class ResizeObserverMock {
  constructor(callback: ResizeObserverCallback) {
    resize.callback = callback;
  }

  disconnect = vi.fn();
  observe = vi.fn();
  unobserve = vi.fn();
}

afterEach(() => {
  resize.callback = null;
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: initialWindowWidth,
  });
  vi.unstubAllGlobals();
  vi.useRealTimers();
  window.localStorage.clear();
});

beforeEach(() => {
  setSiteLocale('en');
  window.localStorage.clear();
});

describe('EditorPreview', () => {
  it(
    'localizes the preview failure without changing its resume language',
    async () => {
      setSiteLocale('vi');
      const accepted = acceptedFixture();
      const wrapper = mount(EditorPreview, {
        props: { document: accepted.document, lng: 'en' },
        global: {
          stubs: {
            ResumeDocument: defineComponent({
              setup() { throw new Error('renderer failed'); },
              template: '<div />',
            }),
          },
        },
      });
      await nextTick();

      expect(wrapper.get('[role="status"]').text()).toContain(
        'Bản xem trước tạm thời không khả dụng.',
      );
      wrapper.unmount();
    },
  );

  it('restarts page counting when a hidden phone preview becomes active',
    async () => {
      const host = document.createElement('div');
      host.style.visibility = 'hidden';
      document.body.append(host);
      const accepted = acceptedFixture();
      const wrapper = mount(EditorPreview, {
        attachTo: host,
        props: {
          active: false,
          document: accepted.document,
          lng: accepted.metadata.lng,
        },
        global: {
          stubs: {
            ResumeDocument: defineComponent({
              template: [
                '<article class="resume-page" ',
                'data-page-index="0"></article>',
              ].join(''),
            }),
          },
        },
      });

      await animationFrames(3);
      expect(wrapper.get('[data-testid="page-count"]').text())
        .toContain('— pages');

      host.style.visibility = 'visible';
      await wrapper.setProps({ active: true });
      await animationFrames(3);
      expect(wrapper.get('[data-testid="page-count"]').text())
        .toContain('1 page');

      wrapper.unmount();
      host.remove();
    });

  it(
    'renders the page-count mark under the sheet, with only the mode '
    + 'switch in a toolbar above it',
    () => {
      const accepted = acceptedFixture();
      const wrapper = mount(EditorPreview, {
        props: {
          document: accepted.document,
          lng: accepted.metadata.lng,
        },
        global: { stubs: { ResumeDocument: true } },
      });

      expect(wrapper.find('[data-preview-header]').exists()).toBe(false);
      expect(wrapper.get('[data-testid="preview-toolbar"]').text()).toBe(
        wrapper.get('[data-testid="preview-mode-switch"]').text(),
      );
      expect(wrapper.get('[data-testid="page-count"]').text()).toMatch(
        /— pages/,
      );
      expect(wrapper.get('[data-testid="preview-sheet"]').classes()).toEqual(
        expect.arrayContaining([
          'rounded-[var(--radius-sheet)]',
          'shadow-[var(--shadow-paper)]',
          'bg-white',
        ]),
      );
    },
  );

  it('defaults the preview to PDF mode', () => {
    const accepted = acceptedFixture();
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: accepted.metadata.lng,
      },
      global: { stubs: { ResumeDocument: true } },
    });

    const pdfButton = wrapper.get('[data-mode="pdf"]');
    const webButton = wrapper.get('[data-mode="web"]');
    expect(pdfButton.attributes('aria-pressed')).toBe('true');
    expect(webButton.attributes('aria-pressed')).toBe('false');
    expect(pdfButton.text()).toBe(editorShellCopy.en.previewModePdf);
    expect(webButton.text()).toBe(editorShellCopy.en.previewModeWeb);
    expect(
      wrapper.getComponent({ name: 'ResumeDocument' }).props('context'),
    ).toEqual({ lng: 'en', mode: 'paged', nameHeading: 'p' });
  });

  it('scales the PDF sheet with a transform, not CSS zoom', () => {
    const accepted = acceptedFixture();
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: accepted.metadata.lng,
      },
      global: { stubs: { ResumeDocument: true } },
    });

    const sheet = wrapper.get('[data-testid="preview-sheet"]');
    expect(sheet.attributes('style') ?? '').not.toContain('zoom');
    const scaledContent = sheet.get('.scaled-sheet-content');
    const zoom = Number(sheet.attributes('data-sheet-zoom'));
    const transformStyle = scaledContent.attributes('style') ?? '';
    const match = /scale\(([^)]+)\)/u.exec(transformStyle);
    expect(match).not.toBeNull();
    expect(Number(match?.[1])).toBeCloseTo(zoom, 4);
  });

  it(
    'switches to the continuous Web document and hides the paged sheet mark',
    async () => {
      const accepted = acceptedFixture();
      const wrapper = mount(EditorPreview, {
        props: {
          document: accepted.document,
          lng: accepted.metadata.lng,
        },
        global: { stubs: { ResumeDocument: true } },
      });

      await wrapper.get('[data-mode="web"]').trigger('click');

      expect(wrapper.get('[data-mode="web"]').attributes('aria-pressed'))
        .toBe('true');
      expect(wrapper.get('[data-mode="pdf"]').attributes('aria-pressed'))
        .toBe('false');
      expect(
        wrapper.getComponent({ name: 'ResumeDocument' }).props('context'),
      ).toEqual({ lng: 'en', mode: 'continuous', nameHeading: 'p' });
      expect(wrapper.find('[data-testid="page-count"]').exists()).toBe(false);
      const sheet = wrapper.get('[data-testid="preview-sheet"]');
      expect(sheet.attributes('data-sheet-zoom')).toBeUndefined();
      expect(sheet.attributes('data-scaled-width')).toBeUndefined();
      expect(sheet.attributes('style') ?? '').not.toContain('zoom');
      expect(sheet.find('.scaled-sheet').exists()).toBe(false);
    },
  );

  it('remembers a chosen mode across mounts in the same browser', async () => {
    const accepted = acceptedFixture();
    const first = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: accepted.metadata.lng,
      },
      global: { stubs: { ResumeDocument: true } },
    });
    await first.get('[data-mode="web"]').trigger('click');
    first.unmount();

    const second = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: accepted.metadata.lng,
      },
      global: { stubs: { ResumeDocument: true } },
    });
    expect(second.get('[data-mode="web"]').attributes('aria-pressed'))
      .toBe('true');
    expect(
      second.getComponent({ name: 'ResumeDocument' }).props('context'),
    ).toEqual({ lng: 'en', mode: 'continuous', nameHeading: 'p' });
    second.unmount();
  });

  it(
    'falls back to PDF and keeps switching modes when storage throws',
    async () => {
      const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(
        () => { throw new Error('blocked'); },
      );
      const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(
        () => { throw new Error('blocked'); },
      );
      const accepted = acceptedFixture();
      const wrapper = mount(EditorPreview, {
        props: {
          document: accepted.document,
          lng: accepted.metadata.lng,
        },
        global: { stubs: { ResumeDocument: true } },
      });

      expect(wrapper.get('[data-mode="pdf"]').attributes('aria-pressed'))
        .toBe('true');

      await wrapper.get('[data-mode="web"]').trigger('click');
      expect(wrapper.get('[data-mode="web"]').attributes('aria-pressed'))
        .toBe('true');

      getItem.mockRestore();
      setItem.mockRestore();
      wrapper.unmount();
    },
  );

  it('labels the mode switch in Vietnamese too', () => {
    setSiteLocale('vi');
    const accepted = acceptedFixture();
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: accepted.metadata.lng,
      },
      global: { stubs: { ResumeDocument: true } },
    });

    expect(wrapper.get('[data-mode="pdf"]').text()).toBe(
      editorShellCopy.vi.previewModePdf,
    );
    expect(wrapper.get('[data-mode="web"]').text()).toBe(
      editorShellCopy.vi.previewModeWeb,
    );
    expect(
      wrapper.get('[data-testid="preview-mode-switch"]').attributes(
        'aria-label',
      ),
    ).toBe(editorShellCopy.vi.previewMode);
  });

  it('fits the whole A4 sheet inside a 390 px preview', async () => {
    vi.stubGlobal('ResizeObserver', ResizeObserverMock);
    Object.defineProperty(window, 'innerWidth', {
      configurable: true,
      value: 390,
    });
    const accepted = acceptedFixture();
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: accepted.metadata.lng,
      },
      global: { stubs: { ResumeDocument: true } },
    });

    await nextTick();
    resize.callback?.([
      { contentRect: { width: 390 } } as ResizeObserverEntry,
    ], {} as ResizeObserver);
    await nextTick();

    const sheet = wrapper.get('[data-testid="preview-sheet"]');
    expect(Number(sheet.attributes('data-sheet-zoom'))).toBeLessThan(1);
    expect(Number(sheet.attributes('data-scaled-width'))).toBeLessThan(390);
  });

  it('never stamps the sheet, published or not (DESIGN.md seal rules)', () => {
    const accepted = acceptedFixture();
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: accepted.metadata.lng,
      },
      global: { stubs: { ResumeDocument: true } },
    });

    const sheet = wrapper.get('[data-testid="preview-sheet"]');
    expect(sheet.find('[data-app-seal]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="preview-stamp"]').exists()).toBe(
      false,
    );
  });

  it('passes only the optimistic document and paged render context', () => {
    const accepted = acceptedFixture();
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: accepted.metadata.lng,
      },
      global: { stubs: { ResumeDocument: true } },
    });

    const renderer = wrapper.getComponent({ name: 'ResumeDocument' });
    expect(renderer.props('document')).toStrictEqual(accepted.document);
    expect(renderer.props('context')).toEqual({
      lng: 'en',
      mode: 'paged',
      nameHeading: 'p',
    });
  });

  it('renders without the photo while the read is pending', () => {
    const accepted = acceptedFixture();
    accepted.document.personalDetails.photo = {
      key: 'resumes/resume-1/private-object.jpg',
    };
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: 'en',
        photoRead: { kind: 'loading', binding: 'k', generation: 1 },
      },
      global: { stubs: { ResumeDocument: true } },
    });

    const renderer = wrapper.getComponent({ name: 'ResumeDocument' });
    expect(renderer.props('document').personalDetails.photo).toBeUndefined();
    expect(renderer.props('context')).toEqual({
      lng: 'en',
      mode: 'paged',
      nameHeading: 'p',
    });
    expect(wrapper.html()).not.toContain('private-object.jpg');
  });

  it('names the unavailable state and the photo panel', () => {
    const accepted = acceptedFixture();
    accepted.document.personalDetails.photo = { key: 'resumes/resume-1/p.jpg' };
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: 'en',
        photoRead: {
          kind: 'suspended',
          binding: 'k',
          generation: 1,
          reason: 'read-failed',
        },
      },
      global: { stubs: { ResumeDocument: true } },
    });

    expect(wrapper.findComponent({ name: 'ResumeDocument' }).exists()).toBe(
      true,
    );
    expect(wrapper.html()).not.toContain('resumes/resume-1/p.jpg');
  });

  it(
    'keeps the safe render notice when the fallback renderer fails',
    async () => {
      const accepted = acceptedFixture();
      accepted.document.personalDetails.photo = {
        key: 'resumes/resume-1/private-object.jpg',
      };
      let projectedPhoto: unknown = 'not-observed';
      const wrapper = mount(EditorPreview, {
        props: {
          document: accepted.document,
          lng: 'en',
          photoRead: { kind: 'loading', binding: 'k', generation: 1 },
        },
        global: {
          stubs: {
            ResumeDocument: defineComponent({
              name: 'ResumeDocument',
              props: { document: { type: Object, required: true } },
              setup(props) {
                projectedPhoto = (
                  props.document as {
                    personalDetails: { photo?: unknown };
                  }
                ).personalDetails.photo;
                throw new Error('renderer failed');
              },
              template: '<div />',
            }),
          },
        },
      });
      await nextTick();

      expect(projectedPhoto).toBeUndefined();
      expect(wrapper.get('[role="status"]').text()).toContain(
        'Preview is temporarily unavailable. Your edits are still safe.',
      );
      expect(wrapper.html()).not.toContain('private-object.jpg');
    },
  );

  it('passes an authorized data URL without exposing the stored key', () => {
    const accepted = acceptedFixture();
    accepted.document.personalDetails.photo = {
      key: 'resumes/resume-1/private-object.jpg',
    };
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: 'en',
        photoUrl: 'data:image/jpeg;base64,AA==',
      },
      global: { stubs: { ResumeDocument: true } },
    });

    expect(
      wrapper.getComponent({ name: 'ResumeDocument' }).props('context'),
    ).toEqual({
      lng: 'en',
      mode: 'paged',
      nameHeading: 'p',
      photoUrl: 'data:image/jpeg;base64,AA==',
    });
    expect(wrapper.html()).not.toContain('private-object.jpg');
  });
});

describe('EditorPreview zoom controls', () => {
  it(
    'shows the zoom controls above the phone breakpoint and hides them at '
    + 'phone width',
    async () => {
      const wide = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      expect(
        wide.find('[data-testid="preview-zoom-controls"]').exists(),
      ).toBe(true);
      wide.unmount();

      const phone = await mountPreviewAtWidth(PHONE_LAYOUT_WIDTH);
      expect(
        phone.find('[data-testid="preview-zoom-controls"]').exists(),
      ).toBe(false);
      phone.unmount();
    },
  );

  it('names the four controls in English', async () => {
    const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);

    expect(wrapper.get('[data-testid="zoom-out"]').attributes('aria-label'))
      .toBe(editorShellCopy.en.zoomOut);
    expect(wrapper.get('[data-testid="zoom-in"]').attributes('aria-label'))
      .toBe(editorShellCopy.en.zoomIn);
    expect(wrapper.get('[data-testid="zoom-fit"]').attributes('aria-label'))
      .toBe(editorShellCopy.en.zoomFit);
    expect(
      wrapper.get('[data-testid="zoom-percent"]').attributes('aria-label'),
    ).toBe(editorShellCopy.en.zoomReset(84));
    wrapper.unmount();
  });

  it('names the four controls in Vietnamese', async () => {
    setSiteLocale('vi');
    const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);

    expect(wrapper.get('[data-testid="zoom-out"]').attributes('aria-label'))
      .toBe(editorShellCopy.vi.zoomOut);
    expect(wrapper.get('[data-testid="zoom-in"]').attributes('aria-label'))
      .toBe(editorShellCopy.vi.zoomIn);
    expect(wrapper.get('[data-testid="zoom-fit"]').attributes('aria-label'))
      .toBe(editorShellCopy.vi.zoomFit);
    expect(
      wrapper.get('[data-testid="zoom-percent"]').attributes('aria-label'),
    ).toBe(editorShellCopy.vi.zoomReset(84));
    wrapper.unmount();
  });

  it(
    'starts at Fit: zoom in and out both enabled, only Fit pressed',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);

      // Fit's live 84% sits well inside 50-200%, so neither direction is
      // disabled: only landing exactly on 50% or 200% disables a button.
      expect(wrapper.get('[data-testid="zoom-out"]').attributes('disabled'))
        .toBeUndefined();
      expect(wrapper.get('[data-testid="zoom-in"]').attributes('disabled'))
        .toBeUndefined();
      expect(wrapper.get('[data-testid="zoom-fit"]').attributes('aria-pressed'))
        .toBe('true');
      expect(
        wrapper.get('[data-testid="zoom-percent"]').attributes('aria-pressed'),
      ).toBeUndefined();
      wrapper.unmount();
    },
  );

  it('disables zoom-in at 200% and clears aria-pressed once off Fit',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      const zoomIn = wrapper.get('[data-testid="zoom-in"]');
      for (let step = 0; step < 10; step += 1) {
        await zoomIn.trigger('click');
      }

      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('200%');
      expect(wrapper.get('[data-testid="zoom-in"]').attributes('disabled'))
        .toBeDefined();
      expect(wrapper.get('[data-testid="zoom-out"]').attributes('disabled'))
        .toBeUndefined();
      expect(wrapper.get('[data-testid="zoom-fit"]').attributes('aria-pressed'))
        .toBe('false');
      wrapper.unmount();
    });

  it('disables zoom-out at 50% after zooming down from Fit', async () => {
    const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
    const zoomOut = wrapper.get('[data-testid="zoom-out"]');
    for (let step = 0; step < 10; step += 1) {
      await zoomOut.trigger('click');
    }

    expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('50%');
    expect(wrapper.get('[data-testid="zoom-out"]').attributes('disabled'))
      .toBeDefined();
    expect(wrapper.get('[data-testid="zoom-in"]').attributes('disabled'))
      .toBeUndefined();
    wrapper.unmount();
  });

  it('reflects a manually chosen zoom in data-sheet-zoom', async () => {
    const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
    await wrapper.get('[data-testid="zoom-in"]').trigger('click');

    // Zooming in from the wide 84% Fit lands on the next fixed step above
    // it, 90%, not the smallest fixed step.
    expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('90%');
    const sheet = wrapper.get('[data-testid="preview-sheet"]');
    expect(sheet.attributes('data-sheet-zoom')).toBe('0.9000');
    wrapper.unmount();
  });

  it('resets to Fit from the percent button and from the Fit toggle',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      await wrapper.get('[data-testid="zoom-in"]').trigger('click');
      await wrapper.get('[data-testid="zoom-percent"]').trigger('click');
      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');

      await wrapper.get('[data-testid="zoom-in"]').trigger('click');
      await wrapper.get('[data-testid="zoom-fit"]').trigger('click');
      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');
      wrapper.unmount();
    });

  it(
    'zooms with Ctrl/Cmd + "=" / "-" / "0" while focus is inside the preview',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      const root = wrapper.element;

      root.dispatchEvent(new KeyboardEvent('keydown', {
        bubbles: true, cancelable: true, ctrlKey: true, key: '=',
      }));
      await nextTick();
      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('90%');

      root.dispatchEvent(new KeyboardEvent('keydown', {
        bubbles: true, cancelable: true, ctrlKey: true, key: '0',
      }));
      await nextTick();
      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');
      wrapper.unmount();
    },
  );

  it('ignores the zoom shortcut when focus is outside the preview',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);

      document.body.dispatchEvent(new KeyboardEvent('keydown', {
        bubbles: true, cancelable: true, ctrlKey: true, key: '=',
      }));
      await nextTick();

      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');
      wrapper.unmount();
    });

  it('does not react to the plain "=" key without Ctrl/Cmd', async () => {
    const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);

    wrapper.element.dispatchEvent(new KeyboardEvent('keydown', {
      bubbles: true, cancelable: true, key: '=',
    }));
    await nextTick();

    expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');
    wrapper.unmount();
  });

  it(
    'zooms on Ctrl/Cmd + wheel once 50px of delta accumulates, and '
    + 'prevents the default',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      const scroll = wrapper.get('[data-testid="preview-scroll"]').element;
      const zoomInEvent = ctrlWheelEvent(-50, { x: 20, y: 20 });

      scroll.dispatchEvent(zoomInEvent);
      await nextTick();

      expect(zoomInEvent.defaultPrevented).toBe(true);
      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('90%');
      wrapper.unmount();
    },
  );

  it('does not step before 50px of accumulated Ctrl/Cmd wheel delta',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      const scroll = wrapper.get('[data-testid="preview-scroll"]').element;

      scroll.dispatchEvent(ctrlWheelEvent(-49, { x: 20, y: 20 }));
      await nextTick();

      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');
      wrapper.unmount();
    });

  it('banks Ctrl/Cmd wheel delta across events until it reaches 50px',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      const scroll = wrapper.get('[data-testid="preview-scroll"]').element;

      scroll.dispatchEvent(ctrlWheelEvent(-20, { x: 20, y: 20 }));
      await nextTick();
      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');

      scroll.dispatchEvent(ctrlWheelEvent(-30, { x: 20, y: 20 }));
      await nextTick();
      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('90%');
      wrapper.unmount();
    });

  it('resets the accumulated wheel delta when its direction reverses',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      const scroll = wrapper.get('[data-testid="preview-scroll"]').element;

      scroll.dispatchEvent(ctrlWheelEvent(-40, { x: 20, y: 20 }));
      await nextTick();
      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');

      // Without a reset, 40px zooming in plus 40px zooming out would total
      // 80px and step immediately; a reversal must bank only the 40px this
      // event carries.
      scroll.dispatchEvent(ctrlWheelEvent(40, { x: 20, y: 20 }));
      await nextTick();
      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');
      wrapper.unmount();
    });

  it('resets the accumulated wheel delta after a 300ms idle gap',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      vi.useFakeTimers();
      const scroll = wrapper.get('[data-testid="preview-scroll"]').element;

      scroll.dispatchEvent(ctrlWheelEvent(-40, { x: 20, y: 20 }));
      await nextTick();
      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');

      vi.advanceTimersByTime(301);
      // Without the idle reset, this 40px would combine with the 40px
      // already banked and step immediately.
      scroll.dispatchEvent(ctrlWheelEvent(-40, { x: 20, y: 20 }));
      await nextTick();
      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');
      wrapper.unmount();
    });

  it(
    'ignores the Ctrl/Cmd zoom keyboard shortcut and wheel in Web mode',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      await wrapper.get('[data-mode="web"]').trigger('click');

      expect(
        wrapper.find('[data-testid="preview-zoom-controls"]').exists(),
      ).toBe(false);

      wrapper.element.dispatchEvent(new KeyboardEvent('keydown', {
        bubbles: true, cancelable: true, ctrlKey: true, key: '=',
      }));
      const scroll = wrapper.get('[data-testid="preview-scroll"]').element;
      const wheelEvent = ctrlWheelEvent(-100, { x: 20, y: 20 });
      scroll.dispatchEvent(wheelEvent);
      await nextTick();

      expect(wheelEvent.defaultPrevented).toBe(false);

      // Switching back to PDF proves the attempts above were true no-ops,
      // not just an unmounted control: the zoom is still exactly at Fit.
      await wrapper.get('[data-mode="pdf"]').trigger('click');
      expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');
      wrapper.unmount();
    },
  );

  it('moves one step for one coarse mouse-wheel notch', async () => {
    const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
    const scroll = wrapper.get('[data-testid="preview-scroll"]').element;

    scroll.dispatchEvent(ctrlWheelEvent(-120, { x: 20, y: 20 }));
    await nextTick();

    expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('90%');
    wrapper.unmount();
  });

  it('keeps focus in the card when a step button disables', async () => {
    const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
    document.body.append(wrapper.element);
    const zoomOut = wrapper.get('[data-testid="zoom-out"]');

    for (let i = 0; i < 6; i += 1) {
      (zoomOut.element as HTMLElement).focus();
      await zoomOut.trigger('click');
      await nextTick();
    }
    await nextTick();

    expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('50%');
    expect(document.activeElement).toBe(
      wrapper.get('[data-testid="zoom-percent"]').element,
    );
    wrapper.unmount();
    wrapper.element.remove();
  });

  it('leaves a plain wheel alone so the pane scrolls instead', async () => {
    const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
    const scroll = wrapper.get('[data-testid="preview-scroll"]').element;
    const plainWheel = new WheelEvent('wheel', {
      bubbles: true, cancelable: true, deltaY: -100,
    });

    scroll.dispatchEvent(plainWheel);
    await nextTick();

    expect(plainWheel.defaultPrevented).toBe(false);
    expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');
    wrapper.unmount();
  });

  it('persists the chosen zoom across mounts in the same browser',
    async () => {
      const first = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      await first.get('[data-testid="zoom-in"]').trigger('click');
      expect(window.localStorage.getItem(PREVIEW_ZOOM_STORAGE_KEY))
        .toBe('90');
      first.unmount();

      const second = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      expect(second.get('[data-testid="zoom-percent"]').text()).toBe('90%');
      second.unmount();
    });

  it('falls back to Fit when the stored zoom is corrupted', async () => {
    window.localStorage.setItem(PREVIEW_ZOOM_STORAGE_KEY, 'not-a-zoom');
    const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);

    expect(wrapper.get('[data-testid="zoom-percent"]').text()).toBe('84%');
    expect(wrapper.get('[data-testid="zoom-fit"]').attributes('aria-pressed'))
      .toBe('true');
    wrapper.unmount();
  });

  it('pads the bottom of the PDF scroll pane to clear the zoom card',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      expect(wrapper.get('[data-testid="preview-scroll"]').classes())
        .toContain('pb-18');

      await wrapper.get('[data-mode="web"]').trigger('click');
      expect(wrapper.get('[data-testid="preview-scroll"]').classes())
        .not.toContain('pb-18');
      wrapper.unmount();
    });

  it('reports no zoom announcement until a user action changes it',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      const announcement = wrapper.get('[data-testid="zoom-announcement"]');

      expect(announcement.attributes('aria-live')).toBe('polite');
      expect(announcement.text()).toBe('');
      wrapper.unmount();
    });

  it('announces the new zoom level in the polite live region', async () => {
    const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
    await wrapper.get('[data-testid="zoom-in"]').trigger('click');

    expect(wrapper.get('[data-testid="zoom-announcement"]').text()).toBe(
      editorShellCopy.en.zoomAnnounce(90),
    );
    wrapper.unmount();
  });

  it('announces the zoom level in Vietnamese too', async () => {
    setSiteLocale('vi');
    const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
    await wrapper.get('[data-testid="zoom-in"]').trigger('click');

    expect(wrapper.get('[data-testid="zoom-announcement"]').text()).toBe(
      editorShellCopy.vi.zoomAnnounce(90),
    );
    wrapper.unmount();
  });

  it('does not announce a zoom action that does not change the value',
    async () => {
      const wrapper = await mountPreviewAtWidth(WIDE_LAYOUT_WIDTH);
      // Already at Fit: resetting to Fit again changes nothing.
      await wrapper.get('[data-testid="zoom-fit"]').trigger('click');

      expect(wrapper.get('[data-testid="zoom-announcement"]').text())
        .toBe('');
      wrapper.unmount();
    });
});

async function animationFrames(count: number): Promise<void> {
  for (let index = 0; index < count; index += 1) {
    await new Promise<void>((resolve) => {
      requestAnimationFrame(() => resolve());
    });
  }
}
