import { mount } from '@vue/test-utils';
import { computed, nextTick } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import EditorShell from '../../app/components/editor/EditorShell.vue';
import type {
  ResumeEditorActions,
} from '../../app/composables/useResumeEditor';
import {
  shouldRetainEditorOnSessionLoss,
} from '../../app/composables/useUnsavedNavigationGuard';
import type { ResumeRecord } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('editor persistence boundary', () => {
  it('retains unsafe in-memory work when authentication becomes anonymous',
    () => {
      const record = editorRecord();
      record.pending = [{ id: 'pending-1' }] as never;

      expect(shouldRetainEditorOnSessionLoss(record)).toBe(true);
      expect(shouldRetainEditorOnSessionLoss(editorRecord())).toBe(false);
    });

  it(
    'does not persist retained session-lost work or change the URL',
    async () => {
      const storage = persistenceProbes();
      const before = currentURL();
      const record = editorRecord();
      record.pending = [{ id: 'pending-1' }] as never;
      record.sessionLost = true;

      const wrapper = mount(EditorShell, {
        attachTo: document.body,
        props: { actions: actionsFor(record), record },
        global: { stubs: shellStubs() },
      });

      try {
        await nextTick();
        expect(
          document.body.querySelector('[data-action="resume-after-auth"]'),
        ).not.toBeNull();
        expect(storage.every((probe) => probe.mock.calls.length === 0)).toBe(
          true,
        );
        expect(currentURL()).toEqual(before);
      } finally {
        wrapper.unmount();
        if (wrapper.element.isConnected) wrapper.element.remove();
        document.body
          .querySelectorAll(
            '[role="alertdialog"], [data-slot="alert-dialog-overlay"]',
          )
          .forEach((element) => element.remove());
      }
    },
  );
});

function currentURL() {
  return {
    href: window.location.href,
    search: window.location.search,
    hash: window.location.hash,
  };
}

function persistenceProbes() {
  const local = ['getItem', 'setItem', 'removeItem', 'clear'].map((name) =>
    vi.spyOn(window.localStorage, name as 'clear'),
  );
  const session = ['getItem', 'setItem', 'removeItem', 'clear'].map((name) =>
    vi.spyOn(window.sessionStorage, name as 'clear'),
  );
  const history = [
    vi.spyOn(window.history, 'pushState'),
    vi.spyOn(window.history, 'replaceState'),
  ];
  const indexedDB = {
    open: vi.fn(),
    deleteDatabase: vi.fn(),
  };
  const sendBeacon = vi.fn();
  vi.stubGlobal('indexedDB', indexedDB);
  vi.stubGlobal('navigator', { ...window.navigator, sendBeacon });
  return [
    ...local,
    ...session,
    ...history,
    indexedDB.open,
    indexedDB.deleteDatabase,
    sendBeacon,
  ];
}

function editorRecord(): ResumeRecord {
  const accepted = acceptedFixture();
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
    publish: {
      state: computed(() => ({ kind: 'idle' })) as never,
      submit: vi.fn(),
      retryUncertain: vi.fn(),
      reauthPassword: vi.fn(),
      startProviderReauth: vi.fn(),
      retryAfterProviderReauth: vi.fn(),
      cancel: vi.fn(),
    },
  };
}

function shellStubs() {
  return {
    EditorPreview: { template: '<div />' },
    PersonalDetailsPanel: { template: '<div />' },
    SectionPanel: { template: '<div />' },
    StructurePanel: { template: '<div />' },
    CustomizationPanel: { template: '<div />' },
    TemplatePanel: { template: '<div />' },
    PhotoPanel: { template: '<div />' },
  };
}
