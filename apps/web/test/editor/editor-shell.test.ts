import { mount } from '@vue/test-utils';
import { computed, type ComputedRef } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import EditorShell from '../../app/components/editor/EditorShell.vue';
import type {
  ResumeEditorActions,
} from '../../app/composables/useResumeEditor';
import type {
  PublishControllerState,
} from '../../app/editor/publishController';
import type { ResumeRecord } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('EditorShell', () => {
  it('renders the four-region editor with its stamped top bar', () => {
    const wrapper = mountShell();

    expect(wrapper.get('[data-region="app-rail"]').exists()).toBe(true);
    expect(wrapper.get('[data-region="outline"]').exists()).toBe(true);
    expect(wrapper.get('[data-region="preview"]').exists()).toBe(true);
    expect(wrapper.get('[data-region="inspector"]').exists()).toBe(true);
    expect(wrapper.get('[data-resume-title]').text()).toBe('Fixture');

    expect(wrapper.get('[aria-label="Account menu"]').exists()).toBe(true);
    expect(wrapper.find('[aria-label^="Switch to"]').exists()).toBe(false);
    const publish = wrapper.get('[data-action="publish"]');
    expect(publish.text()).toBe('Publish');
    expect(publish.attributes('data-variant')).toBe('seal');
    expect(wrapper.findAll('[data-action="publish"]')).toHaveLength(1);
    expect(wrapper.text()).not.toMatch(/Undo all|Redo/);
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false);
  });

  it(
    'shows the canonical public mark only for a live resume with a slug',
    () => {
      const draft = mountShell();
      expect(draft.find('[data-testid="public-mark"]').exists()).toBe(false);

      const record = editorRecord();
      record.current.metadata.live = true;
      record.current.metadata.slug = 'ada-lovelace';
      record.accepted.metadata.live = true;
      record.accepted.metadata.slug = 'ada-lovelace';
      const live = mount(EditorShell, {
        props: { actions: actionsFor(record), record },
        global: { stubs: heavyStubs() },
      });
      expect(live.get('[data-testid="public-mark"]').text()).toContain(
        'aboutme.vn/ada-lovelace',
      );
    },
  );

  it('lands and lifts both accepted canonical marks together', async () => {
    vi.useFakeTimers();
    vi.stubGlobal('matchMedia', vi.fn(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })));
    const record = editorRecord();
    const wrapper = mount(EditorShell, {
      props: { actions: actionsFor(record), record },
      global: { stubs: heavyStubs({ preview: false }) },
    });

    const published = editorRecord();
    published.accepted.metadata.live = true;
    published.accepted.metadata.slug = 'canonical-slug';
    published.current.metadata.live = true;
    published.current.metadata.slug = 'canonical-slug';
    await wrapper.setProps({ record: published });

    const titleMark = wrapper.get('[data-testid="public-mark"]');
    const previewMark = wrapper.get('[data-testid="preview-stamp"]');
    expect(titleMark.get('[data-public-link]').attributes('href'))
      .toBe('/canonical-slug');
    expect(titleMark.attributes('data-stamp')).toBe('landing');
    expect(previewMark.attributes('data-stamp')).toBe('landing');

    vi.advanceTimersByTime(180);
    await wrapper.vm.$nextTick();
    expect(titleMark.attributes('data-stamp')).toBeUndefined();
    expect(previewMark.attributes('data-stamp')).toBeUndefined();

    const unpublished = editorRecord();
    unpublished.accepted.metadata.slug = 'canonical-slug';
    unpublished.current.metadata.slug = 'canonical-slug';
    await wrapper.setProps({ record: unpublished });
    expect(wrapper.get('[data-testid="public-mark"]').attributes('data-stamp'))
      .toBe('lifting');
    expect(wrapper.get('[data-testid="preview-stamp"]').attributes(
      'data-stamp',
    )).toBe('lifting');

    vi.advanceTimersByTime(120);
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[data-testid="public-mark"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="preview-stamp"]').exists()).toBe(
      false,
    );
    wrapper.unmount();
  });

  it('opens the publish dialog from the editor topbar', async () => {
    const wrapper = mountShell({}, { attachTo: document.body });

    await wrapper.get('[data-action="publish"]').trigger('click');
    await wrapper.vm.$nextTick();

    expect(document.body.querySelector('[role="dialog"]')?.textContent)
      .toContain('Publish resume');
    wrapper.unmount();
  });

  it('places each editor region in an explicit grid cell', () => {
    const wrapper = mountShell();

    expect(wrapper.get('[data-region="app-rail"]').classes()).toEqual(
      expect.arrayContaining(['col-start-1', 'row-start-2']),
    );
    expect(wrapper.get('[data-region="outline"]').classes()).toEqual(
      expect.arrayContaining(['col-start-2', 'row-start-2']),
    );
    expect(wrapper.get('[data-region="preview"]').classes()).toEqual(
      expect.arrayContaining(['col-start-3', 'row-start-2']),
    );
    expect(wrapper.get('[data-region="inspector"]').classes()).toEqual(
      expect.arrayContaining(['col-start-4', 'row-start-2']),
    );
  });

  it('keeps the narrow topbar compact', () => {
    const wrapper = mountShell();
    const topbar = wrapper.get('[data-region="topbar"]');

    expect(topbar.classes()).toEqual(
      expect.arrayContaining(['flex', 'h-16']),
    );
    expect(wrapper.get('[data-resume-title]').classes())
      .toContain('max-[42rem]:hidden');
    expect(wrapper.get('[data-region="account-actions"]').classes())
      .not.toContain('max-[72rem]:col-span-full');
  });

  it('uses one headerless preview row', () => {
    const wrapper = mountShell();
    expect(wrapper.get('[data-region="preview"]').classes()).toEqual(
      expect.arrayContaining(['grid', 'grid-rows-[minmax(0,1fr)]']),
    );
    expect(wrapper.find('[data-preview-header]').exists()).toBe(false);
  });

  it(
    'derives the outline order from layout and changes local focus only',
    async () => {
      const record = editorRecord();
      const actions = actionsFor(record);
      const wrapper = mount(EditorShell, {
        props: { actions, record },
        global: { stubs: heavyStubs() },
      });

      const outline = wrapper.get('[aria-label="Resume outline"]');
      expect(
        outline.findAll('[data-outline-key]').map((item) => item.text()),
      ).toEqual(['Personal details', 'Experience', 'Skills']);
      expect(
        outline.get('[data-outline-key="personal"] [data-outline-icon]')
          .attributes('data-icon-key'),
      ).toBe('user');
      expect(
        outline.get('[data-outline-key="work"] [data-outline-icon]')
          .attributes('data-icon-key'),
      ).toBe('briefcase');

      await outline.get('[data-outline-key="skill"]').trigger('click');
      expect(
        wrapper.getComponent({ name: 'SectionPanel' }).props('sectionKey'),
      ).toBe('skill');
      expect(actions.edit).not.toHaveBeenCalled();
    },
  );

  it(
    'keeps editor and preview mounted while narrow navigation changes',
    async () => {
      const record = editorRecord();
      const wrapper = mount(EditorShell, {
        props: { actions: actionsFor(record), record },
        global: { stubs: heavyStubs() },
      });
      const editor = wrapper.get('[data-responsive-region="editor"]');
      const preview = wrapper.get('[data-responsive-region="preview"]');
      const rail = wrapper.get('[data-region="app-rail"]');
      await wrapper.get('[data-action="show-preview"]').trigger('click');

      expect(editor.exists()).toBe(true);
      expect(preview.exists()).toBe(true);
      expect(editor.attributes('data-narrow-active')).toBe('false');
      expect(preview.attributes('data-narrow-active')).toBe('true');
      expect(rail.classes()).toContain(
        'max-[72rem]:data-[narrow-active=false]:opacity-0',
      );
    },
  );

  it('renders the rail as pressed icon buttons with tooltips', async () => {
    const wrapper = mountShell();
    const design = wrapper.get(
      '[data-region="app-rail"] [aria-label="Design"]',
    );
    expect(design.attributes('aria-pressed')).toBe('false');
    await design.trigger('click');
    expect(design.attributes('aria-pressed')).toBe('true');
    expect(design.classes()).toEqual(
      expect.arrayContaining([
        'aria-pressed:bg-secondary',
        'aria-pressed:text-primary',
      ]),
    );
    expect(wrapper.get('[data-testid="customization-title"]').text())
      .toBe('Customization');
    expect(wrapper.findAll('[data-testid="customization-title"]'))
      .toHaveLength(1);
  });

  it(
    'keeps the phone switch outside the topbar and fits the preview',
    async () => {
      const originalWidth = window.innerWidth;
      Object.defineProperty(window, 'innerWidth', {
        configurable: true,
        value: 390,
      });
      const record = editorRecord();
      const wrapper = mount(EditorShell, {
        props: { actions: actionsFor(record), record },
        global: { stubs: heavyStubs({ preview: false }) },
      });
      await wrapper.vm.$nextTick();

      const switcher = wrapper.get('[role="tablist"]');
      expect(switcher.get('[data-action="show-editor"]').text()).toBe('Edit');
      await switcher.get('[data-action="show-preview"]').trigger('click');
      expect(
        wrapper.get('[data-responsive-region="preview"]')
          .attributes('data-narrow-active'),
      ).toBe('true');
      expect(wrapper.get('[data-region="inspector"]').classes()).toEqual(
        expect.arrayContaining([
          'max-[42rem]:w-full',
          'max-[42rem]:max-w-none',
        ]),
      );
      const sheet = wrapper.get('[data-testid="preview-sheet"]');
      expect(Number(sheet.attributes('data-sheet-zoom'))).toBeLessThan(1);
      expect(Number(sheet.attributes('data-scaled-width'))).toBeLessThan(390);

      await wrapper.get('[data-action="show-editor"]').trigger('click');
      await wrapper.get('[data-action="open-sections"]').trigger('click');
      await wrapper.vm.$nextTick();
      expect(document.body.querySelector('[aria-label="Resume sections"]'))
        .not.toBeNull();

      wrapper.unmount();
      Object.defineProperty(window, 'innerWidth', {
        configurable: true,
        value: originalWidth,
      });
    },
  );

  it('renders a hostile resume title as text', () => {
    const record = editorRecord();
    record.current.metadata.title = '<img src=x onerror=alert(1)>';
    const wrapper = mount(EditorShell, {
      props: { actions: actionsFor(record), record },
      global: { stubs: heavyStubs() },
    });
    const title = wrapper.get('[data-resume-title]');

    expect(title.text()).toBe('<img src=x onerror=alert(1)>');
    expect(descendantNames(title.element)).not.toContain('img');
  });

  it('keeps the session-lost dialog open on Escape', async () => {
    const wrapper = mountShell(
      { sessionLost: true },
      { attachTo: document.body },
    );
    await wrapper.vm.$nextTick();
    const dialog = document.body.querySelector('[role="alertdialog"]')!;
    expect(dialog.textContent).toContain('Sign in to continue editing');
    dialog.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    );
    await wrapper.vm.$nextTick();
    expect(document.body.querySelector('[role="alertdialog"]')).not.toBeNull();
    expect(
      document.body.querySelector('[data-action="resume-after-auth"]'),
    ).not.toBeNull();
    const overlay = document.body.querySelector(
      '[data-slot="alert-dialog-overlay"]',
    )!;
    overlay.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
    await wrapper.vm.$nextTick();
    expect(document.body.querySelector('[role="alertdialog"]')).not.toBeNull();
    expect(wrapper.get('[data-region="preview"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it.each([
    ['loading', 'Photo is loading'],
    ['unavailable', 'Photo unavailable'],
  ] as const)(
    'renders the %s photo state with the preview sheet',
    (state, text) => {
      const record = editorRecord();
      record.current.document.personalDetails.photo = {
        key: 'resumes/resume-1/private-object.jpg',
      };
      record.photoRead
        = state === 'loading'
          ? { kind: 'loading', binding: 'k', generation: 1 }
          : {
              kind: 'suspended',
              binding: 'k',
              generation: 1,
              reason: 'read-failed',
            };
      const wrapper = mount(EditorShell, {
        props: { actions: actionsFor(record), record },
        global: { stubs: heavyStubs({ preview: false }) },
      });

      expect(wrapper.findAll('[data-photo-state]')).toHaveLength(1);
      expect(wrapper.get('[data-photo-state]').text()).toContain(text);
      expect(wrapper.html()).not.toContain('private-object.jpg');
    },
  );

  it(
    'matches hostile issue paths without building a CSS selector',
    async () => {
      const record = editorRecord();
      const path = 'personalDetails"]';
      record.issues = {
        [path]: [{ path, code: 'format', message: 'raw server text' }],
      };
      const wrapper = mount(EditorShell, {
        attachTo: document.body,
        props: { actions: actionsFor(record), record },
        global: {
          stubs: {
            ...heavyStubs(),
            PersonalDetailsPanel: {
              data: () => ({ hits: 0, path }),
              template: [
                '<button :data-issue="path" @click="hits += 1">',
                '{{ hits }}',
                '</button>',
              ].join(''),
            },
          },
        },
      });

      await wrapper.get('[data-action="focus-editor-issue"]').trigger('click');

      expect(wrapper.get('[data-issue]').text()).toBe('1');
      wrapper.unmount();
    },
  );
});

