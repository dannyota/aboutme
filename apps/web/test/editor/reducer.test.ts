import { describe, expect, it } from 'vitest';

import { applyIntent, replayCommand } from '../../app/editor/commands';
import type { AtomicEditorCommand } from '../../app/editor/commands';
import { acceptedFixture } from './fixture';

describe('command reducer', () => {
  it(
    'materializes a complete entry without changing its sibling entries',
    () => {
      const snapshot = acceptedFixture({ document: documentWithWork() });
      const next = applyIntent(snapshot, {
        kind: 'entryField',
        sectionKey: 'work',
        entryId: '11111111-1111-4111-8111-111111111111',
        path: 'jobTitle',
        value: { present: true, value: 'Staff engineer' },
      });

      const entries = next.document.content.work.entries;
      expect(entries).toEqual([
        {
          id: '11111111-1111-4111-8111-111111111111',
          jobTitle: 'Staff engineer',
        },
        { id: '22222222-2222-4222-8222-222222222222', jobTitle: 'Designer' },
      ]);
      expect(snapshot.document.content.work.entries[0].jobTitle).toBe(
        'Engineer',
      );
    },
  );

  it(
    'applies ordered structure operations without reading content key order',
    () => {
      const snapshot = acceptedFixture();
      const next = applyIntent(snapshot, {
        kind: 'structure',
        commands: [
          {
            op: 'createSection',
            key: 'custom-a',
            sectionType: 'custom',
            column: 'sidebar',
            index: 0,
            displayName: '',
          },
          {
            op: 'createSection',
            key: 'work',
            sectionType: 'work',
            column: 'main',
            index: 0,
          },
          { op: 'moveSection', key: 'work', column: 'sidebar', index: 1 },
          {
            op: 'reorderColumn',
            column: 'sidebar',
            keys: ['work', 'custom-a'],
          },
          { op: 'deleteSection', key: 'custom-a' },
        ],
      });

      expect(next.document.customization.layout.sections).toEqual({
        main: [],
        sidebar: ['work'],
      });
      expect(next.document.content).toEqual({
        work: { sectionType: 'work', entries: [] },
      });
    },
  );

  it(
    'sets and unsets optional customization values without normalizing absence',
    () => {
      const snapshot = acceptedFixture();
      const next = applyIntent(snapshot, {
        kind: 'customization',
        deltas: [
          { op: 'set', path: 'colors.accent', value: '#ff0000' },
          { op: 'set', path: 'spacing.pageMargin.x', value: 0 },
          { op: 'set', path: 'spacing.pageMargin.y', value: 0 },
          { op: 'unset', path: 'spacing.pageMargin' },
        ],
      });

      expect(next.document.customization.colors.accent).toBe('#ff0000');
      expect(next.document.customization.spacing).not.toHaveProperty(
        'pageMargin',
      );
      expect(snapshot.document.customization.colors).not.toHaveProperty(
        'accent',
      );
    },
  );

  it(
    'replays without mutating a captured command or its input snapshot',
    () => {
      const snapshot = acceptedFixture();
      const command = {
        kind: 'metadataField',
        field: 'title',
        value: 'Changed',
        id: 'command-1',
        resumeId: snapshot.metadata.id,
        ownerId: 'owner-1',
        sequence: 1,
        targetKey: 'metadata:title',
        base: { target: { present: true, value: 'Fixture' }, context: {} },
        intended: { target: { present: true, value: 'Changed' }, context: {} },
        dependencyIds: [],
      } as AtomicEditorCommand;
      const replayed = replayCommand(snapshot, command);

      expect(replayed.metadata.title).toBe('Changed');
      expect(snapshot.metadata.title).toBe('Fixture');
      expect(command.value).toBe('Changed');
    },
  );
});

function documentWithWork() {
  const document = acceptedFixture().document;
  return {
    ...document,
    content: {
      work: {
        sectionType: 'work' as const,
        entries: [
          { id: '11111111-1111-4111-8111-111111111111', jobTitle: 'Engineer' },
          { id: '22222222-2222-4222-8222-222222222222', jobTitle: 'Designer' },
        ],
      },
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
