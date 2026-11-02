import { createHash } from 'node:crypto';
import {
  constants,
  closeSync,
  existsSync,
  fstatSync,
  lstatSync,
  openSync,
  readFileSync,
  readdirSync,
  renameSync,
  unlinkSync,
  writeFileSync,
} from 'node:fs';
import { dirname, join, relative, resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';
import { inflateSync } from 'node:zlib';
import { SAMPLES } from '../../../packages/schema/gen/ts/samples.ts';

export const REPOSITORY = resolve(import.meta.dirname, '../../..');
export const PAGES_PATH = 'apps/web/public/templates/pages';
export const RASTER_DPI = 150;
export const PDF_FLAGS = {
  displayHeaderFooter: false,
  margin: { bottom: 0, left: 0, right: 0, top: 0 },
  preferCSSPageSize: true,
  printBackground: true,
  scale: 1,
} as const;
export const BROWSER_FLAGS = [
  '--force-color-profile=srgb',
  '--font-render-hinting=none',
  '--disable-lcd-text',
  '--disable-gpu',
  '--hide-scrollbars',
];

const SOURCE_ROOTS = [
  'apps/web/scripts/generate-template-pages.mts',
  'apps/web/scripts/validate-template-pages.mts',
  'apps/web/app/components/resume',
  'apps/web/app/utils/fontCatalog.ts',
  'apps/web/app/utils/fontsReady.ts',
  'apps/web/app/utils/sanitizeRichText.ts',
  'apps/web/app/assets/css',
  'apps/web/app/assets/fonts',
  'apps/web/app/pages/_harness',
  'apps/web/app/templates/samplePages.ts',
  'apps/web/nuxt.config.ts',
  'apps/web/package.json',
  'apps/web/package-lock.json',
  'apps/web/tsconfig.json',
  'packages/schema/gen/ts',
  'packages/schema/resume.schema.json',
  'packages/schema/package.json',
  'packages/schema/samples',
  'packages/schema/validation/store.ts',
  'packages/schema/fixtures/full.json',
  'packages/schema/fixtures/vn-full.json',
];
const SHA256 = /^[0-9a-f]{64}$/u;
const MAX_FILE_BYTES = 16 * 1024 * 1024;
const MAX_PNG_BYTES = 8 * 1024 * 1024;
const SIGNATURE = Buffer.from('89504e470d0a1a0a', 'hex');

export interface ManifestEntry {
  templateId: string;
  lng: 'en' | 'vi';
  pages: number;
  width: number;
  height: number;
  files: string[];
  sourceSha256?: string;
  outputSha256?: string[];
}

export function sha256(bytes: Buffer | string): string {
  return createHash('sha256').update(bytes).digest('hex');
}

function requireCondition(value: unknown, message: string): asserts value {
  if (!value) throw new Error(message);
}

function assertParents(path: string): void {
  const parent = dirname(path);
  if (parent !== path) assertParents(parent);
  const info = lstatSync(path);
  requireCondition(!info.isSymbolicLink(), 'symlink is forbidden');
}

export function readRegular(path: string, max = MAX_FILE_BYTES): Buffer {
  assertParents(dirname(path));
  requireCondition(lstatSync(path).isFile(), 'expected a regular file');
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW
    | constants.O_NONBLOCK);
  try {
    const info = fstatSync(fd);
    requireCondition(info.isFile() && info.size <= max, 'invalid file size');
    return readFileSync(fd);
  } finally {
    closeSync(fd);
  }
}

export function sourcePaths(root: string): string[] {
  const paths: string[] = [];
  const walk = (path: string): void => {
    const base = path.split('/').at(-1)!;
    if (base === 'AGENTS.md' || base === 'CLAUDE.md') return;
    if (path === 'apps/web/app/assets/fonts/tools'
      || path === 'apps/web/app/assets/fonts/README.md') return;
    requireCondition(
      !base.startsWith('.')
      && !/^(credentials.*|secrets.*|id_rsa.*|id_ed25519.*|.*\.(key|pem))$/iu
        .test(base)
        && !['node_modules', 'dist', 'coverage', 'test-results',
          'playwright-report'].includes(base),
      'secret-like source is forbidden',
    );
    const controls = [...path].some((character) =>
      character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127);
    requireCondition(!controls, 'invalid source path');
    const full = join(root, path);
    assertParents(full);
    const info = lstatSync(full);
    if (info.isDirectory()) {
      const children = readdirSync(full).sort();
      requireCondition(children.length > 0, 'empty source directory');
      for (const child of children) walk(`${path}/${child}`);
    } else {
      requireCondition(info.isFile(), 'source must be a regular file');
      paths.push(path);
    }
  };
  for (const path of SOURCE_ROOTS) walk(path);
  return paths.sort();
}

