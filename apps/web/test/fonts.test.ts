// fonts.test.ts — Task 5 asset and license checker (AC-REN-003,
// AC-FONT-001).
//
// The 26-row input matrix in docs/design/fonts.md is the frozen
// authority for this suite. Instead of a second hand transcription that
// could drift, the suite parses that document directly and asserts the
// committed manifest (app/assets/fonts/catalog.json), the vendored
// bytes, the runtime license tree, the generated CSS, and the runtime
// mapping against it. Font tables (name, fvar, OS/2, cmap) are
// re-parsed here with fontkit — a different parser than the fonttools
// pipeline that generated the assets — so recorded internal names,
// axes, and coverage are measured twice, independently, from the final
// bytes.
import { createHash } from 'node:crypto';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { create as createFontkitFont } from 'fontkit';
import { describe, expect, it } from 'vitest';
import { resolveFontSelection } from '../app/utils/fontCatalog';
import { fontsReady } from '../app/utils/fontsReady';
import catalogJson from '../app/assets/fonts/catalog.json';

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = join(here, '..');
const repoRoot = join(webRoot, '..', '..');
const fontsDir = join(webRoot, 'app', 'assets', 'fonts');
const licensesDir = join(webRoot, 'public', 'font-licenses');
const designPath = join(repoRoot, 'docs', 'design', 'fonts.md');
const fixturePath = join(here, 'fixtures', 'font-coverage.txt');

const sha256 = (data: Buffer | string): string =>
  createHash('sha256').update(data).digest('hex');

// ---------------------------------------------------------------------
// Design-matrix parsing (docs/design/fonts.md, Approved v4).
// ---------------------------------------------------------------------

interface DesignInput {
  readonly path: string;
  readonly sha256: string;
}

interface DesignRow {
  readonly rank: number;
  readonly id: string;
  readonly displayName: string;
  readonly category: string;
  readonly repository: string;
  readonly commits: readonly string[];
  readonly archiveRef: string | null;
  readonly inputs: readonly DesignInput[];
  readonly licensePath: string;
  readonly licenseSha256: string;
  readonly rfns: readonly string[];
  readonly policy: string;
  readonly v1Family: string;
}

interface DesignArchive {
  readonly ref: string;
  readonly url: string;
  readonly sha256: string;
}

const pathHashPairs = (cell: string): DesignInput[] =>
  [...cell.matchAll(/`([^`]+)`\s+—\s+`([0-9a-f]{64})`/g)]
    .map((m) => ({ path: m[1]!, sha256: m[2]! }));

const parseDesignRow = (line: string): DesignRow => {
  const cells = line.split('|').map((cell) => cell.trim());
  // cells[0] and cells[8] are the empty edges of the Markdown row.
  const idPattern = new RegExp(
    '^`([a-z0-9-]+)`\\s+—\\s+(.+);\\s*'
    + '(sans|serif|slab serif|display serif|monospace)$',
  );
  const idMatch = idPattern.exec(cells[2]!);
  if (idMatch === null) throw new Error(`bad ID cell: ${cells[2]}`);
  const repoMatches = [...cells[3]!.matchAll(
    /github\.com\/([A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+)\/tree\/([0-9a-f]{40})/g,
  )];
  if (repoMatches.length === 0) {
    throw new Error(`no pinned commit in: ${cells[3]}`);
  }
  const repoPaths = new Set(repoMatches.map((m) => m[1]!));
  if (repoPaths.size !== 1) {
    throw new Error(`multiple repositories in one row: ${cells[3]}`);
  }
  const archiveMatch = /Archive (A\d+),/.exec(cells[4]!);
  const licensePair = pathHashPairs(cells[5]!);
  if (licensePair.length !== 1) {
    throw new Error(`bad license cell: ${cells[5]}`);
  }
  const rfnMatch
    = /^(.+?)\s+\/\s+(unmodified-upstream|subset-original-name|subset-renamed)$/
      .exec(cells[6]!);
  if (rfnMatch === null) throw new Error(`bad RFN cell: ${cells[6]}`);
  return {
    rank: Number(cells[1]),
    id: idMatch[1]!,
    displayName: idMatch[2]!.trim(),
    category: idMatch[3]!,
    repository: `https://github.com/${[...repoPaths][0]}`,
    commits: repoMatches.map((m) => m[2]!),
    archiveRef: archiveMatch === null ? null : archiveMatch[1]!,
    inputs: pathHashPairs(cells[4]!),
    licensePath: licensePair[0]!.path,
    licenseSha256: licensePair[0]!.sha256,
    rfns: rfnMatch[1] === 'None' ? [] : rfnMatch[1]!.split(/,\s*/),
    policy: rfnMatch[2]!,
    v1Family: cells[7]!,
  };
};

