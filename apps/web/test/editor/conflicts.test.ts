import { describe, expect, it } from 'vitest';

import { captureCommand } from '../../app/editor/commands';
import {
  createReplacementCommand,
  reconcileCommand,
} from '../../app/editor/reconcile';
import type { EditorRuntime } from '../../app/editor/types';
import { acceptedFixture } from './fixture';

const runtime: EditorRuntime = {
  nowEpochMs: () => 0,
  uuid: () => 'replacement-1',
  delay: async () => {},
};

describe('conflict replacements', () => {
  it('rebases a field override onto a fresh target', () => {
    const accepted = acceptedFixture();
    const command = captureCommand(
      accepted,
      {
        resumeId: accepted.metadata.id,
        ownerId: 'owner-1',
        sequence: 1,
        dependencyIds: [],
        intent: { kind: 'metadataField', field: 'title', value: 'Mine' },
      },
      runtime,
    );
    const latest = {
      ...accepted,
      metadata: { ...accepted.metadata, title: 'Theirs' },
    };
    const decision = reconcileCommand(command, latest);

    expect(decision.kind).toBe('conflict');
    if (
      decision.kind !== 'conflict'
      || decision.conflict.subject !== 'atomic'
    ) {
      return;
    }
    const replacement = createReplacementCommand(decision.conflict, latest, {
      kind: 'field',
    });

    expect(replacement?.base.target).toEqual({
      present: true,
      value: 'Theirs',
    });
    expect(replacement?.intended?.target).toEqual({
      present: true,
      value: 'Mine',
    });
  });

  it('requires exact destructive reconfirmation', () => {
    const accepted = acceptedFixture();
    const command = captureCommand(
      accepted,
      {
        resumeId: accepted.metadata.id,
        ownerId: 'owner-1',
        sequence: 1,
        dependencyIds: [],
        intent: { kind: 'resumeDelete', confirmedTitle: 'Fixture' },
      },
      runtime,
    );
    const latest = {
      ...accepted,
      metadata: { ...accepted.metadata, title: 'Renamed' },
    };
    const decision = reconcileCommand(command, latest);

    expect(decision.kind).toBe('conflict');
    if (
      decision.kind !== 'conflict'
      || decision.conflict.subject !== 'atomic'
    ) {
      return;
    }
    expect(
      createReplacementCommand(decision.conflict, latest, {
        kind: 'destructive',
        latestTitle: 'Fixture',
      }),
    ).toBeNull();
  });

  it('classifies missing entry identity and changed crop photo context', () => {
    const accepted = entryFixture();
    const entry = captureCommand(
      accepted,
      {
        resumeId: accepted.metadata.id,
        ownerId: 'owner-1',
        sequence: 1,
        dependencyIds: [],
        intent: {
          kind: 'entryField',
          sectionKey: 'work',
          entryId: 'entry-1',
          path: 'jobTitle',
          value: { present: true, value: 'Staff' },
        },
      },
      runtime,
    );
    const missing = {
      ...accepted,
      document: { ...accepted.document, content: {} },
    };
    const crop = captureCommand(
      accepted,
      {
        resumeId: accepted.metadata.id,
        ownerId: 'owner-1',
        sequence: 2,
        dependencyIds: [],
        intent: {
          kind: 'photoCrop',
          crop: { x: 1, y: 1, width: 2, height: 2 },
        },
      },
      runtime,
    );
    const changedPhoto = {
      ...accepted,
      document: {
        ...accepted.document,
        personalDetails: {
          ...accepted.document.personalDetails,
          photo: { key: 'photo-2' },
        },
      },
    };

    expect(reconcileCommand(entry, missing)).toMatchObject({
      kind: 'conflict',
      conflict: { kind: 'identity-missing' },
    });
    expect(reconcileCommand(crop, changedPhoto)).toMatchObject({
      kind: 'conflict',
      conflict: { kind: 'photo-changed' },
    });
  });
});

function entryFixture() {
  const fixture = acceptedFixture();
  return {
    ...fixture,
    document: {
      ...fixture.document,
      content: {
        work: {
          sectionType: 'work' as const,
          entries: [{ id: 'entry-1', jobTitle: 'Engineer' }],
        },
      },
      customization: {
        ...fixture.document.customization,
        layout: {
          ...fixture.document.customization.layout,
          sections: { main: ['work'], sidebar: [] },
        },
      },
      personalDetails: {
        ...fixture.document.personalDetails,
        photo: { key: 'photo-1' },
      },
    },
  };
}