export function browserImage(root: string): string {
  const makefile = join(root, 'Makefile');
  const fromMake = existsSync(makefile)
    ? /^WEB_E2E_IMAGE := (.+)$/mu.exec(
      readRegular(makefile).toString('utf8'),
    )?.[1]
    : undefined;
  const image = fromMake ?? process.env.TEMPLATE_PAGES_BROWSER_IMAGE;
  requireCondition(
    typeof image === 'string'
    && /^mcr\.microsoft\.com\/playwright:v[\d.]+-noble@sha256:[a-f0-9]{64}$/u
      .test(image),
    'pinned browser image is required',
  );
  if (fromMake && process.env.TEMPLATE_PAGES_BROWSER_IMAGE) {
    requireCondition(
      image === process.env.TEMPLATE_PAGES_BROWSER_IMAGE,
      'browser image differs from Makefile',
    );
  }
  return image;
}

export function sourceFingerprint(root: string): {
  sha256: string;
  browserDigest: string;
} {
  const digest = browserImage(root).split('@')[1]!;
  const hash = createHash('sha256');
  const inputs: [string, Buffer][] = [
    ['@browser-image-digest', Buffer.from(digest)],
    ['@browser-flags', Buffer.from(JSON.stringify(BROWSER_FLAGS))],
    ['@pdf-flags', Buffer.from(JSON.stringify(PDF_FLAGS))],
    ['@raster-dpi', Buffer.from(String(RASTER_DPI))],
  ];
  let total = 0;
  for (const path of sourcePaths(root)) {
    const bytes = readRegular(join(root, path));
    total += bytes.length;
    requireCondition(total <= 128 * 1024 * 1024, 'source size limit');
    inputs.push([path, bytes]);
  }
  inputs.sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0);
  for (const [path, bytes] of inputs) {
    hash.update(`${path}\0${bytes.length}\0`);
    hash.update(bytes);
  }
  return { sha256: hash.digest('hex'), browserDigest: digest };
}

const crcTable = Array.from({ length: 256 }, (_, byte) => {
  let crc = byte;
  for (let bit = 0; bit < 8; bit += 1) {
    crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0);
  }
  return crc >>> 0;
});

function crc32(bytes: Buffer): number {
  let crc = 0xffffffff;
  for (const byte of bytes) crc = crcTable[(crc ^ byte) & 255]! ^ (crc >>> 8);
  return (crc ^ 0xffffffff) >>> 0;
}

/** Decode bounded RGB/RGBA data; reject metadata and trailing payloads. */
export function pngDimensions(bytes: Buffer): {
  width: number;
  height: number;
} {
  const valid = (value: unknown): void =>
    requireCondition(value, 'invalid PNG');
  valid(bytes.length <= MAX_PNG_BYTES
    && bytes.subarray(0, 8).equals(SIGNATURE));
  let offset = 8;
  let width = 0;
  let height = 0;
  let channels = 0;
  let ended = false;
  const data: Buffer[] = [];
  while (offset + 12 <= bytes.length) {
    const length = bytes.readUInt32BE(offset);
    const end = offset + 12 + length;
    valid(end <= bytes.length);
    const kind = bytes.toString('latin1', offset + 4, offset + 8);
    const payload = bytes.subarray(offset + 8, end - 4);
    valid(crc32(bytes.subarray(offset + 4, end - 4))
      === bytes.readUInt32BE(end - 4));
    if (offset === 8) {
      valid(kind === 'IHDR' && length === 13);
      width = payload.readUInt32BE(0);
      height = payload.readUInt32BE(4);
      valid(width > 0 && height > 0 && width * height <= 4_000_000
        && width <= 2048 && height <= 2048);
      valid(payload[8] === 8 && (payload[9] === 2 || payload[9] === 6)
        && payload[10] === 0 && payload[11] === 0 && payload[12] === 0);
      channels = payload[9] === 2 ? 3 : 4;
    } else if (kind === 'IDAT') {
      data.push(payload);
    } else if (kind === 'IEND') {
      valid(length === 0 && data.length > 0 && end === bytes.length);
      ended = true;
      break;
    } else {
      valid(data.length === 0 && (
        (kind === 'sRGB' && length === 1 && payload[0]! <= 3)
        || (kind === 'sBIT' && length === channels
          && payload.every((byte) => byte === 8))
        || (kind === 'pHYs' && length === 9 && payload[8]! <= 1)
      ));
    }
    offset = end;
  }
  valid(ended);
  const stride = width * channels + 1;
  let decoded: Buffer;
  try {
    const compressed = Buffer.concat(data);
    const result = inflateSync(compressed, {
      maxOutputLength: stride * height,
      info: true,
    }) as unknown as { buffer: Buffer; engine: { bytesWritten: number } };
    valid(result.engine.bytesWritten === compressed.length);
    decoded = result.buffer;
  } catch {
    throw new Error('invalid PNG data');
  }
  valid(decoded.length === stride * height);
  for (let row = 0; row < height; row += 1) {
    valid(decoded[row * stride]! <= 4);
  }
  return { width, height };
}

