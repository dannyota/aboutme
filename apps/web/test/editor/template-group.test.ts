import { TEMPLATES } from '@aboutme/schema/templates';
import { reactive } from 'vue';
import { describe, expect, it } from 'vitest';

import {
  advanceTemplateGroup,
  captureTemplateUndo,
  captureTemplateGroup,
  nextTemplateChild,
  recoverTemplateGroup,
  templateChildApplied,
  templateRecoveryAvailable,
} from '../../app/editor/templateGroup';
import { replayCommand } from '../../app/editor/commands';
import { applyTemplate } from '../../app/components/resume/applyTemplate';
import { parseRevision } from '../../app/editor/revision';
import type {
  EditorRuntime,
  TemplateGroupState,
} from '../../app/editor/templateGroup';
import { acceptedFixture } from './fixture';

const preset = TEMPLATES.find(
  ({ customization }) => customization.layout.placement === 'byType',
)!;

function group() {
  const fixture = acceptedFixture();
  const current = {
    ...fixture,
    document: {
      ...fixture.document,
      content: { skill: { sectionType: 'skill' as const, entries: [] } },
      customization: {
        ...fixture.document.customization,
        spacing: { ...fixture.document.customization.spacing, entryGap: 1 },
        layout: {
          ...fixture.document.customization.layout,
          sections: { main: ['skill'], sidebar: [] },
        },
      },
    },
  };
  const ids = ['group-1', 'structure-1', 'customization-1'];
  return captureTemplateGroup({
    resumeId: current.metadata.id,
    ownerId: 'owner-1',
    sequence: 4,
    current,
    preset,
    dependencyIds: ['prior-1'],
    runtime: {
      nowEpochMs: () => 0,
      uuid: () => ids.shift()!,
      delay: async () => {},
    } satisfies EditorRuntime,
  });
}

