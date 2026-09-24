import { mount } from '@vue/test-utils';
import { defineComponent, nextTick } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import EditorPreview from '../../app/components/editor/EditorPreview.vue';
import { editorShellCopy } from '../../app/i18n/editor-shell';
import { acceptedFixture } from './fixture';
import { setSiteLocale } from '../support/locale';

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
    ).toEqual({ lng: 'en', mode: 'paged' });
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
      ).toEqual({ lng: 'en', mode: 'continuous' });
      expect(wrapper.find('[data-testid="page-count"]').exists()).toBe(false);
      const sheet = wrapper.get('[data-testid="preview-sheet"]');
      expect(sheet.attributes('data-sheet-zoom')).toBeUndefined();
      expect(sheet.attributes('data-scaled-width')).toBeUndefined();
      expect(sheet.attributes('style') ?? '').not.toContain('zoom');
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
    ).toEqual({ lng: 'en', mode: 'continuous' });
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

  it('overlays the accepted canonical stamp outside renderer output', () => {
    const accepted = acceptedFixture();
    const wrapper = mount(EditorPreview, {
      props: {
        document: accepted.document,
        lng: accepted.metadata.lng,
        publicLink: '/ada-lovelace',
        stampState: 'landing',
      },
      global: { stubs: { ResumeDocument: true } },
    });

    const sheet = wrapper.get('[data-testid="preview-sheet"]');
    const stamp = sheet.get('[data-testid="preview-stamp"]');
    expect(stamp.attributes('aria-label')).toBe(
      'Public at aboutme.vn/ada-lovelace',
    );
    expect(stamp.attributes('data-stamp')).toBe('landing');
    expect(
      wrapper.getComponent({ name: 'ResumeDocument' })
        .find('[data-testid="preview-stamp"]').exists(),
    ).toBe(false);
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
    expect(renderer.props('context')).toEqual({ lng: 'en', mode: 'paged' });
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
    expect(renderer.props('context')).toEqual({ lng: 'en', mode: 'paged' });
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
      photoUrl: 'data:image/jpeg;base64,AA==',
    });
    expect(wrapper.html()).not.toContain('private-object.jpg');
  });
});

async function animationFrames(count: number): Promise<void> {
  for (let index = 0; index < count; index += 1) {
    await new Promise<void>((resolve) => {
      requestAnimationFrame(() => resolve());
    });
  }
}