function readManifest(directory: string): ManifestEntry[] {
  const entries: ManifestEntry[] = JSON.parse(
    readRegular(join(directory, 'manifest.json'), 128 * 1024).toString('utf8'),
  );
  requireCondition(Array.isArray(entries) && entries.length === 26
    && SAMPLES.length === 26, 'invalid manifest entry count');
  for (const [index, entry] of entries.entries()) {
    const sample = SAMPLES[index]!;
    requireCondition(entry !== null && typeof entry === 'object'
      && entry.templateId === sample.templateId && entry.lng === sample.lng
      && entry.pages === 1 && Array.isArray(entry.files)
      && entry.files.length === 1
      && entry.files[0] === `${sample.templateId}-${sample.lng}-p1.png`
      && Number.isInteger(entry.width) && entry.width > 0
      && Number.isInteger(entry.height) && entry.height > 0,
    'invalid manifest page order or dimensions');
  }
  return entries;
}

function outputHashes(directory: string, entry: ManifestEntry): string[] {
  return entry.files.map((file) => {
    const bytes = readRegular(join(directory, file), MAX_PNG_BYTES);
    const { width, height } = pngDimensions(bytes);
    requireCondition(width === entry.width && height === entry.height,
      'PNG dimensions differ from manifest');
    return sha256(bytes);
  });
}

function checkFileSet(directory: string, entries: ManifestEntry[]): void {
  assertParents(directory);
  const expected = entries.flatMap((entry) => entry.files);
  expected.push('manifest.json');
  if (existsSync(join(directory, 'source-metadata.txt'))) {
    expected.push('source-metadata.txt');
  }
  requireCondition(JSON.stringify(readdirSync(directory).sort())
    === JSON.stringify(expected.sort()), 'invalid output file set');
}

export function validatePages(
  root = REPOSITORY,
  directory = join(root, PAGES_PATH),
): ManifestEntry[] {
  const entries = readManifest(directory);
  checkFileSet(directory, entries);
  const source = sourceFingerprint(root);
  if (existsSync(join(directory, 'source-metadata.txt'))) {
    const metadata = readRegular(
      join(directory, 'source-metadata.txt'), 4096,
    ).toString('utf8');
    const pattern = new RegExp(
      '^commit=[a-f0-9]{40}\\nmanifest_sha256=[a-f0-9]{64}'
      + '\\ntar_sha256=[a-f0-9]{64}'
      + `\\nsource_sha256=${source.sha256}`
      + `\\nbrowser_image_digest=${source.browserDigest}`
      + `\\npdf_flags_sha256=${sha256(JSON.stringify(PDF_FLAGS))}`
      + `\\nraster_dpi=${RASTER_DPI}\\n$`,
      'u',
    );
    requireCondition(pattern.exec(metadata)?.[0] === metadata,
      'invalid source metadata');
  }
  for (const entry of entries) {
    requireCondition(entry.sourceSha256 !== undefined
      && SHA256.test(entry.sourceSha256), 'missing source hash');
    requireCondition(entry.sourceSha256 === source.sha256, 'stale source hash');
    const hashes = outputHashes(directory, entry);
    requireCondition(Array.isArray(entry.outputSha256)
      && JSON.stringify(hashes) === JSON.stringify(entry.outputSha256),
    'invalid output hash');
  }
  return entries;
}

/** Bootstrap existing assets without launching a browser or writing PNGs. */
export function refreshHashes(root = REPOSITORY): void {
  const directory = join(root, PAGES_PATH);
  const entries = readManifest(directory);
  checkFileSet(directory, entries);
  const source = sourceFingerprint(root).sha256;
  const updated = entries.map((entry) => ({
    ...entry,
    sourceSha256: source,
    outputSha256: outputHashes(directory, entry),
  }));
  const temporary = join(directory, 'manifest.json.tmp');
  try {
    writeFileSync(temporary, `${JSON.stringify(updated, null, 2)}\n`, {
      flag: 'wx',
      mode: 0o644,
    });
  } catch {
    throw new Error('could not create manifest candidate');
  }
  try {
    renameSync(temporary, join(directory, 'manifest.json'));
  } finally {
    if (existsSync(temporary)) unlinkSync(temporary);
  }
}

export function assertCandidatePath(directory: string, root: string): void {
  const path = relative(root, directory);
  requireCondition(path.startsWith(`..${sep}`) || path.startsWith(`.dev${sep}`),
    'candidate directory must be outside sources or below .dev');
  assertParents(dirname(directory));
  requireCondition(!existsSync(directory),
    'candidate directory already exists');
}

if (process.argv[1]
  && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  try {
    if (process.argv.length !== 3) throw new Error('invalid arguments');
    if (process.argv[2] === '--test') validatePages();
    else if (process.argv[2] === '--refresh-hashes') refreshHashes();
    else throw new Error('invalid arguments');
    console.log('template-pages: manifest validated');
  } catch {
    console.error('template-pages: failed; check inputs and manifest hashes');
    process.exitCode = 1;
  }
}
