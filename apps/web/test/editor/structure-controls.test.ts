import { mount } from '@vue/test-utils';
import { computed, nextTick, ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import StructurePanel from
  '../../app/components/editor/structure/StructurePanel.vue';
import EntryOrderControls from
  '../../app/components/editor/structure/EntryOrderControls.vue';
import SectionControls from
  '../../app/components/editor/structure/SectionControls.vue';
import type {
  ResumeEditorActions,
} from '../../app/composables/useResumeEditor';
import type { ResumeRecord } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

afterEach(() => {
  document.body
    .querySelectorAll(
      '[data-slot="alert-dialog-content"], [data-slot="alert-dialog-overlay"]',
    )
    .forEach((element) => element.remove());
});

describe('StructurePanel', () => {
  it('captures a remove-then-insert move through edit', async () => {
    const edit = vi.fn();
    const wrapper = mount(StructurePanel, {
      props: {
        actions: actionsFor(edit),
      },
    });

    await wrapper
      .get('[data-section="work"] [data-action="move-sidebar"]')
      .trigger('click');

    expect(edit).toHaveBeenCalledWith({
      kind: 'structure',
      commands: [
        { op: 'moveSection', key: 'work', column: 'sidebar', index: 1 },
      ],
    });
  });
});

describe('structure intent boundaries', () => {
  it('uses structure commands for custom and built-in sections', async () => {
    const edit = vi.fn();
    const createEntityId = vi.fn(() => '11111111-1111-1111-1111-111111111111');
    const wrapper = mount(StructurePanel, {
      props: {
        actions: actionsFor(edit, undefined, undefined, createEntityId),
      },
    });

    await wrapper.get('[data-action="section-type"]').setValue('custom');
    await wrapper.get('form').trigger('submit');
    await wrapper.get('[data-action="section-type"]').setValue('education');
    await wrapper.get('form').trigger('submit');

    expect(edit.mock.calls.map(([intent]) => intent)).toEqual([
      {
        kind: 'structure',
        commands: [
          {
            op: 'createSection',
            key: '11111111-1111-1111-1111-111111111111',
            sectionType: 'custom',
            column: 'main',
            index: 2,
          },
        ],
      },
      {
        kind: 'structure',
        commands: [
          {
            op: 'createSection',
            key: 'education',
            sectionType: 'education',
            column: 'main',
            index: 2,
          },
        ],
      },
    ]);
    expect(createEntityId).toHaveBeenCalledTimes(1);
  });

  it('generates no extra custom key for rejected duplicate and invalid keys',
    async () => {
      const duplicateKey = vi.fn(() => 'work');
      const invalidKey = vi.fn(() => 'not-a-uuid');
      const duplicate = mount(StructurePanel, {
        props: {
          actions: actionsFor(vi.fn(), undefined, undefined, duplicateKey),
        },
      });
      const invalid = mount(StructurePanel, {
        props: {
          actions: actionsFor(vi.fn(), undefined, undefined, invalidKey),
        },
      });

      await duplicate.get('[data-action="section-type"]').setValue('custom');
      await duplicate.get('form').trigger('submit');
      await invalid.get('[data-action="section-type"]').setValue('custom');
      await invalid.get('form').trigger('submit');

      expect(duplicateKey).toHaveBeenCalledTimes(1);
      expect(invalidKey).toHaveBeenCalledTimes(1);
    });

  it('blocks a malformed generated custom key without editing', async () => {
    const edit = vi.fn();
    const createEntityId = vi.fn(() => '111111111-1111-1111-1111-11111111111');
    const wrapper = mount(StructurePanel, {
      props: {
        actions: actionsFor(edit, undefined, undefined, createEntityId),
      },
    });

    await wrapper.get('[data-action="section-type"]').setValue('custom');
    await wrapper.get('form').trigger('submit');

    expect(edit).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain('Cannot create a custom section');
  });

  it('creates a section from the card form and clears the drafts', async () => {
    const edit = vi.fn();
    const wrapper = mount(StructurePanel, {
      props: { actions: actionsFor(edit) },
    });
    await wrapper.get('[data-action="section-type"]').setValue('project');
    await wrapper
      .get('[data-field="displayName"] input')
      .setValue('Side projects');
    await wrapper.get('[data-testid="section-create-form"]').trigger('submit');
    expect(edit).toHaveBeenLastCalledWith(expect.objectContaining({
      kind: 'structure',
      commands: [expect.objectContaining({
        op: 'createSection',
        key: 'project',
        displayName: 'Side projects',
      })],
    }));
    const displayName = wrapper.get('[data-field="displayName"] input')
      .element as HTMLInputElement;
    expect(displayName.value).toBe('');
  });

  it('sends complete section and entry permutations through their endpoints',
    async () => {
      const edit = vi.fn();
      const wrapper = mount(StructurePanel, {
        props: {
          actions: actionsFor(edit),
        },
      });

      await wrapper
        .get('[data-section="work"] [data-action="reorder"]')
        .trigger('click');
      await wrapper
        .get('[data-section="work"] [data-action="entry-down"]')
        .trigger('click');

      expect(edit.mock.calls.map(([intent]) => intent)).toEqual([
        {
          kind: 'structure',
          commands: [
            {
              op: 'reorderColumn',
              column: 'main',
              keys: ['work', 'skill'],
            },
          ],
        },
        {
          kind: 'entryReorder',
          sectionKey: 'work',
          entryIds: ['entry-2', 'entry-1'],
        },
      ]);
    });

  it('uses only the section metadata intent for name and icon changes',
    async () => {
      const edit = vi.fn();
      const wrapper = mount(StructurePanel, {
        props: {
          actions: actionsFor(edit),
        },
      });

      await wrapper
        .get('[data-section="work"] [data-action="displayName"]')
        .setValue('Experience');
      await wrapper
        .get('[data-section="work"] [data-action="iconKey"]')
        .setValue('briefcase');

      expect(edit.mock.calls.map(([intent]) => intent)).toEqual([
        {
          kind: 'sectionMetadata',
          sectionKey: 'work',
          change: { field: 'displayName', value: 'Experience' },
        },
        {
          kind: 'sectionMetadata',
          sectionKey: 'work',
          change: { field: 'iconKey', value: 'briefcase' },
        },
      ]);
      expect(edit).not.toHaveBeenCalledWith(
        expect.objectContaining({ kind: 'customization' }),
      );
    });

  it('requires a fresh destructive confirmation before deleting a section',
    async () => {
      const edit = vi.fn();
      const wrapper = mount(StructurePanel, {
        props: {
          actions: actionsFor(edit),
        },
      });

      await wrapper
        .get('[data-section="work"] [data-action="delete"]')
        .trigger('click');
      expect(edit).not.toHaveBeenCalled();
      await triggerBodyAction('confirm-delete');

      expect(edit).toHaveBeenCalledWith({
        kind: 'structure',
        commands: [{ op: 'deleteSection', key: 'work' }],
      });
    });

  it('rejects a stale section type and incomplete entry permutation',
    async () => {
      const edit = vi.fn();
      const wrapper = mount(StructurePanel, {
        props: {
          actions: actionsFor(edit),
        },
      });
      const section = wrapper.findAllComponents(SectionControls)[0]!;
      const entryOrder = wrapper.findAllComponents(EntryOrderControls)[1]!;

      section.vm.$emit('move', {
        key: 'work',
        sectionType: 'profile',
        column: 'main',
        index: 0,
      });
      entryOrder.vm.$emit('reorder', {
        entryIds: ['entry-1'],
        sectionKey: 'work',
        sectionType: 'work',
      });
      await nextTick();

      expect(edit).not.toHaveBeenCalled();
    });

  it('disables structure actions when a key appears in both columns',
    async () => {
      const edit = vi.fn();
      const wrapper = mount(StructurePanel, {
        props: {
          actions: actionsFor(
            edit,
            ref(
              structureRecord({
                current: placementRecord(
                  ['skill', 'work'],
                  ['profile', 'work'],
                ),
              }),
            ),
          ),
        },
      });

      expect(
        wrapper
          .get('[data-section="work"] [data-action="displayName"]')
          .attributes('disabled'),
      ).toBeDefined();
      await wrapper
        .get('[data-section="work"] [data-action="move-up"]')
        .trigger('click');

      expect(edit).not.toHaveBeenCalled();
    });

  it('removes before clamping a cross-column move index', async () => {
    const edit = vi.fn();
    const wrapper = mount(StructurePanel, {
      props: { actions: actionsFor(edit) },
    });
    const work = wrapper.findAllComponents(SectionControls)[1]!;

    work.vm.$emit('move', {
      key: 'work',
      sectionType: 'work',
      column: 'sidebar',
      index: 99,
    });
    await nextTick();

    expect(edit).toHaveBeenCalledWith({
      kind: 'structure',
      commands: [
        { op: 'moveSection', key: 'work', column: 'sidebar', index: 1 },
      ],
    });
  });

  it('reconfirms deletion after a changed section and placement', async () => {
    const edit = vi.fn();
    const state = ref(structureRecord());
    const wrapper = mount(StructurePanel, {
      attachTo: document.body,
      props: { actions: actionsFor(edit, state) },
    });
    const deleteButton = wrapper.get(
      '[data-section="work"] [data-action="delete"]',
    );
    (deleteButton.element as HTMLButtonElement).focus();
    await deleteButton.trigger('click');
    await nextTick();
    expect(document.activeElement).toBe(
      document.body.querySelector('[data-action="cancel-delete"]'),
    );

    state.value = structureRecord({
      current: placementRecord(['skill', 'work'], ['profile'], {
        sectionType: 'work',
        entries: [{ id: 'entry-1', jobTitle: 'Changed' }],
      }),
    });
    await nextTick();
    await triggerBodyAction('confirm-delete');
    await nextTick();

    expect(edit).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain(
      'This section changed. Reopen deletion and confirm again.',
    );
    expect(document.activeElement).toBe(
      wrapper.get('[data-section="work"] [data-action="delete"]').element,
    );

    await wrapper
      .get('[data-section="work"] [data-action="delete"]')
      .trigger('click');
    await triggerBodyAction('confirm-delete');

    expect(edit).toHaveBeenCalledWith({
      kind: 'structure',
      commands: [{ op: 'deleteSection', key: 'work' }],
    });
    wrapper.unmount();
  });

  it('reconfirms deletion after a placement-only change', async () => {
    const edit = vi.fn();
    const state = ref(structureRecord());
    const wrapper = mount(StructurePanel, {
      props: { actions: actionsFor(edit, state) },
    });

    await wrapper
      .get('[data-section="work"] [data-action="delete"]')
      .trigger('click');
    state.value = structureRecord({
      current: placementRecord(['work', 'skill'], ['profile']),
    });
    await nextTick();
    await triggerBodyAction('confirm-delete');

    expect(edit).not.toHaveBeenCalled();
    await wrapper
      .get('[data-section="work"] [data-action="delete"]')
      .trigger('click');
    await triggerBodyAction('confirm-delete');

    expect(edit).toHaveBeenCalledWith({
      kind: 'structure',
      commands: [{ op: 'deleteSection', key: 'work' }],
    });
  });

  it('keeps delete dialog focus contained and returns it on Escape and confirm',
    async () => {
      const edit = vi.fn();
      const wrapper = mount(StructurePanel, {
        attachTo: document.body,
        props: { actions: actionsFor(edit) },
      });
      const deleteButton = wrapper.get(
        '[data-section="work"] [data-action="delete"]',
      );
      (deleteButton.element as HTMLButtonElement).focus();
      await deleteButton.trigger('click');
      await nextTick();

      const dialog = document.body.querySelector<HTMLElement>(
        '[role="alertdialog"]',
      )!;
      expect(dialog.getAttribute('aria-describedby')).toMatch(
        /^reka-dialog-description-/,
      );
      expect(dialog.textContent).toContain(
        'permanently deletes',
      );

      dialog.dispatchEvent(new KeyboardEvent('keydown', {
        key: 'Escape', bubbles: true,
      }));
      await nextTick();
      expect(document.activeElement).toBe(deleteButton.element);

      await deleteButton.trigger('click');
      await triggerBodyAction('confirm-delete');
      await nextTick();
      expect(document.activeElement).toBe(deleteButton.element);
      wrapper.unmount();
    });

  it('returns focus to the delete button after cancellation', async () => {
    const wrapper = mount(StructurePanel, {
      attachTo: document.body,
      props: { actions: actionsFor(vi.fn()) },
    });
    const deleteButton = wrapper.get(
      '[data-section="work"] [data-action="delete"]',
    );
    (deleteButton.element as HTMLButtonElement).focus();
    await deleteButton.trigger('click');
    await nextTick();
    await triggerBodyAction('cancel-delete');
    await nextTick();

    expect(document.activeElement).toBe(deleteButton.element);
    wrapper.unmount();
  });

  it('keeps drafts and confirmation open when an edit is blocked', async () => {
    const edit = vi.fn(() => ({
      kind: 'blocked' as const,
      reason: 'session-lost' as const,
    }));
    const wrapper = mount(StructurePanel, {
      attachTo: document.body,
      props: { actions: actionsFor(edit) },
    });
    const [name, icon] = wrapper.findAll('form input');
    await name!.setValue('Experience');
    await icon!.setValue('briefcase');
    await wrapper.get('form').trigger('submit');

    expect((name!.element as HTMLInputElement).value).toBe('Experience');
    expect((icon!.element as HTMLInputElement).value).toBe('briefcase');

    await wrapper
      .get('[data-section="work"] [data-action="delete"]')
      .trigger('click');
    await triggerBodyAction('confirm-delete');

    expect(document.body.querySelector('[role="alertdialog"]')).not.toBeNull();
    expect(document.activeElement).toBe(
      document.body.querySelector('[data-action="cancel-delete"]'),
    );
    wrapper.unmount();
  });

  it('reopens placement conflicts against the latest record and focuses it',
    async () => {
      const edit = vi.fn();
      const acceptLatest = vi.fn().mockResolvedValue(undefined);
      const state = ref(
        structureRecord({
          conflicts: [
            {
              id: 'placement-conflict',
              subject: 'atomic',
              kind: 'target-changed',
              command: {
                id: 'placement-conflict',
                kind: 'structure',
                commands: [
                  {
                    op: 'moveSection',
                    key: 'work',
                    column: 'sidebar',
                    index: 0,
                  },
                ],
              },
            },
          ],
        }),
      );
      const wrapper = mount(StructurePanel, {
        attachTo: document.body,
        props: {
          actions: actionsFor(edit, state, acceptLatest),
        },
      });

      await wrapper.get('[data-action="reopen-placement"]').trigger('click');
      await nextTick();

      expect(acceptLatest).toHaveBeenCalledWith('placement-conflict');
      expect(document.activeElement).toBe(
        wrapper.get('[data-section="work"] button').element,
      );
      wrapper.unmount();
    });

  it('guides recreation when accepted latest removes the conflict section',
    async () => {
      const edit = vi.fn();
      const state = ref(
        structureRecord({
          conflicts: [structureConflict('work')],
        }),
      );
      const acceptLatest = vi.fn(() => {
        state.value = withoutWorkRecord();
        return Promise.resolve();
      });
      const wrapper = mount(StructurePanel, {
        attachTo: document.body,
        props: { actions: actionsFor(edit, state, acceptLatest) },
      });

      await wrapper.get('[data-action="reopen-placement"]').trigger('click');
      await nextTick();

      expect(wrapper.text()).toContain(
        [
          'This section is no longer available.',
          'Create a new section or select another section.',
        ].join(' '),
      );
      expect(document.activeElement).toBe(
        wrapper.get('[data-action="section-type"]').element,
      );
      wrapper.unmount();
    });

  it.each([
    ['removes', () => withoutWorkRecord()],
    [
      'retypes',
      () =>
        structureRecord({
          current: placementRecord(['skill', 'work'], ['profile'], {
            sectionType: 'profile',
            entries: [{ id: 'entry-1' }],
          }),
        }),
    ],
  ])(
    'guides entry-order recovery when accepted latest %s its section',
    async (_change, latest) => {
      const edit = vi.fn();
      const state = ref(
        structureRecord({
          conflicts: [entryOrderConflict('work')],
        }),
      );
      const acceptLatest = vi.fn(() => {
        state.value = latest();
        return Promise.resolve();
      });
      const wrapper = mount(StructurePanel, {
        attachTo: document.body,
        props: { actions: actionsFor(edit, state, acceptLatest) },
      });

      await wrapper.get('[data-action="reopen-entry-order"]').trigger('click');
      await nextTick();
      await nextTick();

      expect(wrapper.text()).toContain(
        'Entry order cannot reopen because its section is no longer available.',
      );
      expect(document.activeElement).toBe(
        wrapper.get('[data-action="section-type"]').element,
      );
      wrapper.unmount();
    },
  );

  it('focuses the control named by a structure validation issue', async () => {
    const edit = vi.fn();
    const state = ref(structureRecord());
    const wrapper = mount(StructurePanel, {
      attachTo: document.body,
      props: { actions: actionsFor(edit, state) },
    });

    state.value = structureRecord({
      issues: {
        'structure-command': [
          {
            path: 'content.work.displayName',
            code: 'max_length',
          },
        ],
      },
    });
    await nextTick();
    await nextTick();

    expect(document.activeElement).toBe(
      wrapper.get('[data-section="work"] [data-action="displayName"]').element,
    );
    wrapper.unmount();
  });

  it('leaves entry field validation focus to the entry form or summary',
    async () => {
      const state = ref(structureRecord());
      const wrapper = mount(StructurePanel, {
        attachTo: document.body,
        props: { actions: actionsFor(vi.fn(), state) },
      });
      const create = wrapper.get('[data-action="create"]');
      (create.element as HTMLButtonElement).focus();

      state.value = structureRecord({
        issues: {
          'entry-command': [
            {
              path: 'content.work.entries.0.jobTitle',
              code: 'max_length',
            },
          ],
        },
      });
      await nextTick();
      await nextTick();

      expect(document.activeElement).toBe(create.element);
      wrapper.unmount();
    });

  it('uses focusable move buttons and disables boundary controls', async () => {
    const edit = vi.fn();
    const wrapper = mount(StructurePanel, {
      attachTo: document.body,
      props: { actions: actionsFor(edit) },
    });
    const workUp = wrapper.get('[data-section="work"] [data-action="move-up"]');
    const profileMain = wrapper.get(
      '[data-section="profile"] [data-action="move-main"]',
    );

    expect((workUp.element as HTMLButtonElement).type).toBe('button');
    expect(
      wrapper
        .get('[data-section="skill"] [data-action="move-up"]')
        .attributes('disabled'),
    ).toBeDefined();
    expect(
      wrapper
        .get('[data-section="work"] [data-action="move-down"]')
        .attributes('disabled'),
    ).toBeDefined();
    expect(
      wrapper
        .get('[data-section="profile"] [data-action="move-up"]')
        .attributes('disabled'),
    ).toBeDefined();
    expect(
      wrapper
        .get('[data-section="profile"] [data-action="move-down"]')
        .attributes('disabled'),
    ).toBeDefined();

    (workUp.element as HTMLButtonElement).focus();
    expect(document.activeElement).toBe(workUp.element);
    await workUp.trigger('click');
    (profileMain.element as HTMLButtonElement).focus();
    expect(document.activeElement).toBe(profileMain.element);
    await profileMain.trigger('click');

    expect(edit.mock.calls.map(([intent]) => intent)).toEqual([
      {
        kind: 'structure',
        commands: [
          { op: 'moveSection', key: 'work', column: 'main', index: 0 },
        ],
      },
      {
        kind: 'structure',
        commands: [
          { op: 'moveSection', key: 'profile', column: 'main', index: 0 },
        ],
      },
    ]);
    wrapper.unmount();
  });
});

function actionsFor(
  edit: ReturnType<typeof vi.fn>,
  state = ref(structureRecord()),
  acceptLatest = vi.fn().mockResolvedValue(undefined),
  createEntityId = vi.fn(() => '11111111-1111-1111-1111-111111111111'),
): ResumeEditorActions {
  return {
    record: computed(() => state.value),
    createEntityId,
    edit: (intent) => edit(intent) ?? { kind: 'enqueued' },
    acceptLatest,
  } as ResumeEditorActions;
}

async function triggerBodyAction(action: string): Promise<void> {
  const elements = document.body.querySelectorAll<HTMLElement>(
    `[data-action="${action}"]`,
  );
  const element = elements[elements.length - 1];
  expect(element).not.toBeNull();
  element?.click();
  await nextTick();
}

function placementRecord(
  main: readonly string[],
  sidebar: readonly string[],
  work = { sectionType: 'work' as const, entries: [{ id: 'entry-1' }] },
): ResumeRecord['current'] {
  const record = structureRecord();
  return {
    ...record.current,
    document: {
      ...record.current.document,
      content: {
        ...record.current.document.content,
        work,
      },
      customization: {
        ...record.current.document.customization,
        layout: {
          ...record.current.document.customization.layout,
          sections: { main: [...main], sidebar: [...sidebar] },
        },
      },
    },
  };
}

function withoutWorkRecord(): ResumeRecord {
  return structureRecord({
    current: placementRecord(['skill'], ['profile']),
    conflicts: [],
  });
}

function structureConflict(key: string): ResumeRecord['conflicts'][number] {
  return {
    id: 'placement-conflict',
    subject: 'atomic',
    kind: 'target-changed',
    command: {
      id: 'placement-conflict',
      kind: 'structure',
      commands: [{ op: 'moveSection', key, column: 'sidebar', index: 0 }],
    },
  } as ResumeRecord['conflicts'][number];
}

function entryOrderConflict(key: string): ResumeRecord['conflicts'][number] {
  return {
    id: 'entry-order-conflict',
    subject: 'atomic',
    kind: 'membership-changed',
    command: {
      id: 'entry-order-conflict',
      kind: 'entryReorder',
      sectionKey: key,
      entryIds: ['entry-2', 'entry-1'],
      base: {
        target: { present: true, value: ['entry-1', 'entry-2'] },
        context: {
          sectionType: { present: true, value: 'work' },
        },
      },
    },
  } as ResumeRecord['conflicts'][number];
}

function structureRecord(overrides: Partial<ResumeRecord> = {}): ResumeRecord {
  const fixture = acceptedFixture();
  return {
    accepted: fixture,
    current: {
      ...fixture,
      document: {
        ...fixture.document,
        content: {
          profile: { sectionType: 'profile' as const, entries: [] },
          skill: { sectionType: 'skill' as const, entries: [] },
          work: {
            sectionType: 'work' as const,
            entries: [{ id: 'entry-1' }, { id: 'entry-2' }],
          },
        },
        customization: {
          ...fixture.document.customization,
          layout: {
            ...fixture.document.customization.layout,
            sections: { main: ['skill', 'work'], sidebar: ['profile'] },
          },
        },
      },
    },
    pending: [],
    attempt: null,
    conflicts: [],
    issues: {},
    templateState: null,
    photoRead: { kind: 'none' as const },
    completeReadRequired: false,
    sessionLost: false,
    opaquePhotoOutcome: null,
    ...overrides,
  } as ResumeRecord;
}
