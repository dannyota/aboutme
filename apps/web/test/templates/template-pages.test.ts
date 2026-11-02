// @vitest-environment node
import { createCanvas } from '@napi-rs/canvas';
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { deflateSync } from 'node:zlib';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { generatePages } from '../../scripts/generate-template-pages.mts';
import {
  pngDimensions,
  refreshHashes,
  sha256,
  sourceFingerprint,
  sourcePaths,
  validatePages,
} from '../../scripts/validate-template-pages.mts';

const repository = resolve(import.meta.dirname, '../../../..');
const committed = join(repository, 'apps/web/public/templates/pages');
let root: string;
let pages: string;

function put(path: string, bytes: string | Buffer): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, bytes);
}

function manifest(): Record<string, unknown>[] {
  return JSON.parse(readFileSync(join(pages, 'manifest.json'), 'utf8'));
}

function save(entries: Record<string, unknown>[]): void {
  writeFileSync(
    join(pages, 'manifest.json'),
    `${JSON.stringify(entries, null, 2)}\n`,
  );
}

function chunk(kind: string, data: Buffer): Buffer {
  const bytes = Buffer.concat([Buffer.from(kind), data]);
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit += 1) {
      crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0);
    }
  }
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const checksum = Buffer.alloc(4);
  checksum.writeUInt32BE((crc ^ 0xffffffff) >>> 0);
  return Buffer.concat([length, bytes, checksum]);
}

beforeEach(() => {
  root = realpathSync(mkdtempSync(join(tmpdir(), 'template-pages-')));
  pages = join(root, 'apps/web/public/templates/pages');
  for (const path of sourcePaths(repository)) put(join(root, path), 'input');
  put(join(root, 'Makefile'), readFileSync(join(repository, 'Makefile')));
  put(join(pages, 'manifest.json'), readFileSync(
    join(committed, 'manifest.json'),
  ));
  const png = createCanvas(1240, 1754).toBuffer('image/png');
  for (const entry of manifest()) {
    for (const file of entry.files as string[]) put(join(pages, file), png);
  }
});

afterEach(() => rmSync(root, { recursive: true, force: true }));

