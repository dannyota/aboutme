import { describe, expect, it } from 'vitest';

import { equalProjection, projectIntent } from '../../app/editor/commands';
import { acceptedFixture } from './fixture';

describe('command projections', () => {
  it(
    'keeps absence, undefined, null, empty strings, zero, and order distinct',
    () => {
      const absent = { target: { present: false as const }, context: {} };
      const undefinedValue = {
        target: { present: true as const, value: undefined },
        context: {},
      };
      const nullValue = {
        target: { present: true as const, value: null },
        context: {},
      };
      const emptyValue = {
        target: { present: true as const, value: '' },
        context: {},
      };

      expect(equalProjection(absent, undefinedValue)).toBe(false);
      expect(equalProjection(undefinedValue, nullValue)).toBe(false);
      expect(equalProjection(nullValue, emptyValue)).toBe(false);
      expect(
        equalProjection(
          { target: { present: true, value: [0, 1] }, context: {} },
          { target: { present: true, value: [1, 0] }, context: {} },
        ),
      ).toBe(false);
    },
  );

  it(
    'captures entry identity and membership context from placement order',
    () => {
      const snapshot = acceptedFixture({
        document: documentWithWorkAndProfile(),
      });
      const projection = projectIntent(snapshot, {
        kind: 'entryField',
        sectionKey: 'work',
        entryId: '11111111-1111-4111-8111-111111111111',
        path: 'jobTitle',
        value: { present: true, value: 'Staff engineer' },
      });

      expect(projection.target).toEqual({ present: true, value: 'Engineer' });
      expect(projection.context).toMatchObject({
        resumeId: { present: true, value: 'resume-1' },
        schemaVersion: { present: true, value: 2 },
        sectionKey: { present: true, value: 'work' },
        sectionType: { present: true, value: 'work' },
        entryId: {
          present: true,
          value: '11111111-1111-4111-8111-111111111111',
        },
        membership: { present: true, value: 'work' },
      });
    },
  );

  it(
    'captures photo crop with exact photo-key context and an opaque upload',
    () => {
      const document = acceptedFixture().document;
      const snapshot = acceptedFixture({
        document: {
          ...document,
          personalDetails: {
            ...document.personalDetails,
            photo: {
              key: 'private/photo.png',
              crop: { x: 0, y: 1, width: 2, height: 3 },
            },
          },
        },
      });

      expect(
        projectIntent(snapshot, { kind: 'photoCrop', crop: null }),
      ).toMatchObject({
        target: {
          present: true,
          value: { x: 0, y: 1, width: 2, height: 3 },
        },
        context: { photoKey: { present: true, value: 'private/photo.png' } },
      });
    },
  );

  it(
    'projects destructive target capture and placement deterministically',
    () => {
      const snapshot = acceptedFixture({
        document: documentWithWorkAndProfile(),
      });
      const deletion = projectIntent(snapshot, {
        kind: 'resumeDelete',
        confirmedTitle: 'Fixture',
      });
      const structure = projectIntent(snapshot, {
        kind: 'structure',
        commands: [
          { op: 'moveSection', key: 'work', column: 'sidebar', index: 0 },
        ],
      });

      expect(deletion.target).toEqual({ present: true, value: snapshot });
      expect(deletion.context.ownerId).toEqual({ present: false });
      expect(structure.target).toMatchObject({
        present: true,
        value: { placement: { main: ['profile', 'work'], sidebar: [] } },
      });
    },
  );

  it('keeps created section absence in the structural target', () => {
    const snapshot = acceptedFixture();
    const projection = projectIntent(snapshot, {
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
    });

    expect(projection.target).toMatchObject({
      present: true,
      value: {
        sections: [{ key: 'work', value: { present: false } }],
      },
    });
  });
});

function documentWithWorkAndProfile() {
  const document = acceptedFixture().document;
  return {
    ...document,
    content: {
      profile: { sectionType: 'profile' as const, entries: [] },
      work: {
        sectionType: 'work' as const,
        entries: [
          { id: '11111111-1111-4111-8111-111111111111', jobTitle: 'Engineer' },
        ],
      },
    },
    customization: {
      ...document.customization,
      layout: {
        ...document.customization.layout,
        sections: { main: ['profile', 'work'], sidebar: [] },
      },
    },
  };
}