function editorRecord(): ResumeRecord {
  const accepted = acceptedFixture();
  accepted.document.content = {
    work: {
      sectionType: 'work',
      displayName: 'Experience',
      iconKey: 'briefcase',
      entries: [],
    },
    skill: {
      sectionType: 'skill',
      displayName: 'Skills',
      iconKey: 'code',
      entries: [],
    },
  };
  accepted.document.customization.layout.sections = {
    main: ['work'],
    sidebar: ['skill'],
  };
  return {
    accepted,
    current: {
      document: structuredClone(accepted.document),
      metadata: structuredClone(accepted.metadata),
    },
    pending: [],
    attempt: null,
    conflicts: [],
    issues: {},
    templateState: null,
    photoRead: { kind: 'none' },
    completeReadRequired: false,
    sessionLost: false,
    opaquePhotoOutcome: null,
  };
}

function descendantNames(root: Element): string[] {
  const names: string[] = [];
  const pending = [...root.children];
  while (pending.length > 0) {
    const element = pending.pop()!;
    names.push(element.localName);
    pending.push(...element.children);
  }
  return names;
}

function mountShell(
  recordOverrides: Partial<ResumeRecord> = {},
  options: { attachTo?: Element | string } = {},
) {
  const record = { ...editorRecord(), ...recordOverrides };
  return mount(EditorShell, {
    ...options,
    props: { actions: actionsFor(record), record },
    global: { stubs: heavyStubs() },
  });
}