describe('template page freshness', () => {
  it('rejects generation into the committed product directory', async () => {
    await expect(generatePages(committed))
      .rejects.toThrow(/candidate directory/);
  });

  it('refreshes only hashes and preserves every PNG and existing field', () => {
    const original = manifest();
    const images = new Map(original.flatMap((entry) =>
      (entry.files as string[]).map((file) =>
        [file, readFileSync(join(pages, file))] as const)));
    refreshHashes(root);
    validatePages(root);
    for (const [file, bytes] of images) {
      expect(readFileSync(join(pages, file))).toEqual(bytes);
    }
    expect(manifest().map(({ sourceSha256, outputSha256, ...entry }) => {
      expect(sourceSha256).toMatch(/^[0-9a-f]{64}$/);
      expect(outputSha256).toEqual([expect.stringMatching(/^[0-9a-f]{64}$/)]);
      return entry;
    })).toEqual(original.map(({ sourceSha256, outputSha256, ...entry }) => {
      void sourceSha256;
      void outputSha256;
      return entry;
    }));
  });

  it('rejects missing hashes without writing or rendering', () => {
    save(manifest().map(({ sourceSha256, outputSha256, ...entry }) => {
      void sourceSha256;
      void outputSha256;
      return entry;
    }));
    const before = readFileSync(join(pages, 'manifest.json'));
    expect(() => validatePages(root)).toThrow(/source hash/);
    expect(readFileSync(join(pages, 'manifest.json'))).toEqual(before);
  });

  it.each([
    'apps/web/scripts/generate-template-pages.mts',
    'apps/web/scripts/validate-template-pages.mts',
    'apps/web/app/components/resume/ResumeDocument.vue',
    'apps/web/app/utils/fontsReady.ts',
    'apps/web/app/assets/fonts/inter-var.woff2',
    'apps/web/app/assets/css/fonts.css',
    'apps/web/app/pages/_harness/sample-fixtures.ts',
    'packages/schema/resume.schema.json',
    'packages/schema/gen/ts/templates.ts',
    'packages/schema/samples/ats-plain.en.json',
    'apps/web/nuxt.config.ts',
    'apps/web/package-lock.json',
  ])('rejects changed source %s', (path) => {
    refreshHashes(root);
    put(join(root, path), 'changed');
    expect(() => validatePages(root)).toThrow(/stale source hash/);
  });

  it('detects new renderer inputs and browser drift', () => {
    refreshHashes(root);
    const before = sourceFingerprint(root).sha256;
    put(join(root, 'apps/web/app/components/resume/new.css'), 'input');
    expect(sourceFingerprint(root).sha256).not.toBe(before);
    expect(() => validatePages(root)).toThrow(/stale source hash/);
    rmSync(join(root, 'apps/web/app/components/resume/new.css'));
    const make = readFileSync(join(root, 'Makefile'), 'utf8');
    put(join(root, 'Makefile'), make.replace(
      /(@sha256:)[0-9a-f]{64}/,
      `$1${'a'.repeat(64)}`,
    ));
    expect(() => validatePages(root)).toThrow(/stale source hash/);
  });

  it('excludes outputs and font tooling from sources', () => {
    const before = sourceFingerprint(root).sha256;
    put(join(pages, 'ats-plain-en-p1.png'), 'changed image');
    put(join(pages, 'manifest.json'), 'changed manifest');
    put(join(root, 'apps/web/app/assets/fonts/tools/local.py'), 'tool');
    expect(sourceFingerprint(root).sha256).toBe(before);
  });

  it('rejects a changed output hash and wrong dimensions', () => {
    refreshHashes(root);
    const file = join(pages, 'ats-plain-en-p1.png');
    const other = createCanvas(1240, 1754);
    other.getContext('2d').fillRect(0, 0, 10, 10);
    put(file, other.toBuffer('image/png'));
    expect(() => validatePages(root)).toThrow(/output hash/);
    put(file, createCanvas(1, 1).toBuffer('image/png'));
    const before = readFileSync(join(pages, 'manifest.json'));
    expect(() => refreshHashes(root)).toThrow(/dimensions/);
    expect(readFileSync(join(pages, 'manifest.json'))).toEqual(before);
  });

  it.each(['order', 'traversal', 'extra', 'duplicate', 'pages'])(
    'rejects invalid manifest %s',
    (kind) => {
      const entries = manifest();
      if (kind === 'order') entries.reverse();
      if (kind === 'traversal') entries[0]!.files = ['../outside.png'];
      if (kind === 'extra') entries.push({ ...entries[0] });
      if (kind === 'duplicate') entries[1] = { ...entries[0] };
      if (kind === 'pages') entries[0]!.pages = 2;
      save(entries);
      expect(() => refreshHashes(root)).toThrow(/manifest/);
    },
  );

  it('rejects symlinks, unknown files, PNG trailers and broken data', () => {
    refreshHashes(root);
    const file = join(pages, 'ats-plain-en-p1.png');
    const png = readFileSync(file);
    rmSync(file);
    symlinkSync(join(pages, 'ats-plain-vi-p1.png'), file);
    expect(() => validatePages(root)).toThrow(/regular file/);
    rmSync(file);
    put(file, Buffer.concat([png, Buffer.from('private trailer')]));
    expect(() => refreshHashes(root)).toThrow(/PNG/);
    put(file, png.subarray(0, png.length - 12));
    expect(() => refreshHashes(root)).toThrow(/PNG/);
    put(file, png);
    put(join(pages, 'unexpected.txt'), 'private text');
    expect(() => validatePages(root)).toThrow(/file set/);
  });

  it('rejects symlinked source directories', () => {
    const renderer = join(root, 'apps/web/app/components/resume');
    rmSync(renderer, { recursive: true });
    symlinkSync(dirname(renderer), renderer);
    expect(() => sourceFingerprint(root)).toThrow(/symlink/);
  });

  it('rejects local state before reading it', () => {
    put(join(root, 'apps/web/app/components/resume/.env.test'), 'sentinel');
    expect(() => sourceFingerprint(root)).toThrow(/secret-like source/);
  });

  it('rejects malformed, oversized or metadata-bearing PNGs', () => {
    const png = createCanvas(1, 1).toBuffer('image/png');
    const signature = png.subarray(0, 8);
    const header = png.subarray(8, 33);
    const end = chunk('IEND', Buffer.alloc(0));
    const pack = (pixels: Buffer): Buffer => Buffer.concat([
      signature, header, chunk('IDAT', deflateSync(pixels)), end,
    ]);
    expect(pngDimensions(pack(Buffer.from([0, 0, 0, 0, 0]))))
      .toEqual({ width: 1, height: 1 });
    for (const pixels of [Buffer.alloc(1000), Buffer.from([5, 0, 0, 0, 0])]) {
      expect(() => pngDimensions(pack(pixels))).toThrow(/PNG/);
    }
    const metadata = Buffer.concat([
      signature, header, chunk('tEXt', Buffer.from('Comment\0private')),
      chunk('IDAT', deflateSync(Buffer.alloc(5))), end,
    ]);
    expect(() => pngDimensions(metadata)).toThrow(/PNG/);
    const compressedTrailer = Buffer.concat([
      signature, header,
      chunk('IDAT', Buffer.concat([
        deflateSync(Buffer.alloc(5)), Buffer.from('private trailer'),
      ])), end,
    ]);
    expect(() => pngDimensions(compressedTrailer)).toThrow(/PNG/);
    const corrupt = Buffer.from(png);
    corrupt[corrupt.length - 1]! ^= 1;
    expect(() => pngDimensions(corrupt)).toThrow(/PNG/);
    const large = Buffer.from(png.subarray(16, 29));
    large.writeUInt32BE(100_000, 0);
    expect(() => pngDimensions(Buffer.concat([
      signature, chunk('IHDR', large), png.subarray(33),
    ]))).toThrow(/PNG/);
  });

  it('accepts only fixed source metadata fields', () => {
    refreshHashes(root);
    const source = sourceFingerprint(root);
    const flags = {
      displayHeaderFooter: false,
      margin: { bottom: 0, left: 0, right: 0, top: 0 },
      preferCSSPageSize: true,
      printBackground: true,
      scale: 1,
    };
    const metadata = `commit=${'a'.repeat(40)}\n`
      + `manifest_sha256=${'b'.repeat(64)}\n`
      + `tar_sha256=${'c'.repeat(64)}\n`
      + `source_sha256=${source.sha256}\n`
      + `browser_image_digest=${source.browserDigest}\n`
      + `pdf_flags_sha256=${sha256(JSON.stringify(flags))}\n`
      + 'raster_dpi=150\n';
    put(join(pages, 'source-metadata.txt'), metadata);
    validatePages(root);
    put(join(pages, 'source-metadata.txt'), `${metadata}private text\n`);
    expect(() => validatePages(root)).toThrow(/source metadata/);
  });
});
