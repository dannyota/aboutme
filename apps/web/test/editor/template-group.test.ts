import { TEMPLATES } from '@aboutme/schema/templates';
import { describe, expect, it } from 'vitest';

import {
  advanceTemplateGroup,
  captureTemplateGroup,
  nextTemplateChild,
  recoverTemplateGroup,
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