function actionsFor(
  record: ResumeRecord,
  publishState: ComputedRef<PublishControllerState> = computed(
    () => ({ kind: 'idle' }),
  ),
): ResumeEditorActions {
  return {
    record: computed(() => record),
    createEntityId: vi.fn(() => '00000000-0000-4000-8000-000000000001'),
    edit: vi.fn(() => ({ kind: 'blocked', reason: 'not-loaded' })),
    applyTemplate: vi.fn(() => ({ kind: 'no-change' })),
    undoTemplate: vi.fn(() => ({
      kind: 'unavailable',
      reason: 'state-changed',
    })),
    recoverTemplate: vi.fn(() => ({
      kind: 'unavailable',
      reason: 'state-changed',
    })),
    resolveOpaquePhoto: vi.fn(),
    retry: vi.fn(),
    acceptLatest: vi.fn(),
    applyMine: vi.fn(),
    resumeAfterAuth: vi.fn(),
    discard: vi.fn(),
    publish: {
      state: publishState,
      submit: vi.fn(),
      retryUncertain: vi.fn(),
      reauthPassword: vi.fn(),
      startProviderReauth: vi.fn(),
      retryAfterProviderReauth: vi.fn(),
      cancel: vi.fn(),
    },
  };
}

function heavyStubs(options: { preview?: boolean } = {}) {
  return {
    ...(options.preview === false
      ? { ResumeDocument: { name: 'ResumeDocument', template: '<div />' } }
      : { EditorPreview: { name: 'EditorPreview', template: '<div />' } }),
    PersonalDetailsPanel: {
      name: 'PersonalDetailsPanel',
      template: '<div />',
    },
    SectionPanel: {
      name: 'SectionPanel',
      props: ['sectionKey', 'section'],
      template: '<div />',
    },
    StructurePanel: { name: 'StructurePanel', template: '<div />' },
    CustomizationPanel: {
      name: 'CustomizationPanel',
      template: [
        '<div><h2 id="customization-title"',
        ' data-testid="customization-title">Customization</h2></div>',
      ].join(''),
    },
    TemplatePanel: { name: 'TemplatePanel', template: '<div />' },
    PhotoPanel: { name: 'PhotoPanel', template: '<div />' },
  };
}
