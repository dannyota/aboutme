import { describe, expect, it } from 'vitest';

import { captureCommand, replayCommand } from '../../app/editor/commands';
import type {
  AtomicEditorCommand,
  CaptureCommandInput,
} from '../../app/editor/commands';
import { coalescePending } from '../../app/editor/coalesce';
import type { EditorRuntime } from '../../app/editor/types';
import { acceptedFixture } from './fixture';

function coalesceFixtures(): readonly [
  AtomicEditorCommand,
  AtomicEditorCommand,
  AtomicEditorCommand,
] {
  const accepted = acceptedFixture();
  let id = 0;
  const runtime: EditorRuntime = {
    nowEpochMs: () => 0,
    uuid: () => `c-${++id}`,
    delay: async () => {},
  };
  const input = (
    value: string,
    sequence: number,
    dependencyIds: readonly string[],
  ): CaptureCommandInput => ({
    resumeId: accepted.metadata.id,
    ownerId: 'owner-1',
    sequence,
    dependencyIds,
    intent: { kind: 'metadataField', field: 'title', value },
  });
  const first = captureCommand(accepted, input('A', 1, ['base']), runtime);
  const last = captureCommand(
    replayCommand(accepted, first),
    input('B', 2, ['later']),
    runtime,
  );
  const destructive = captureCommand(
    accepted,
    {
      resumeId: accepted.metadata.id,
      ownerId: 'owner-1',
      sequence: 3,
      dependencyIds: [],
      intent: {
        kind: 'resumeDelete',
        confirmedTitle: accepted.metadata.title,
      },
    },
    runtime,
  );
  return [first, last, destructive];
}

describe('pending command coalescing', () => {
  it('coalesces only an adjacent unsent value command', () => {
    const [first, last, destructive] = coalesceFixtures();
    const merged = coalescePending([first], last);

    expect(merged).toEqual([
      {
        ...last,
        id: first.id,
        sequence: first.sequence,
        base: first.base,
        dependencyIds: first.dependencyIds,
      },
    ]);
    expect(coalescePending(merged, destructive)).toEqual([
      ...merged,
      destructive,
    ]);
    expect(first).toEqual(coalesceFixtures()[0]);
  });

  it.each([
    { kind: 'entryUpsert', sectionKey: 'work', entry: { id: 'x' } },
    { kind: 'structure', commands: [] },
    { kind: 'photoDelete' },
  ] as const)(
    'does not coalesce destructive or structural %o commands',
    (intent) => {
      const [first] = coalesceFixtures();
      const accepted = acceptedFixture();
      const candidate = captureCommand(
        accepted,
        {
          resumeId: accepted.metadata.id,
          ownerId: 'owner-1',
          sequence: 4,
          dependencyIds: [],
          intent,
        },
        { nowEpochMs: () => 0, uuid: () => 'other', delay: async () => {} },
      );

      expect(coalescePending([first], candidate)).toEqual([first, candidate]);
    },
  );
});
