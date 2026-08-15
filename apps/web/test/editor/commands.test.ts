import type { Section } from '@aboutme/schema';
import { describe, expect, it } from 'vitest';

import { captureCommand, replayCommand } from '../../app/editor/commands';
import type {
  AtomicCommandIntent,
  CaptureCommandInput,
} from '../../app/editor/commands';
import type { EditorRuntime } from '../../app/editor/types';
import { acceptedFixture } from './fixture';

const runtime: EditorRuntime = {
  nowEpochMs: () => 10,
  uuid: () => 'command-1',
  delay: async () => {},
};

function capture(intent: AtomicCommandIntent) {
  const accepted = acceptedFixture();
  const input: CaptureCommandInput = {
    resumeId: accepted.metadata.id,
    ownerId: 'owner-1',
    sequence: 1,
    dependencyIds: ['earlier-command'],
    intent,
  };
  return { accepted, command: captureCommand(accepted, input, runtime) };
}

const workEntry: Section['entries'][number] = {
  id: '11111111-1111-4111-8111-111111111111',
  jobTitle: 'Engineer',
};

describe('command capture', () => {
  it('captures before replay and preserves absence distinctly', () => {
    const accepted = acceptedFixture({
      document: {
        ...acceptedFixture().document,
        personalDetails: {
          ...acceptedFixture().document.personalDetails,
          headline: 'Original',
        },
      },
    });
    const command = captureCommand(
      accepted,
      {
        resumeId: accepted.metadata.id,
        ownerId: 'owner-1',
        sequence: 1,
        dependencyIds: [],
        intent: {
          kind: 'personalField',
          path: 'headline',
          value: { present: false },
        },
      },
      runtime,
    );

    expect(command.base.target).toEqual({ present: true, value: 'Original' });
    expect(command.intended?.target).toEqual({ present: false });
    expect(
      replayCommand(accepted, command).document.personalDetails,
    ).not.toHaveProperty('headline');
    expect(accepted.document.personalDetails).toHaveProperty('headline');
  });

  it.each<AtomicCommandIntent>([
    { kind: 'metadataField', field: 'title', value: 'New title' },
    { kind: 'metadataField', field: 'lng', value: null },
    {
      kind: 'personalField',
      path: 'fullName',
      value: { present: true, value: '' },
    },
    {
      kind: 'personalField',
      path: 'details',
      value: { present: true, value: [] },
    },
    {
      kind: 'entryField',
      sectionKey: 'work',
      entryId: workEntry.id,
      path: 'jobTitle',
      value: { present: true, value: 'Staff engineer' },
    },
    { kind: 'entryUpsert', sectionKey: 'work', entry: workEntry },
    { kind: 'entryDelete', sectionKey: 'work', entryId: workEntry.id },
    { kind: 'entryReorder', sectionKey: 'work', entryIds: [workEntry.id] },
    {
      kind: 'sectionMetadata',
      sectionKey: 'work',
      change: { field: 'displayName', value: '' },
    },
    {
      kind: 'sectionMetadata',
      sectionKey: 'work',
      change: { field: 'iconKey', value: null },
    },
    {
      kind: 'structure',
      commands: [
        {
          op: 'createSection',
          key: 'work',
          sectionType: 'work',
          column: 'main',
          index: 0,
        },
      ],
    },
    {
      kind: 'customization',
      deltas: [{ op: 'set', path: 'spacing.entryGap', value: 0 }],
    },
    { kind: 'photoCrop', crop: null },
    { kind: 'photoUpload', file: new File(['photo'], 'photo.png') },
    { kind: 'photoDelete' },
    { kind: 'resumeDelete', confirmedTitle: 'Fixture' },
  ])('captures immutable data-only %s commands', (intent) => {
    const prepared
      = intent.kind === 'entryField'
        || intent.kind === 'entryDelete'
        || intent.kind === 'entryReorder'
        || intent.kind === 'sectionMetadata'
        ? acceptedFixture({ document: withWorkEntry() })
        : acceptedFixture();
    const command = captureCommand(
      prepared,
      {
        resumeId: prepared.metadata.id,
        ownerId: 'owner-1',
        sequence: 1,
        dependencyIds: ['earlier-command'],
        intent,
      },
      runtime,
    );

    expect(command).toMatchObject({
      ...intent,
      id: 'command-1',
      resumeId: 'resume-1',
      ownerId: 'owner-1',
      sequence: 1,
      dependencyIds: ['earlier-command'],
    });
    expect(Object.isFrozen(command)).toBe(true);
    if (intent.kind === 'photoUpload') {
      expect(command.intended).toBeNull();
    } else {
      expect(command.intended).not.toBeNull();
    }
  });

  it('uses a canonical target key instead of command identity', () => {
    const { command: first } = capture({
      kind: 'metadataField',
      field: 'title',
      value: 'A',
    });
    const { command: second } = capture({
      kind: 'metadataField',
      field: 'title',
      value: 'B',
    });
    expect(first.targetKey).toBe('metadata:title');
    expect(second.targetKey).toBe(first.targetKey);
  });

  it('binds the authenticated owner to destructive command context', () => {
    const { command } = capture({
      kind: 'resumeDelete',
      confirmedTitle: 'Fixture',
    });

    expect(command.base.context.ownerId).toEqual({
      present: true,
      value: 'owner-1',
    });
    expect(command.intended).toEqual({
      target: { present: false },
      context: { ownerId: { present: true, value: 'owner-1' } },
    });
  });

  it('preserves owner metadata booleans through capture and replay', () => {
    const { accepted, command } = capture({
      kind: 'metadataField',
      field: 'title',
      value: 'Changed',
    });
    const replayed = replayCommand(accepted, command);

    expect(accepted.metadata).toMatchObject({
      downloadEnabled: false,
      seoGeoEnabled: false,
    });
    expect(replayed.metadata).toMatchObject({
      downloadEnabled: false,
      seoGeoEnabled: false,
    });
  });
});

function withWorkEntry() {
  const document = acceptedFixture().document;
  return {
    ...document,
    content: {
      work: { sectionType: 'work' as const, entries: [workEntry] },
    },
    customization: {
      ...document.customization,
      layout: {
        ...document.customization.layout,
        sections: { main: ['work'], sidebar: [] },
      },
    },
  };
}
