import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

import {
  parseCurrentDocument,
  UnknownDocumentVersionError,
} from '../../app/editor/documentValidation';

const fixture = (path: string): unknown =>
  JSON.parse(readFileSync(`../../packages/schema/fixtures/${path}`, 'utf8'));

describe('current document validation', () => {
  it('accepts the minimal current document', () => {
    const minimal = fixture('minimal.json');
    expect(parseCurrentDocument(structuredClone(minimal))).toEqual(minimal);
  });

  it('rejects a non-current version', () => {
    const minimal = fixture('minimal.json') as Record<string, unknown>;
    expect(() =>
      parseCurrentDocument({ ...minimal, schemaVersion: 1 }),
    ).toThrow('invalid current document');
  });

  it('rejects a fractional schema version without requesting a reload', () => {
    const minimal = fixture('minimal.json') as Record<string, unknown>;
    try {
      parseCurrentDocument({ ...minimal, schemaVersion: 2.5 });
      throw new Error('fractional schema version was accepted');
    } catch (error) {
      expect(error).not.toBeInstanceOf(UnknownDocumentVersionError);
      expect(error).toMatchObject({ message: 'invalid current document' });
    }
  });

  it.each([
    'invalid-missing-required.json',
    'store/invalid-layout-duplicate-across-arrays.json',
    'store/invalid-duplicate-entry-id.json',
    'store/invalid-personal-detail-url-scheme.json',
    'store/invalid-hostile-sectiontype-proto.json',
  ])('rejects invalid fixture %s', (name) => {
    expect(() => parseCurrentDocument(fixture(name))).toThrow(
      'invalid current document',
    );
  });
});
