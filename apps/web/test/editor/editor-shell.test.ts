import { mount } from '@vue/test-utils';
import { computed } from 'vue';
import { describe, expect, it, vi } from 'vitest';

import EditorShell from '../../app/components/editor/EditorShell.vue';
import type {
  ResumeEditorActions,
} from '../../app/composables/useResumeEditor';
import type { ResumeRecord } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

describe('EditorShell', () => {
  it('renders the four-region editor with adjacent account and theme', () => {
    const record = editorRecord();
    const wrapper = mount(EditorShell, {
      props: { actions: actionsFor(record), record },
      global: { stubs: heavyStubs() },
    });

    expect(wrapper.get('[data-region="app-rail"]').exists()).toBe(true);
    expect(wrapper.get('[data-region="outline"]').exists()).toBe(true);
    expect(wrapper.get('[data-region="preview"]').exists()).toBe(true);
    expect(wrapper.get('[data-region="inspector"]').exists()).toBe(true);
    expect(wrapper.get('[data-resume-title]').text()).toBe('Fixture');

    const controls = wrapper.get('.editor-account-actions')
      .findAll(':scope > *');
    expect(controls).toHaveLength(2);
    expect(controls[0]?.classes()).toContain('account-control');
    expect(controls[1]?.classes()).toContain('theme-toggle');
    expect(wrapper.text()).not.toMatch(/\bPublish\b|Undo all|Redo/);
  });

  it('derives the outline order from layout and changes local focus only',
    async () => {
      const record = editorRecord();
      const actions = actionsFor(record);
      const wrapper = mount(EditorShell, {
        props: { actions, record },
        global: { stubs: heavyStubs() },
      });

      const outline = wrapper.get('[aria-label="Resume outline"]');
      expect(outline.findAll('[data-outline-key]').map((item) => item.text()))
        .toEqual(['Personal details', 'Experience', 'Skills']);

      await outline.get('[data-outline-key="skill"]').trigger('click');
      expect(wrapper.getComponent({ name: 'SectionPanel' }).props('sectionKey'))
        .toBe('skill');
      expect(actions.edit).not.toHaveBeenCalled();
    });

  it('keeps editor and preview mounted while narrow navigation changes',
    async () => {
      const record = editorRecord();
      const wrapper = mount(EditorShell, {
        props: { actions: actionsFor(record), record },
        global: { stubs: heavyStubs() },
      });

      const editor = wrapper.get('[data-responsive-region="editor"]');
      const preview = wrapper.get('[data-responsive-region="preview"]');
      await wrapper.get('[data-action="show-preview"]').trigger('click');

      expect(editor.exists()).toBe(true);
      expect(preview.exists()).toBe(true);
      expect(editor.attributes('data-narrow-active')).toBe('false');
      expect(preview.attributes('data-narrow-active')).toBe('true');
    });

  it('matches hostile issue paths without building a CSS selector',
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

      await wrapper.get('.editor-error-summary button').trigger('click');

      expect(wrapper.get('[data-issue]').text()).toBe('1');
      wrapper.unmount();
    });
});

function editorRecord(): ResumeRecord {
  const accepted = acceptedFixture();
  accepted.document.content = {
    work: {
      sectionType: 'work',
      displayName: 'Experience',
      entries: [],
    },
    skill: {
      sectionType: 'skill',
      displayName: 'Skills',
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

function actionsFor(record: ResumeRecord): ResumeEditorActions {
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
  };
}

function heavyStubs() {
  return {
    EditorPreview: { name: 'EditorPreview', template: '<div />' },
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
      template: '<div />',
    },
    TemplatePanel: { name: 'TemplatePanel', template: '<div />' },
    PhotoPanel: { name: 'PhotoPanel', template: '<div />' },
  };
}
