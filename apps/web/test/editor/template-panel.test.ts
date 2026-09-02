import { mount } from '@vue/test-utils';
import { computed, nextTick, ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { TEMPLATES } from '@aboutme/schema/templates';

import TemplatePanel from
  '../../app/components/editor/templates/TemplatePanel.vue';
import TemplatePartialDialog from
  '../../app/components/editor/templates/TemplatePartialDialog.vue';
import type { ResumeEditorActions } from
  '../../app/composables/useResumeEditor';
import {
  captureTemplateGroup,
  type EditorRuntime,
} from '../../app/editor/templateGroup';
import { replayCommand } from '../../app/editor/commands';
import { parseRevision } from '../../app/editor/revision';
import type { AcceptedResume } from '../../app/editor/types';
import type { ResumeRecord } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

afterEach(() => {
  document.body
    .querySelectorAll(
      '[data-slot="alert-dialog-content"], [data-slot="alert-dialog-overlay"]',
    )
    .forEach((element) => element.remove());
});

const runtime: EditorRuntime = {
  nowEpochMs: () => 0,
  uuid: () => 'generated-id',
  delay: async () => {},
};

describe('TemplatePanel', () => {
  it('delegates one preset and renders the returned group state', async () => {
    const group = templateGroup();
    const applyTemplate = vi.fn(() => ({ kind: 'enqueued' as const, group }));
    const wrapper = mount(TemplatePanel, {
      props: {
        actions: actionsFor(applyTemplate),
        group,
        state: { kind: 'queued', nextChild: 0 },
      },
    });

    expect(wrapper.findAll('[data-template]')).toHaveLength(TEMPLATES.length);
    await wrapper
      .get(`[data-template="${TEMPLATES[0]!.id}"] button`)
      .trigger('click');

    expect(applyTemplate).toHaveBeenCalledOnce();
    expect(applyTemplate).toHaveBeenCalledWith(TEMPLATES[0]);
    expect(wrapper.get('[role="status"]').text()).toBe('Saving template');
    expect(wrapper.text()).not.toContain(TEMPLATES[0]!.id);
  });

  it('reports no change and format warnings', async () => {
    const applyTemplate = vi.fn(() => ({ kind: 'no-change' as const }));
    const record = recordFor();
    const wrapper = mount(TemplatePanel, {
      props: { actions: actionsFor(applyTemplate), record },
    });

    const preset = TEMPLATES.find(
      (candidate) =>
        candidate.customization.dateFormat
        !== record.current.document.customization.dateFormat,
    )!;
    expect(wrapper.get(`[data-template="${preset.id}"]`).text()).toContain(
      'Page or date format will change.',
    );

    await wrapper.get(`[data-template="${preset.id}"] button`).trigger('click');

    expect(wrapper.get('[role="status"]').text()).toBe('No changes');
    expect(wrapper.text()).not.toContain('Selected template');
    expect(wrapper.text()).not.toContain('Saved template');
  });

  it('exposes undo only for the untouched latest complete group', async () => {
    const group = templateGroup();
    const final = acceptedFinal(group);
    const undo = {
      groupId: group.id,
      finalRevision: final.revision,
      preApplyTarget: group.base,
      finalTarget: group.intended,
      contentContext: group.contentContext,
    };
    const state = {
      kind: 'complete' as const,
      finalRevision: final.revision,
      undo,
    };
    const record = recordFor(final);
    const undoTemplate = vi.fn(() => ({
      kind: 'unavailable' as const,
      reason: 'state-changed' as const,
    }));
    const wrapper = mount(TemplatePanel, {
      props: {
        actions: actionsFor(vi.fn(), undefined, undefined, undoTemplate),
        record,
        state,
      },
    });

    expect(wrapper.get('[data-action="undo-template"]').exists()).toBe(true);
    await wrapper.get('[data-action="undo-template"]').trigger('click');
    expect(undoTemplate).toHaveBeenCalledOnce();

    const changed = recordFor(final);
    changed.current.document.customization.spacing.entryGap += 1;
    await wrapper.setProps({ record: changed });
    expect(wrapper.find('[data-action="undo-template"]').exists()).toBe(false);
  });

  it('keeps undo for entries and hides it after group changes', async () => {
    const group = templateGroup();
    const final = acceptedFinal(group);
    const state = {
      kind: 'complete' as const,
      finalRevision: final.revision,
      undo: {
        groupId: group.id,
        finalRevision: final.revision,
        preApplyTarget: group.base,
        finalTarget: group.intended,
        contentContext: group.contentContext,
      },
    };
    const stateRef = ref(recordFor(final));
    const wrapper = mount(TemplatePanel, {
      props: { actions: actionsFor(vi.fn(), stateRef), state },
    });

    const entryChanged = recordFor(final);
    entryChanged.current.document.content.skill!.entries = [
      {
        id: 'entry-1',
        name: 'Changed entry field',
      },
    ];
    stateRef.value = entryChanged;
    await nextTick();
    expect(wrapper.find('[data-action="undo-template"]').exists()).toBe(true);

    const placementChanged = recordFor(final);
    placementChanged.current.document.customization.layout.sections = {
      main: ['skill'],
      sidebar: [],
    };
    stateRef.value = placementChanged;
    await nextTick();
    expect(wrapper.find('[data-action="undo-template"]').exists()).toBe(false);
  });
});

describe('TemplatePartialDialog', () => {
  it.each([
    'retry-remaining',
    'restore-pre-apply',
    'keep-partial',
  ] as const)(
    'maps %s to the guarded recovery result',
    async (action) => {
      const group = templateGroup();
      const latest = partialLatest(group);
      const recoverTemplate = vi.fn(() =>
        action === 'keep-partial'
          ? { kind: 'keep-partial' as const }
          : { kind: 'enqueue' as const, group },
      );
      const wrapper = mount(TemplatePartialDialog, {
        attachTo: document.body,
        props: {
          actions: actionsFor(vi.fn(), undefined, recoverTemplate),
          group,
          state: partialState(latest),
        },
      });

      await nextTick();
      const actionButtons = document.body.querySelectorAll<HTMLElement>(
        `[data-action="${action}"]`,
      );
      actionButtons[actionButtons.length - 1]?.click();

      expect(recoverTemplate).toHaveBeenCalledWith(action);
      wrapper.unmount();
    },
  );

  it('renders safe structured partial changes', async () => {
    const group = templateGroup();
    const latest = partialLatest(group);
    const wrapper = mount(TemplatePartialDialog, {
      attachTo: document.body,
      props: {
        actions: actionsFor(vi.fn()),
        group,
        state: { ...partialState(latest), reason: 'unknown-outcome' },
      },
    });

    await nextTick();

    expect(document.body.textContent).toContain('Placement change accepted.');
    expect(document.body.textContent).toContain(
      'Customization change remains.',
    );
    expect(document.body.textContent).toContain(
      'The template result needs review.',
    );
    expect(document.body.textContent).not.toContain('unknown-outcome');
    expect(document.body.textContent).not.toContain(group.id);
    wrapper.unmount();
  });

  it('keeps the dialog open for unavailable recovery', async () => {
    const group = templateGroup();
    const wrapper = mount(TemplatePartialDialog, {
      attachTo: document.body,
      props: {
        actions: actionsFor(
          vi.fn(),
          undefined,
          vi.fn(() => ({
            kind: 'unavailable' as const,
            reason: 'context-changed' as const,
          })),
        ),
        group,
        state: partialState(partialLatest(group)),
      },
    });

    await nextTick();
    const retries = document.body.querySelectorAll<HTMLElement>(
      '[data-action="retry-remaining"]',
    );
    retries[retries.length - 1]?.click();
    await nextTick();

    expect(
      document.body.querySelector('[role="alertdialog"]'),
    ).not.toBeNull();
    const alerts = document.body.querySelectorAll('[role="alert"]');
    expect(alerts[alerts.length - 1]!.textContent).toBe(
      [
        'The resume context changed.',
        'Review the current resume before trying again.',
      ].join(' '),
    );
    wrapper.unmount();
  });

  it(
    'keeps the controlled dialog mounted on Escape without recovery',
    async () => {
      const group = templateGroup();
      const latest = partialLatest(group);
      const recoverTemplate = vi.fn();
      const wrapper = mount(TemplatePartialDialog, {
        attachTo: document.body,
        props: {
          actions: actionsFor(vi.fn(), undefined, recoverTemplate),
          group,
          state: partialState(latest),
        },
      });

      await nextTick();
      const retryButtons = document.body.querySelectorAll(
        '[data-action="retry-remaining"]',
      );
      const retry = retryButtons[retryButtons.length - 1]!;
      expect(document.activeElement).toBe(retry);
      document.body
        .querySelector('[role="alertdialog"]')!
        .dispatchEvent(new KeyboardEvent('keydown', {
          key: 'Escape', bubbles: true,
        }));
      await nextTick();

      expect(
        document.body.querySelector('[role="alertdialog"]'),
      ).not.toBeNull();
      expect(document.activeElement).toBe(retry);
      expect(recoverTemplate).not.toHaveBeenCalled();
      wrapper.unmount();
    },
  );
});

function templateGroup() {
  const current = acceptedFixture();
  current.document.content = {
    skill: { sectionType: 'skill', entries: [] },
  };
  current.document.customization = {
    ...current.document.customization,
    spacing: { ...current.document.customization.spacing, entryGap: 1 },
    layout: {
      ...current.document.customization.layout,
      sections: {
        main: ['skill'],
        sidebar: [],
      },
    },
  };
  const ids = ['group-1', 'structure-1', 'customization-1'];
  const preset = TEMPLATES.find(
    ({ customization }) => customization.layout.placement === 'byType',
  )!;
  return captureTemplateGroup({
    resumeId: current.metadata.id,
    ownerId: 'owner-1',
    sequence: 1,
    current,
    preset,
    dependencyIds: [],
    runtime: { ...runtime, uuid: () => ids.shift()! },
  })!;
}

function acceptedFinal(
  group: ReturnType<typeof templateGroup>,
): AcceptedResume {
  return {
    ...group.intendedFinal,
    revision: parseRevision('2'),
    metadataFreshness: 'complete',
  };
}

function partialLatest(
  group: ReturnType<typeof templateGroup>,
): AcceptedResume {
  return {
    ...replayCommand(group.preApply, group.children[0]!),
    revision: parseRevision('2'),
    metadataFreshness: 'complete',
  };
}

function partialState(latest: AcceptedResume) {
  return {
    kind: 'partial' as const,
    accepted: latest,
    nextChild: 1 as const,
    reason: 'child-failed' as const,
  };
}

function recordFor(current = acceptedFixture()): ResumeRecord {
  const accepted = structuredClone(current);
  return {
    accepted,
    current: structuredClone(current),
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

function actionsFor(
  applyTemplate: ReturnType<typeof vi.fn>,
  record = ref<ResumeRecord | undefined>(),
  recoverTemplate = vi.fn(() => ({
    kind: 'unavailable' as const,
    reason: 'state-changed' as const,
  })),
  undoTemplate = vi.fn(() => ({
    kind: 'unavailable' as const,
    reason: 'state-changed' as const,
  })),
): ResumeEditorActions {
  return {
    record: computed(() => record.value),
    applyTemplate,
    undoTemplate,
    recoverTemplate,
  } as ResumeEditorActions;
}
