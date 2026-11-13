import { execFileSync } from 'node:child_process';
import { readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import type { Section } from '@aboutme/schema';

const here = dirname(fileURLToPath(import.meta.url));
const schemaFilePath = join(
  here, '..', '..', '..', 'packages', 'schema', 'resume.schema.json',
);
const tscBin = join(here, '..', 'node_modules', '.bin', 'tsc');

type SectionTypeLiteral = Section['sectionType'];

const KNOWN_SECTION_TYPES: readonly SectionTypeLiteral[] = [
  'profile',
  'work',
  'education',
  'skill',
  'language',
  'certificate',
  'project',
  'custom',
] as const;

function dispatch(section: Section): string {
  switch (section.sectionType) {
    case 'profile':
      return 'profile';
    case 'work':
      return 'work';
    case 'education':
      return 'education';
    case 'skill':
      return 'skill';
    case 'language':
      return 'language';
    case 'certificate':
      return 'certificate';
    case 'project':
      return 'project';
    case 'custom':
      return 'custom';
    default: {
      // vitest does not type-check; the tsc fixture test below enforces this.
      const exhaustive: never = section;
      throw new Error(
        `schema-contract: unhandled sectionType ${JSON.stringify(exhaustive)}`,
      );
    }
  }
}

function minimalSection(sectionType: SectionTypeLiteral): Section {
  return {
    sectionType,
    displayName: 'Section',
    iconKey: 'star',
    entries: [{ id: '018f0000-0000-7000-8000-000000000099' }],
  } as Section;
}

interface SchemaSectionOneOfBranch {
  properties?: {
    sectionType?: {
      const?: unknown;
    };
  };
}

function sectionTypesFromSchemaFile(): string[] {
  const raw = readFileSync(schemaFilePath, 'utf8');
  const schema = JSON.parse(raw) as {
    $defs?: { section?: { oneOf?: SchemaSectionOneOfBranch[] } };
  };
  const oneOf = schema.$defs?.section?.oneOf;
  if (!Array.isArray(oneOf) || oneOf.length === 0) {
    throw new Error(
      `schema-contract: ${schemaFilePath} `
      + '$defs.section.oneOf is missing or empty',
    );
  }
  return oneOf.map((branch, index) => {
    const sectionType = branch.properties?.sectionType?.const;
    if (typeof sectionType !== 'string') {
      throw new Error(
        `schema-contract: ${schemaFilePath} $defs.section.oneOf[${index}] `
        + 'has no properties.sectionType.const',
      );
    }
    return sectionType;
  });
}

function exhaustivenessFixtureSource(
  sectionTypes: readonly string[],
): string {
  const cases = sectionTypes
    .map((t) => {
      const literal = JSON.stringify(t);
      return `    case ${literal}:\n      return ${literal};`;
    })
    .join('\n');
  return [
    'import type { Section } from \'@aboutme/schema\';',
    '',
    'function dispatch(section: Section): string {',
    '  switch (section.sectionType) {',
    cases,
    '    default: {',
    '      const exhaustive: never = section;',
    '      throw new Error(String(exhaustive));',
    '    }',
    '  }',
    '}',
    'void dispatch;',
    '',
  ].join('\n');
}

describe('schema contract: apps/web recognizes every sectionType', () => {
  it('reads at least one sectionType from resume.schema.json', () => {
    expect(sectionTypesFromSchemaFile().length).toBeGreaterThan(0);
  });

  it('KNOWN_SECTION_TYPES matches resume.schema.json (both directions)', () => {
    const schemaTypes = [...sectionTypesFromSchemaFile()].sort();
    const knownTypes = [...KNOWN_SECTION_TYPES].sort();
    expect(
      schemaTypes,
      'resume.schema.json declares a sectionType set that no longer '
      + 'matches this file\'s KNOWN_SECTION_TYPES / dispatch() — a '
      + 'sectionType was added to (or removed from) the schema without '
      + 'updating this file',
    ).toEqual(knownTypes);
  });

  it.each(KNOWN_SECTION_TYPES)(
    'dispatch() handles a minimal %s section at runtime',
    (sectionType) => {
      expect(dispatch(minimalSection(sectionType))).toBe(sectionType);
    },
  );

  it('every schema sectionType is one dispatch() actually handles', () => {
    for (const sectionType of sectionTypesFromSchemaFile()) {
      expect(
        () => dispatch(minimalSection(sectionType as SectionTypeLiteral)),
        `apps/web does not recognize sectionType ${JSON.stringify(sectionType)}`
        + ' declared by resume.schema.json',
      ).not.toThrow();
    }
  });

  it('KNOWN_SECTION_TYPES exhausts Section at compile time (tsc)', () => {
    const fixturePath = join(here, 'schema-contract-exhaustiveness.tmp.ts');
    const fixtureSource = exhaustivenessFixtureSource(KNOWN_SECTION_TYPES);
    writeFileSync(fixturePath, fixtureSource);
    try {
      expect(() =>
        execFileSync(
          tscBin,
          [
            '--ignoreConfig',
            '--noEmit',
            '--strict',
            '--module', 'esnext',
            '--target', 'esnext',
            '--moduleResolution', 'bundler',
            fixturePath,
          ],
          { stdio: 'pipe' },
        ),
      ).not.toThrow();
    } finally {
      rmSync(fixturePath, { force: true });
    }
  }, 15_000);
});
