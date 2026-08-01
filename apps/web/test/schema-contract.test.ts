// schema-contract.test.ts is the web-side half of the cross-language
// conformance tripwire (design spec §3, "Codegen fidelity"). Before this
// file existed, packages/schema/test/conformance.test.ts already proved
// the generated TS union (packages/schema/gen/ts/resume.ts) matches
// resume.schema.json — but that suite runs only inside packages/schema.
// Nothing forced apps/web itself to depend on @aboutme/schema at all: the
// web app could grow its own parallel section-type model, drift from the
// schema, and packages/schema's conformance suite would stay green the
// whole time, because it was never testing the web app.
//
// This file closes that gap from the web app's side. It has two halves,
// matching two different, independent failure modes, both exercised by
// `npm run test`:
//
//   1. Compile-time: exhaustivenessFixtureSource() builds a throwaway .ts
//      fixture — a switch with a case per KNOWN_SECTION_TYPES entry and a
//      `default` branch assigning the unmatched value to a `never`-typed
//      binding — that imports the REAL generated `Section` type from
//      '@aboutme/schema' (a genuine dependency — see package.json's
//      "file:../../packages/schema" entry, resolved through normal
//      `npm install`/`npm ci`, not a copy-pasted type). The
//      "compiles the real Section union" test below writes it to disk and
//      runs `tsc --noEmit --strict` on it directly (mirroring
//      packages/schema/test/conformance.test.ts's
//      assertTsRejectsForeignField pattern), deliberately bypassing the
//      project tsconfig with `--ignoreConfig` rather than going through
//      `npm run typecheck`: Nuxt's generated tsconfig.app.json only
//      `include`s `test/nuxt/**/*`, not this directory, so vue-tsc never
//      sees apps/web/test/*.test.ts at all (confirmed empirically —
//      `npm run typecheck` alone does NOT catch a missing case here). If
//      packages/schema/gen/ts/resume.ts ever grows a ninth `Section`
//      variant not in KNOWN_SECTION_TYPES, the fixture's switch no longer
//      exhausts `Section`, the `never` assignment fails to compile, tsc
//      exits non-zero, and this vitest test fails.
//   2. Runtime: sectionTypesFromSchemaFile() reads resume.schema.json off
//      disk directly (independently of both generate.mjs and
//      packages/schema/test/conformance.test.ts's own derivation — the
//      point is to check the pipeline against the schema file itself,
//      not trust a derivation already trusted elsewhere) and compares it
//      against KNOWN_SECTION_TYPES, the same literal set both dispatch()
//      and the compile-time fixture above are built from. A sectionType
//      added to the schema shows up here even before anyone updates this
//      file, so `npm run test` fails loudly instead of silently ignoring
//      the new type at runtime.
import { execFileSync } from 'node:child_process';
import { readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import type { Section } from '@aboutme/schema';

const here = dirname(fileURLToPath(import.meta.url));
// apps/web/test -> apps/web -> apps -> repo root -> packages/schema/...
const schemaFilePath = join(
  here, '..', '..', '..', 'packages', 'schema', 'resume.schema.json',
);
const tscBin = join(here, '..', 'node_modules', '.bin', 'tsc');

type SectionTypeLiteral = Section['sectionType'];

// The literal set both dispatch() and the compile-time fixture below are
// written against. Kept as a plain array (not derived from the Section
// type, which TypeScript can't turn into a runtime value) so it can be
// compared against the independently-read schema file at runtime, and
// used to generate the compile-time exhaustiveness fixture.
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

// dispatch is the exhaustive switch a real production consumer of Section
// must keep in sync with the generated union — apps/web has no section
// renderer wired up yet (see design spec §5: components/resume/ is a
// later phase), so this test file is where that discipline is enforced
// for now. Every case matches a literal from the real generated Section
// union, never a web-local copy of the discriminator strings.
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
      // Compile-time exhaustiveness (documentation only here — the actual
      // enforcement is the tsc-on-a-fixture test below, since vitest does
      // not type-check): if Section ever grows a variant not handled
      // above, `section` is no longer assignable to `never`.
      const exhaustive: never = section;
      throw new Error(
        `schema-contract: unhandled sectionType ${JSON.stringify(exhaustive)}`,
      );
    }
  }
}

function minimalSection(sectionType: SectionTypeLiteral): Section {
  // A minimal, draft-permissive section+entry (design spec §3: only `id`
  // and the section's own sectionType are required). Cast through
  // `as Section` per variant is unnecessary here because the
  // discriminant literal picks the matching union member structurally.
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

// exhaustivenessFixtureSource generates a standalone .ts source: a
// dispatch switch with one case per `sectionTypes` entry and a
// `never`-typed default branch, importing the real `Section` type. Kept
// as a generator (not a static file) so the fixture's case list always
// matches KNOWN_SECTION_TYPES exactly, with no risk of the two drifting.
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
  });
});
