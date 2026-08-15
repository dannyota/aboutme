import { describe, expect, it } from 'vitest';

import {
  captureCommand,
  replayCommand,
} from '../../app/editor/commands';
import { reconcileCommand } from '../../app/editor/reconcile';
import type { AtomicCommandIntent } from '../../app/editor/commands';
import type { EditorRuntime } from '../../app/editor/types';
import { acceptedFixture } from './fixture';

const runtime: EditorRuntime = {
  nowEpochMs: () => 0,
  uuid: () => 'command-1',
  delay: async () => {},
};

function commandFor(intent: Parameters<typeof captureCommand>[1]['intent']) {
  const accepted = acceptedFixture();
  const command = captureCommand(
    accepted,
    {
      resumeId: accepted.metadata.id,
      ownerId: 'owner-1',
      sequence: 1,
      dependencyIds: [],
      intent,
    },
    runtime,
  );
  return { accepted, command };
}

describe('reconcileCommand', () => {
  it('uses intended, then base, then conflict ordering', () => {
    const { accepted, command } = commandFor({
      kind: 'metadataField',
      field: 'title',
      value: 'Mine',
    });
    const intended = replayCommand(accepted, command);

    expect(reconcileCommand(command, intended)).toEqual({ kind: 'satisfied' });
    expect(reconcileCommand(command, accepted)).toEqual({ kind: 'safe-base' });
    const changed = {
      ...accepted,
      metadata: { ...accepted.metadata, title: 'Theirs' },
    };
    expect(reconcileCommand(command, changed).kind).toBe('conflict');
  });

  it('never marks opaque photo upload satisfied from a read', () => {
    const { accepted, command } = commandFor({
      kind: 'photoUpload',
      file: new File(['x'], 'x.png'),
    });

    expect(reconcileCommand(command, accepted)).toEqual({ kind: 'safe-base' });
  });

  it.each<AtomicCommandIntent>([
    { kind: 'metadataField', field: 'title', value: 'Mine' },
    { kind: 'metadataField', field: 'lng', value: 'fr' },
    {
      kind: 'personalField',
      path: 'headline',
      value: { present: true, value: 'Mine' },
    },
    {
      kind: 'entryField',
      sectionKey: 'work',
      entryId: 'entry-1',
      path: 'jobTitle',
      value: { present: true, value: 'Staff' },
    },
    {
      kind: 'entryUpsert',
      sectionKey: 'work',
      entry: { id: 'entry-2', jobTitle: 'New' },
    },
    { kind: 'entryDelete', sectionKey: 'work', entryId: 'entry-1' },
    {
      kind: 'entryReorder',
      sectionKey: 'work',
      entryIds: ['entry-2', 'entry-1'],
    },
    {
      kind: 'sectionMetadata',
      sectionKey: 'work',
      change: { field: 'displayName', value: 'Experience' },
    },
    {
      kind: 'structure',
      commands: [
        { op: 'moveSection', key: 'work', column: 'sidebar', index: 0 },
      ],
    },
    {
      kind: 'customization',
      deltas: [{ op: 'set', path: 'spacing.entryGap', value: 3 }],
    },
    { kind: 'photoCrop', crop: { x: 1, y: 1, width: 2, height: 2 } },
    { kind: 'photoDelete' },
  ])('orders intended, base, and conflict for %s', (intent) => {
    const accepted = complexFixture();
    const command = captureCommand(
      accepted,
      {
        resumeId: accepted.metadata.id,
        ownerId: 'owner-1',
        sequence: 1,
        dependencyIds: [],
        intent,
      },
      runtime,
    );
    const intended = replayCommand(accepted, command);
    const changed = {
      ...accepted,
      metadata: { ...accepted.metadata, id: 'changed-resume' },
    };

    expect(reconcileCommand(command, intended)).toEqual({ kind: 'satisfied' });
    expect(reconcileCommand(command, accepted)).toEqual({ kind: 'safe-base' });
    expect(reconcileCommand(command, changed).kind).toBe('conflict');
  });

  it('keeps an unchanged resumed owner delete safe to dispatch', () => {
    const accepted = complexFixture();
    const command = captureCommand(
      accepted,
      {
        resumeId: accepted.metadata.id,
        ownerId: 'owner-1',
        sequence: 1,
        dependencyIds: [],
        intent: {
          kind: 'resumeDelete',
          confirmedTitle: accepted.metadata.title,
        },
      },
      runtime,
    );

    expect(reconcileCommand(command, accepted)).toEqual({ kind: 'safe-base' });
  });
});

function complexFixture() {
  const fixture = acceptedFixture();
  return {
    ...fixture,
    document: {
      ...fixture.document,
      content: {
        work: {
          sectionType: 'work' as const,
          displayName: 'Work',
          entries: [
            { id: 'entry-1', jobTitle: 'Engineer' },
            { id: 'entry-2', jobTitle: 'Manager' },
          ],
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