describe('template groups', () => {
  it('captures an immutable group from a reactive editor snapshot', () => {
    const current = reactive(acceptedFixture());

    const captured = captureTemplateGroup({
      resumeId: current.metadata.id,
      ownerId: 'owner-1',
      sequence: 1,
      current,
      preset,
      dependencyIds: [],
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => 'reactive-id',
        delay: async () => {},
      },
    });

    expect(captured).not.toBeNull();
    expect(Object.isFrozen(captured)).toBe(true);
  });

  it('captures an immutable reverse only from the recorded final', () => {
    const captured = group()!;
    const final = {
      ...captured.intendedFinal,
      revision: parseRevision('2'),
      metadataFreshness: 'complete' as const,
    };
    const complete = advanceTemplateGroup(
      captured,
      { kind: 'running', nextChild: 1, lastRevision: parseRevision('1') },
      final,
    );
    if (complete.kind !== 'complete') throw new Error('expected complete');
    const reverse = captureTemplateUndo({
      undo: complete.undo,
      current: final,
      ownerId: 'owner-1',
      sequence: 5,
      dependencyIds: [],
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => 'reverse-id',
        delay: async () => {},
      },
    });

    expect(reverse).toMatchObject({
      kind: 'enqueue',
      group: {
        preApply: final,
        intendedFinal: { document: captured.preApply.document },
      },
    });
    if (reverse.kind !== 'enqueue') throw new Error('expected reverse');
    expect(Object.isFrozen(reverse.group)).toBe(true);
    const changed = structuredClone(final);
    changed.document.customization.spacing.entryGap = 99;
    expect(reverse.group.preApply.document.customization.spacing.entryGap)
      .not.toBe(99);
    expect(
      captureTemplateUndo({
        undo: complete.undo,
        current: { ...changed, revision: parseRevision('3') },
        ownerId: 'owner-1',
        sequence: 5,
        dependencyIds: [],
        runtime: {
          nowEpochMs: () => 0,
          uuid: () => 'ignored',
          delay: async () => {},
        },
      }),
    ).toEqual({ kind: 'unavailable', reason: 'state-changed' });
  });

  it('keeps undo after an unrelated accepted entry-field edit', () => {
    const captured = group()!;
    const final = {
      ...captured.intendedFinal,
      revision: parseRevision('2'),
      metadataFreshness: 'complete' as const,
    };
    const complete = advanceTemplateGroup(
      captured,
      { kind: 'running', nextChild: 1, lastRevision: parseRevision('1') },
      final,
    );
    if (complete.kind !== 'complete') throw new Error('expected complete');
    const edited = structuredClone(final);
    edited.revision = parseRevision('3');
    edited.document.content.skill!.entries = [{
      id: 'entry-1',
      name: 'Unrelated entry edit',
    }];

    const reverse = captureTemplateUndo({
      undo: complete.undo,
      current: edited,
      ownerId: 'owner-1',
      sequence: 5,
      dependencyIds: [],
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => 'reverse-id',
        delay: async () => {},
      },
    });

    expect(reverse).toMatchObject({ kind: 'enqueue' });
  });
  it('returns null when the helper result is already current', () => {
    const fixture = acceptedFixture();
    const current = {
      ...fixture,
      document: {
        ...fixture.document,
        customization: applyTemplate(
          fixture.document.customization,
          preset,
          fixture.document.content,
        ),
      },
    };

    expect(
      captureTemplateGroup({
        resumeId: current.metadata.id,
        ownerId: 'owner-1',
        sequence: 1,
        current,
        preset,
        dependencyIds: [],
        runtime: {
          nowEpochMs: () => 0,
          uuid: () => 'unused',
          delay: async () => {},
        },
      }),
    ).toBeNull();
  });

  it('captures only customization when placement already matches', () => {
    const current = acceptedFixture();
    const captured = captureTemplateGroup({
      resumeId: current.metadata.id,
      ownerId: 'owner-1',
      sequence: 1,
      current,
      preset,
      dependencyIds: [],
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => 'id',
        delay: async () => {},
      },
    });

    expect(captured?.children.map(({ kind }) => kind)).toEqual([
      'customization',
    ]);
  });

  it('captures deterministic IDs and adjacent dependency order', () => {
    const captured = group();

    expect(captured?.id).toBe('group-1');
    expect(captured?.children.map(({ id }) => id)).toEqual([
      'structure-1',
      'customization-1',
    ]);
    expect(captured?.children[1]?.dependencyIds).toEqual([
      'prior-1',
      'structure-1',
    ]);
  });

  it('replays children to the helper result without changing content', () => {
    const captured = group()!;
    const final = captured.children.reduce(replayCommand, captured.preApply);
    const helperResult = applyTemplate(
      captured.preApply.document.customization,
      preset,
      captured.preApply.document.content,
    );

    expect(final.document.customization).toEqual(helperResult);
    expect(final.document.content).toEqual(captured.preApply.document.content);
    expect(captured.intendedFinal.document.content).toEqual(
      captured.preApply.document.content,
    );
  });

  it('completes only from one accepted intended-final revision', () => {
    const captured = group()!;
    const state: TemplateGroupState = {
      kind: 'running',
      nextChild: 1,
      lastRevision: parseRevision('1'),
    };
    const final = {
      ...captured.intendedFinal,
      revision: parseRevision('2'),
      metadataFreshness: 'complete' as const,
    };

    expect(advanceTemplateGroup(captured, state, final)).toMatchObject({
      kind: 'complete',
      finalRevision: parseRevision('2'),
    });
  });

  it('admits customization after the accepted structure child', () => {
    const captured = group()!;
    const structure = captured.children[0]!;
    const intermediate = replayCommand(captured.preApply, structure);
    const accepted = {
      ...intermediate,
      revision: parseRevision('2'),
      metadataFreshness: 'complete' as const,
    };

    expect(
      advanceTemplateGroup(
        captured,
        { kind: 'queued', nextChild: 0 },
        accepted,
      ),
    ).toEqual({
      kind: 'running',
      nextChild: 1,
      lastRevision: parseRevision('2'),
    });
  });

  it(
    'recovers remaining work only from the expected intermediate target',
    () => {
      const captured = group()!;
      const state = {
        kind: 'partial' as const,
        accepted: {
          ...captured.preApply,
          revision: parseRevision('1'),
          metadataFreshness: 'complete' as const,
        },
        nextChild: 0 as const,
        reason: 'remote-change' as const,
      };
      const latest = state.accepted;

      expect(
        recoverTemplateGroup(captured, state, latest, 'keep-partial'),
      ).toEqual({
        kind: 'keep-partial',
      });
      expect(
        nextTemplateChild(captured, { kind: 'queued', nextChild: 0 }),
      ).toEqual(captured.children[0]);
    });

  it('builds a guarded reverse group for restore-pre-apply', () => {
    const captured = group()!;
    const intermediate = replayCommand(
      captured.preApply,
      captured.children[0]!,
    );
    const latest = {
      ...intermediate,
      revision: parseRevision('2'),
      metadataFreshness: 'complete' as const,
    };
    const state: TemplateGroupState = {
      kind: 'partial',
      accepted: latest,
      nextChild: 1,
      reason: 'child-failed',
    };
    const ids = [
      'reverse-group',
      'reverse-structure',
      'reverse-customization',
    ];

    expect(
      recoverTemplateGroup(
        captured,
        state,
        latest,
        'restore-pre-apply',
        {
          nowEpochMs: () => 0,
          uuid: () => ids.shift()!,
          delay: async () => {},
        },
      ),
    ).toMatchObject({
      kind: 'enqueue',
      group: {
        id: 'reverse-group',
        preApply: latest,
        intendedFinal: captured.preApply,
      },
    });
  });

  // docs/design/data.md#resume-aggregate: content is an unordered map, and
  // the server re-encodes its keys in byte order.
  it('completes when the accepted resume lists content keys in another order',
    () => {
      const fixture = acceptedFixture();
      const current = {
        ...fixture,
        document: {
          ...fixture.document,
          content: {
            work: { sectionType: 'work' as const, entries: [] },
            education: { sectionType: 'education' as const, entries: [] },
          },
          customization: {
            ...fixture.document.customization,
            layout: {
              ...fixture.document.customization.layout,
              sections: { main: ['work', 'education'], sidebar: [] },
            },
          },
        },
      };
      const keep = TEMPLATES.find(
        ({ customization }) => customization.layout.placement === 'keep',
      )!;
      const captured = captureTemplateGroup({
        resumeId: current.metadata.id,
        ownerId: 'owner-1',
        sequence: 1,
        current,
        preset: keep,
        dependencyIds: [],
        runtime: {
          nowEpochMs: () => 0,
          uuid: () => 'id',
          delay: async () => {},
        },
      })!;
      const saved = captured.children.reduce(replayCommand, captured.preApply);
      const { work, education } = saved.document.content;
      const accepted = {
        ...saved,
        document: { ...saved.document, content: { education, work } },
        revision: parseRevision('2'),
        metadataFreshness: 'complete' as const,
      };

      expect(captured.children.map(({ kind }) => kind)).toEqual([
        'customization',
      ]);
      expect(
        advanceTemplateGroup(
          captured,
          { kind: 'queued', nextChild: 0 },
          accepted,
        ),
      ).toMatchObject({ kind: 'complete' });
    });

  it('stays partial when another writer adds a section', () => {
    const captured = group()!;
    const final = captured.children.reduce(replayCommand, captured.preApply);
    const accepted = {
      ...final,
      document: {
        ...final.document,
        content: {
          ...final.document.content,
          added: { sectionType: 'language' as const, entries: [] },
        },
      },
      revision: parseRevision('3'),
      metadataFreshness: 'complete' as const,
    };

    expect(
      advanceTemplateGroup(
        captured,
        { kind: 'running', nextChild: 1, lastRevision: parseRevision('2') },
        accepted,
      ),
    ).toMatchObject({ kind: 'partial', reason: 'context-change' });
  });

  it('offers recovery only where it can succeed', () => {
    const captured = group()!;
    const intermediate = {
      ...replayCommand(captured.preApply, captured.children[0]!),
      revision: parseRevision('2'),
      metadataFreshness: 'complete' as const,
    };
    const failed: Extract<TemplateGroupState, { kind: 'partial' }> = {
      kind: 'partial',
      accepted: intermediate,
      nextChild: 1,
      reason: 'child-failed',
    };
    const changed = {
      ...intermediate,
      document: {
        ...intermediate.document,
        content: {
          ...intermediate.document.content,
          added: { sectionType: 'language' as const, entries: [] },
        },
      },
      revision: parseRevision('3'),
    };

    for (const action of ['retry-remaining', 'restore-pre-apply'] as const) {
      expect(
        templateRecoveryAvailable(captured, failed, intermediate, action),
      ).toBe(true);
      expect(templateRecoveryAvailable(captured, failed, changed, action))
        .toBe(false);
      expect(
        recoverTemplateGroup(captured, failed, changed, action, {
          nowEpochMs: () => 0,
          uuid: () => 'unused',
          delay: async () => {},
        }),
      ).toEqual({ kind: 'unavailable', reason: 'context-changed' });
    }
  });

  it('reports each child as applied only from the accepted resume', () => {
    const captured = group()!;
    const [structure, customization] = captured.children;
    const intermediate = replayCommand(captured.preApply, structure!);

    expect(templateChildApplied(structure!, captured.preApply)).toBe(false);
    expect(templateChildApplied(structure!, intermediate)).toBe(true);
    expect(templateChildApplied(customization!, intermediate)).toBe(false);
    expect(templateChildApplied(customization!, captured.intendedFinal))
      .toBe(true);
  });

  it('captures structure and customization child context separately', () => {
    const captured = group()!;
    const [structure, customization] = captured.children;

    expect(structure?.base.context).toMatchObject({
      customization: { present: true },
      contentIdentity: { present: true },
    });
    expect(customization?.base.context).toMatchObject({
      placement: { present: true },
      contentIdentity: { present: true },
    });
  });
});