const designText = readFileSync(designPath, 'utf8');
const designRows: readonly DesignRow[] = designText
  .split('\n')
  .filter((line) => /^\|\s*\d+\s*\|/.test(line))
  .map(parseDesignRow);
const designArchives: readonly DesignArchive[] = designText
  .split('\n')
  .map((line) =>
    /^\|\s*(A\d+)\s*\|\s*`([^`]+)`\s*\|\s*`([0-9a-f]{64})`\s*\|/.exec(line))
  .filter((m): m is RegExpExecArray => m !== null)
  .map((m) => ({ ref: m[1]!, url: m[2]!, sha256: m[3]! }));

// ---------------------------------------------------------------------
// Manifest shape.
// ---------------------------------------------------------------------

interface CatalogAsset {
  readonly path: string;
  readonly role: 'variable' | 'upright-400' | 'upright-700';
  readonly input: string;
  readonly sha256: string;
  readonly bytes: number;
  readonly weight: readonly [number, number];
  readonly axes: readonly {
    readonly tag: string;
    readonly min: number;
    readonly default: number;
    readonly max: number;
  }[];
  readonly internalNames: {
    readonly family: string;
    readonly subfamily: string;
    readonly fullName: string;
    readonly postScriptName: string;
  };
  readonly coverage: {
    readonly codepoints: number;
    readonly ranges: string;
  };
}

interface CatalogEntry {
  readonly id: string;
  readonly displayName: string;
  readonly category: string;
  readonly cssFamily: string;
  readonly policy: string;
  readonly spdx: string;
  readonly copyright: string;
  readonly reservedFontNames: readonly string[];
  readonly source: {
    readonly repository: string;
    readonly commit: string;
    readonly binaryCommit: string | null;
    readonly archive: {
      readonly ref: string;
      readonly url: string;
      readonly sha256: string;
    } | null;
    readonly inputs: readonly DesignInput[];
    readonly license: { readonly path: string; readonly sha256: string };
  };
  readonly licenseFile: {
    readonly runtimePath: string;
    readonly sha256: string;
  };
  readonly assets: readonly CatalogAsset[];
  readonly fixtureCoverage: {
    readonly complete: boolean;
    readonly missing: readonly string[];
  };
  readonly fallback: { readonly id: string; readonly cssFamily: string };
  readonly v1Family: string;
}

interface Catalog {
  readonly catalogVersion: number;
  readonly design: {
    readonly document: string;
    readonly revision: string;
    readonly sha256: string;
  };
  readonly generator: {
    readonly tool: string;
    readonly python: string;
    readonly fonttools: string;
    readonly brotli: string;
    readonly subsetUnicodes: string;
  };
  readonly fixture: { readonly path: string; readonly sha256: string };
  readonly excluded: readonly {
    readonly id: string;
    readonly reason: string;
  }[];
  readonly entries: readonly CatalogEntry[];
}

const catalog = catalogJson as unknown as Catalog;
const entryById = new Map(catalog.entries.map((e) => [e.id, e]));

const FALLBACK_BY_CATEGORY: Record<string, string> = {
  'sans': 'noto-sans',
  'serif': 'noto-serif',
  'slab serif': 'noto-serif',
  'display serif': 'noto-serif',
  'monospace': 'space-mono',
};

// ---------------------------------------------------------------------
// fontkit re-parse of the final bytes.
// ---------------------------------------------------------------------

interface FontLike {
  readonly 'copyright': string;
  readonly 'familyName': string;
  readonly 'subfamilyName': string;
  readonly 'fullName': string;
  readonly 'postscriptName': string;
  readonly 'characterSet': readonly number[];
  readonly 'variationAxes': Record<string, {
    readonly name: string;
    readonly min: number;
    readonly default: number;
    readonly max: number;
  }>;
  readonly 'OS/2': { readonly usWeightClass: number };
  'glyphForCodePoint'(codePoint: number): { readonly id: number };
}

const fontCache = new Map<string, { buf: Buffer; font: FontLike }>();
const loadAsset = (asset: CatalogAsset): { buf: Buffer; font: FontLike } => {
  const cached = fontCache.get(asset.path);
  if (cached !== undefined) return cached;
  const buf = readFileSync(join(fontsDir, asset.path));
  const font = createFontkitFont(buf) as unknown as FontLike;
  const loaded = { buf, font };
  fontCache.set(asset.path, loaded);
  return loaded;
};

const measuredCodepoints = (font: FontLike): number[] =>
  [...font.characterSet]
    .filter((cp) => cp !== 0xFFFF && font.glyphForCodePoint(cp).id !== 0)
    .sort((a, b) => a - b);

const formatRange = (start: number, end: number): string => {
  const hex = (value: number): string => value.toString(16).toUpperCase();
  return start === end ? hex(start) : `${hex(start)}-${hex(end)}`;
};

const toRanges = (codepoints: readonly number[]): string => {
  const parts: string[] = [];
  let start = -1;
  let prev = -1;
  for (const cp of codepoints) {
    if (start < 0) {
      start = cp;
      prev = cp;
      continue;
    }
    if (cp === prev + 1) {
      prev = cp;
      continue;
    }
    parts.push(formatRange(start, prev));
    start = cp;
    prev = cp;
  }
  if (start >= 0) parts.push(formatRange(start, prev));
  return parts.join(',');
};

const fixtureText = readFileSync(fixturePath, 'utf8');
const fixtureCodepoints: readonly number[] = [...new Set(
  [...fixtureText.replace(/[\r\n]/g, '')].map((ch) => ch.codePointAt(0)!),
)].sort((a, b) => a - b);

const entryCoverageIntersection = (entry: CatalogEntry): Set<number> => {
  const sets = entry.assets
    .map((asset) => new Set(measuredCodepoints(loadAsset(asset).font)));
  const first = sets[0]!;
  return new Set(
    [...first].filter((cp) => sets.every((set) => set.has(cp))),
  );
};

// ---------------------------------------------------------------------
// Suites.
// ---------------------------------------------------------------------

describe('design matrix', () => {
  it('parses the exact 26 frozen rows and 5 archives', () => {
    expect(designRows).toHaveLength(26);
    expect(designRows.map((row) => row.rank))
      .toEqual([...Array(26).keys()].map((i) => i + 1));
    expect(designArchives.map((archive) => archive.ref))
      .toEqual(['A1', 'A2', 'A3', 'A4', 'A5']);
    for (const archive of designArchives) {
      expect(archive.url).toMatch(/^https:\/\/github\.com\//);
    }
  });
});

describe('manifest schema', () => {
  it('has version 2, a design record, and generator pins', () => {
    expect(catalog.catalogVersion).toBe(2);
    expect(catalog.design.document).toBe('docs/design/fonts.md');
    expect(catalog.design.revision).toMatch(/^[0-9a-f]{40}$/);
    // Tripwire: a change to the frozen design without regeneration.
    expect(catalog.design.sha256).toBe(sha256(designText));
    expect(catalog.generator.python).toBe('3.14.7');
    expect(catalog.generator.fonttools).toBe('4.63.0');
    expect(catalog.generator.brotli).toBe('1.2.0');
    expect(catalog.generator.subsetUnicodes.length).toBeGreaterThan(0);
    expect(catalog.fixture.sha256).toBe(sha256(readFileSync(fixturePath)));
    expect(catalog.excluded).toEqual([]);
  });

  it('records every contract field for every entry', () => {
    for (const entry of catalog.entries) {
      expect(entry.id).toMatch(/^[a-z0-9-]+$/);
      expect(entry.displayName.length).toBeGreaterThan(0);
      expect(entry.cssFamily).toBe(entry.displayName);
      expect(entry.spdx).toBe('OFL-1.1');
      expect(entry.copyright).toMatch(/Copyright/i);
      expect(entry.source.commit).toMatch(/^[0-9a-f]{40}$/);
      expect(entry.source.inputs.length).toBeGreaterThan(0);
      expect(entry.source.license.sha256).toMatch(/^[0-9a-f]{64}$/);
      expect(entry.licenseFile.runtimePath)
        .toBe(`font-licenses/${entry.id}/`
          + entry.source.license.path.split('/').at(-1));
      expect(entry.assets.length).toBeGreaterThan(0);
      for (const asset of entry.assets) {
        expect(asset.path).toMatch(/^[a-z0-9-]+(-var|-400|-700)\.woff2$/);
        expect(asset.sha256).toMatch(/^[0-9a-f]{64}$/);
        expect(asset.bytes).toBeGreaterThan(0);
        expect(asset.internalNames.family.length).toBeGreaterThan(0);
        expect(asset.internalNames.postScriptName.length)
          .toBeGreaterThan(0);
        expect(asset.coverage.codepoints).toBeGreaterThan(0);
      }
      expect(typeof entry.fixtureCoverage.complete).toBe('boolean');
      expect(entry.v1Family.length).toBeGreaterThan(0);
    }
  });
});

describe('official provenance', () => {
  it('matches the 26 design rows exactly, in order', () => {
    expect(catalog.entries.map((e) => e.id))
      .toEqual(designRows.map((row) => row.id));
    for (const [index, row] of designRows.entries()) {
      const entry = catalog.entries[index]!;
      expect(entry.displayName).toBe(row.displayName);
      expect(entry.category).toBe(row.category);
      expect(entry.policy).toBe(row.policy);
      expect(entry.reservedFontNames).toEqual(row.rfns);
      expect(entry.v1Family).toBe(row.v1Family);
      expect(entry.source.repository).toBe(row.repository);
      expect(entry.source.commit).toBe(row.commits[0]);
      expect(entry.source.binaryCommit).toBe(row.commits[1] ?? null);
      expect(entry.source.inputs).toEqual(row.inputs);
      expect(entry.source.license.path).toBe(row.licensePath);
      expect(entry.source.license.sha256).toBe(row.licenseSha256);
    }
  });

  it('uses only the pinned official GitHub projects', () => {
    for (const entry of catalog.entries) {
      expect(entry.source.repository)
        .toMatch(/^https:\/\/github\.com\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/);
      if (entry.source.archive !== null) {
        expect(entry.source.archive.url)
          .toMatch(/^https:\/\/github\.com\//);
      }
    }
  });

  it('records the exact design archives for archive-sourced rows', () => {
    const archiveByRef = new Map(designArchives.map((a) => [a.ref, a]));
    for (const [index, row] of designRows.entries()) {
      const entry = catalog.entries[index]!;
      if (row.archiveRef === null) {
        expect(entry.source.archive).toBeNull();
        continue;
      }
      const archive = archiveByRef.get(row.archiveRef)!;
      expect(entry.source.archive).toEqual({
        ref: archive.ref,
        url: archive.url,
        sha256: archive.sha256,
      });
    }
  });
});

describe('vendored assets', () => {
  it('every manifest asset exists, hashes match, and is WOFF2', () => {
    for (const entry of catalog.entries) {
      for (const asset of entry.assets) {
        const { buf } = loadAsset(asset);
        expect(buf.length, asset.path).toBe(asset.bytes);
        expect(sha256(buf), asset.path).toBe(asset.sha256);
        expect(buf.subarray(0, 4).toString('latin1'), asset.path)
          .toBe('wOF2');
      }
    }
  });

  it('the fonts directory contains no unlisted font files', () => {
    const listed = new Set(
      catalog.entries.flatMap((e) => e.assets.map((a) => a.path)),
    );
    const present = readdirSync(fontsDir)
      .filter((name) => name.endsWith('.woff2'));
    expect(new Set(present)).toEqual(listed);
  });

  it('unmodified-upstream rows keep the exact upstream bytes', () => {
    const unmodified = catalog.entries
      .filter((e) => e.policy === 'unmodified-upstream');
    expect(unmodified.map((e) => e.id))
      .toEqual(['fira-sans', 'source-sans-3']);
    for (const entry of unmodified) {
      for (const asset of entry.assets) {
        const input = entry.source.inputs
          .find((candidate) => candidate.path === asset.input);
        expect(input, `${entry.id} ${asset.input}`).toBeDefined();
        expect(asset.sha256, asset.path).toBe(input!.sha256);
      }
    }
  });

  it('subset rows record final hashes that differ from inputs', () => {
    for (const entry of catalog.entries) {
      if (entry.policy !== 'subset-original-name') continue;
      for (const asset of entry.assets) {
        const inputHashes = entry.source.inputs.map((i) => i.sha256);
        expect(inputHashes).not.toContain(asset.sha256);
      }
    }
  });
});

describe('internal name tables', () => {
  it('manifest names equal the parsed name table of the final bytes',
    () => {
      for (const entry of catalog.entries) {
        for (const asset of entry.assets) {
          const { font } = loadAsset(asset);
          expect(font.familyName, asset.path)
            .toBe(asset.internalNames.family);
          expect(font.subfamilyName, asset.path)
            .toBe(asset.internalNames.subfamily);
          expect(font.fullName, asset.path)
            .toBe(asset.internalNames.fullName);
          expect(font.postscriptName, asset.path)
            .toBe(asset.internalNames.postScriptName);
        }
      }
    });
});

describe('faces and axes', () => {
  it('manifest axes equal the parsed fvar table', () => {
    for (const entry of catalog.entries) {
      for (const asset of entry.assets) {
        const { font } = loadAsset(asset);
        const parsed = Object.entries(font.variationAxes)
          .map(([tag, axis]) => ({
            tag,
            min: axis.min,
            default: axis.default,
            max: axis.max,
          }));
        expect(parsed, asset.path).toEqual(asset.axes);
      }
    }
  });

  it('every entry truthfully provides upright 400 and 700', () => {
    for (const entry of catalog.entries) {
      const variable = entry.assets
        .filter((asset) => asset.role === 'variable');
      if (variable.length === 1 && entry.assets.length === 1) {
        const wght = variable[0]!.axes
          .find((axis) => axis.tag === 'wght');
        expect(wght, entry.id).toBeDefined();
        expect(wght!.min, entry.id).toBeLessThanOrEqual(400);
        expect(wght!.max, entry.id).toBeGreaterThanOrEqual(700);
        expect(variable[0]!.weight).toEqual([wght!.min, wght!.max]);
      } else {
        expect(entry.assets.map((asset) => asset.role).sort(), entry.id)
          .toEqual(['upright-400', 'upright-700']);
        for (const asset of entry.assets) {
          const expected = asset.role === 'upright-400' ? 400 : 700;
          const { font } = loadAsset(asset);
          expect(font['OS/2'].usWeightClass, asset.path).toBe(expected);
          expect(asset.weight).toEqual([expected, expected]);
        }
      }
    }
  });
});

describe('measured coverage', () => {
  it('manifest coverage equals an independent cmap measurement', () => {
    for (const entry of catalog.entries) {
      for (const asset of entry.assets) {
        const { font } = loadAsset(asset);
        const codepoints = measuredCodepoints(font);
        expect(codepoints.length, asset.path)
          .toBe(asset.coverage.codepoints);
        expect(toRanges(codepoints), asset.path)
          .toBe(asset.coverage.ranges);
      }
    }
  });

  it('fixtureCoverage is truthful for every entry', () => {
    expect(fixtureCodepoints.length).toBeGreaterThan(150);
    for (const entry of catalog.entries) {
      const covered = entryCoverageIntersection(entry);
      const missing = fixtureCodepoints
        .filter((cp) => !covered.has(cp))
        .map((cp) =>
          `U+${cp.toString(16).toUpperCase().padStart(4, '0')}`);
      expect(missing, entry.id).toEqual([...entry.fixtureCoverage.missing]);
      expect(entry.fixtureCoverage.complete, entry.id)
        .toBe(missing.length === 0);
    }
  });

  it('the fixture never reaches a platform font', () => {
    // The three deterministic fallbacks must cover the whole fixture
    // alone; every other entry must cover it in union with its
    // fallback.
    for (const fallbackId of ['noto-sans', 'noto-serif', 'space-mono']) {
      const entry = entryById.get(fallbackId)!;
      expect(entry.fixtureCoverage.complete, fallbackId).toBe(true);
    }
    for (const entry of catalog.entries) {
      const own = entryCoverageIntersection(entry);
      const fallback
        = entryCoverageIntersection(entryById.get(entry.fallback.id)!);
      const unreachable = fixtureCodepoints
        .filter((cp) => !own.has(cp) && !fallback.has(cp));
      expect(unreachable, entry.id).toEqual([]);
    }
  });
});

describe('license files', () => {
  it('every runtime license file hashes to the design license', () => {
    for (const [index, row] of designRows.entries()) {
      const entry = catalog.entries[index]!;
      const file = join(webRoot, 'public', entry.licenseFile.runtimePath);
      const buf = readFileSync(file);
      expect(sha256(buf), entry.id).toBe(row.licenseSha256);
      expect(entry.licenseFile.sha256, entry.id).toBe(row.licenseSha256);
    }
  });

  it('license text grants the fee-free rights and matches the RFNs',
    () => {
      for (const entry of catalog.entries) {
        const file = join(
          webRoot, 'public', entry.licenseFile.runtimePath,
        );
        const text = readFileSync(file, 'utf8');
        expect(text, entry.id).toMatch(/SIL OPEN FONT LICENSE Version 1\.1/);
        expect(text, entry.id)
          .toMatch(/Permission is hereby granted, free of charge/);
        const preambleMarkers = [
          text.search(/This Font Software is licensed/i),
          text.search(/SIL OPEN FONT LICENSE Version 1\.1/),
        ].filter((offset) => offset >= 0);
        const preambleEnd = Math.min(...preambleMarkers);
        expect(preambleEnd, entry.id).toBeGreaterThan(0);
        const preamble = text.slice(0, preambleEnd);
        const embeddedNotices = entry.assets
          .map((asset) => loadAsset(asset).font.copyright);
        const declaresRfn = /Reserved Font Name/i.test(preamble)
          || embeddedNotices.some((notice) =>
            /Reserved Font Name/i.test(notice));
        expect(declaresRfn, entry.id)
          .toBe(entry.reservedFontNames.length > 0);
        for (const rfn of entry.reservedFontNames) {
          const embeddedDeclaresRfn = embeddedNotices
            .some((notice) => notice.includes(rfn));
          expect(preamble.includes(rfn) || embeddedDeclaresRfn, entry.id)
            .toBe(true);
        }
        if (entry.policy === 'subset-original-name') {
          expect(entry.reservedFontNames, entry.id).toEqual([]);
        }
      }
    });

  it('the notices index lists every entry', () => {
    const notices = readFileSync(
      join(licensesDir, 'THIRD_PARTY_NOTICES.txt'), 'utf8',
    );
    for (const entry of catalog.entries) {
      expect(notices).toContain(`${entry.id} — ${entry.displayName}`);
      expect(notices).toContain(entry.copyright);
      expect(notices).toContain(entry.licenseFile.runtimePath);
      const rfnLine = entry.reservedFontNames.length > 0
        ? entry.reservedFontNames.join(', ')
        : 'none';
      expect(notices)
        .toContain(`Reserved Font Names: ${rfnLine}`);
    }
    expect(notices).toContain('OFL-1.1');
  });

  it('the license tree has no unlisted files', () => {
    const listed = new Set(
      catalog.entries.map((e) => e.licenseFile.runtimePath),
    );
    const walk = (dir: string, prefix: string): string[] =>
      readdirSync(dir, { withFileTypes: true }).flatMap((item) =>
        item.isDirectory()
          ? walk(join(dir, item.name), `${prefix}${item.name}/`)
          : [`${prefix}${item.name}`]);
    const present = walk(licensesDir, 'font-licenses/')
      .filter((name) => name !== 'font-licenses/THIRD_PARTY_NOTICES.txt');
    expect(new Set(present)).toEqual(listed);
  });
});

describe('generated CSS', () => {
  const cssPath = join(webRoot, 'app', 'assets', 'css', 'fonts.css');
  const cssText = readFileSync(cssPath, 'utf8');

  it('contains no remote source', () => {
    expect(cssText).not.toMatch(/https?:/);
    expect(cssText).not.toMatch(/url\(\s*['"]?\/\//);
    expect(cssText).not.toMatch(/@import/);
    expect(cssText).not.toMatch(/local\(/);
  });

  it('declares exactly the manifest faces, locally, upright only', () => {
    const blocks = cssText.match(/@font-face\s*\{[^}]*\}/g) ?? [];
    const parsed = blocks.map((block) => {
      const family = /font-family:\s*'([^']+)'/.exec(block)?.[1];
      const style = /font-style:\s*([a-z]+)/.exec(block)?.[1];
      const weight = /font-weight:\s*([0-9 ]+);/.exec(block)?.[1];
      const display = /font-display:\s*([a-z]+)/.exec(block)?.[1];
      const src = /src:\s*url\('([^']+)'\)\s*format\('woff2'\)/
        .exec(block)?.[1];
      return { family, style, weight, display, src };
    });
    const expected = catalog.entries.flatMap((entry) =>
      entry.assets.map((asset) => ({
        family: entry.cssFamily,
        style: 'normal',
        weight: asset.weight[0] === asset.weight[1]
          ? String(asset.weight[0])
          : `${asset.weight[0]} ${asset.weight[1]}`,
        display: 'swap',
        src: `../fonts/${asset.path}`,
      })));
    expect(parsed).toEqual(expected);
  });

  it('is registered in nuxt.config.ts', () => {
    const nuxtConfig = readFileSync(
      join(webRoot, 'nuxt.config.ts'), 'utf8',
    );
    expect(nuxtConfig).toContain('~/assets/css/fonts.css');
  });
});

describe('resolveFontSelection', () => {
  it('maps every catalog ID to its family and category fallback', () => {
    for (const entry of catalog.entries) {
      const selection = resolveFontSelection(entry.id);
      expect(selection.id).toBe(entry.id);
      expect(selection.cssFamily).toBe(entry.cssFamily);
      expect(selection.fallbackId)
        .toBe(FALLBACK_BY_CATEGORY[entry.category]);
      expect(selection.fallbackId).toBe(entry.fallback.id);
      expect(selection.fallbackCssFamily).toBe(entry.fallback.cssFamily);
    }
  });

  it('builds a quoted two-family stack with no generic family', () => {
    const selection = resolveFontSelection('inter');
    expect(selection.cssStack).toBe('"Inter", "Noto Sans"');
    for (const entry of catalog.entries) {
      const stack = resolveFontSelection(entry.id).cssStack;
      expect(stack).not.toMatch(
        /\b(sans-serif|serif|monospace|system-ui|ui-sans-serif)\b/,
      );
      for (const family of stack.split(', ')) {
        expect(family).toMatch(/^"[^"]+"$/);
      }
    }
  });

  it('de-duplicates the stack for the fallback families', () => {
    for (const id of ['noto-sans', 'noto-serif', 'space-mono']) {
      const selection = resolveFontSelection(id);
      expect(selection.cssStack).toBe(`"${selection.cssFamily}"`);
    }
  });

  it('orders load descriptors upright 400 then 700 at 1em', () => {
    const selection = resolveFontSelection('alegreya');
    expect(selection.loadDescriptors).toEqual([
      `400 1em ${selection.cssStack}`,
      `700 1em ${selection.cssStack}`,
    ]);
  });

  it('rejects an unknown ID', () => {
    expect(() => resolveFontSelection('comic-sans')).toThrow(/unknown/);
    expect(() => resolveFontSelection('')).toThrow(/unknown/);
  });
});

describe('fontsReady', () => {
  interface FakeFontFaceSet {
    readonly events: string[];
    load(descriptor: string): Promise<unknown[]>;
    readonly ready: Promise<unknown>;
  }

  const makeFakeSet = (emptyFor: readonly string[] = []):
  FakeFontFaceSet => {
    const events: string[] = [];
    return {
      events,
      load(descriptor: string): Promise<unknown[]> {
        events.push(`load:${descriptor}`);
        return Promise.resolve(
          emptyFor.some((piece) => descriptor.includes(piece))
            ? []
            : [{ status: 'loaded' }],
        );
      },
      get ready(): Promise<unknown> {
        events.push('ready');
        return Promise.resolve(undefined);
      },
    };
  };

  it('loads 400 then 700 for the stack, then awaits ready', async () => {
    const fake = makeFakeSet();
    await fontsReady('inter', fake as unknown as FontFaceSet);
    expect(fake.events).toEqual([
      'load:400 1em "Inter", "Noto Sans"',
      'load:700 1em "Inter", "Noto Sans"',
      'ready',
    ]);
  });

  it('rejects when a load resolves empty, before ready', async () => {
    const fake = makeFakeSet(['700 1em']);
    await expect(
      fontsReady('inter', fake as unknown as FontFaceSet),
    ).rejects.toThrow(/nothing loaded/);
    expect(fake.events).toEqual([
      'load:400 1em "Inter", "Noto Sans"',
      'load:700 1em "Inter", "Noto Sans"',
    ]);
  });

  it('rejects an unknown ID without touching the set', async () => {
    const fake = makeFakeSet();
    await expect(
      fontsReady('unknown-font', fake as unknown as FontFaceSet),
    ).rejects.toThrow(/unknown/);
    expect(fake.events).toEqual([]);
  });
});

const outputLicenses
  = join(webRoot, '.output', 'public', 'font-licenses');

describe.runIf(existsSync(outputLicenses))('built output', () => {
  it('retains every license byte-for-byte after nuxt build', () => {
    for (const entry of catalog.entries) {
      const source = readFileSync(
        join(webRoot, 'public', entry.licenseFile.runtimePath),
      );
      const built = readFileSync(join(
        webRoot, '.output', 'public', entry.licenseFile.runtimePath,
      ));
      expect(built.equals(source), entry.id).toBe(true);
    }
    const sourceNotices
      = readFileSync(join(licensesDir, 'THIRD_PARTY_NOTICES.txt'));
    const builtNotices
      = readFileSync(join(outputLicenses, 'THIRD_PARTY_NOTICES.txt'));
    expect(builtNotices.equals(sourceNotices)).toBe(true);
  });
});
