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
import { isBuiltin } from 'node:module';
import { dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';
import { inflateSync } from 'node:zlib';
import ts from 'typescript';
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

const SOURCE_INPUTS = [
  'apps/web/scripts/generate-template-pages.mts',
  'apps/web/scripts/validate-template-pages.mts',
  'apps/web/app/assets/fonts',
  'apps/web/app/templates/samplePages.ts',
  'apps/web/nuxt.config.ts',
  'apps/web/package.json',
  'apps/web/package-lock.json',
  'apps/web/tsconfig.json',
  'apps/web/public/csp-bootstrap.js',
  'apps/web/public/theme-bootstrap.js',
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

const IMPORT_ERROR = 'unresolvable source import';
const MODULE_EXTENSIONS = ['.ts', '.tsx', '.mts', '.cts', '.js', '.jsx',
  '.mjs', '.cjs', '.vue', '.json', '.css'];

function sourcePath(root: string, path: string): string {
  const result = relative(root, resolve(root, path));
  requireCondition(result !== '' && !isAbsolute(result)
    && result !== '..' && !result.startsWith(`..${sep}`), IMPORT_ERROR);
  const normalized = result.split(sep).join('/');
  const controls = [...normalized].some((character) =>
    character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127);
  requireCondition(!controls, 'invalid source path');
  for (const part of normalized.split('/')) {
    requireCondition(!part.startsWith('.')
      && !/^(credentials.*|secrets.*|id_rsa.*|id_ed25519.*|.*\.(key|pem))$/iu
        .test(part)
        && !['node_modules', 'dist', 'coverage', 'test-results',
          'playwright-report'].includes(part),
    'secret-like source is forbidden');
  }
  return normalized;
}

function resolveSource(root: string, path: string): string {
  const base = sourcePath(root, path);
  let parent = dirname(join(root, base));
  while (!existsSync(parent)) parent = dirname(parent);
  assertParents(parent);
  const candidates = [base, ...MODULE_EXTENSIONS.map((ext) => base + ext),
    ...MODULE_EXTENSIONS.map((ext) => `${base}/index${ext}`)];
  const found = candidates.find((candidate) =>
    existsSync(join(root, candidate))
    && lstatSync(join(root, candidate)).isFile());
  requireCondition(found !== undefined, IMPORT_ERROR);
  return found;
}

function scriptTrees(path: string, text: string): ts.SourceFile[] {
  const scripts = path.endsWith('.vue')
    ? [...text.replace(/<!--[\s\S]*?-->/gu, '')
        .matchAll(/<script\b[^>]*>([\s\S]*?)<\/script\s*>/giu)]
        .map((match) => match[1]!)
    : /\.[cm]?[jt]sx?$/u.test(path) ? [text] : [];
  return scripts.map((script) => ts.createSourceFile(
    path, script, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS,
  ));
}

function property(node: ts.Node | undefined, name: string): ts.Expression
  | undefined {
  if (!node || !ts.isObjectLiteralExpression(node)) return undefined;
  const found = node.properties.find((item) =>
    ts.isPropertyAssignment(item) && item.name.getText()
      .replace(/^['"]|['"]$/gu, '') === name);
  return found && ts.isPropertyAssignment(found)
    ? found.initializer
    : undefined;
}

function staticStrings(node: ts.Expression | undefined): string[] {
  if (node === undefined) return [];
  if (ts.isStringLiteralLike(node)) return [node.text];
  requireCondition(ts.isArrayLiteralExpression(node), IMPORT_ERROR);
  return node.elements.flatMap((item) => staticStrings(item));
}

function moduleFiles(root: string, directory: string): string[] {
  if (!existsSync(join(root, directory))) return [];
  assertParents(join(root, directory));
  return readdirSync(join(root, directory)).sort().flatMap((name) => {
    const path = `${directory}/${name}`;
    if (/^[^.].*\.[cm]?[jt]sx?$/u.test(name)) return [sourcePath(root, path)];
    if (!lstatSync(join(root, path)).isDirectory()) return [];
    return MODULE_EXTENSIONS.filter((ext) => /[jt]sx?$/u.test(ext))
      .map((ext) => `${path}/index${ext}`)
      .filter((file) => existsSync(join(root, file)))
      .map((file) => sourcePath(root, file));
  });
}

function autoImportSources(root: string, app: string): Map<string, string[]> {
  const found = new Map<string, string[]>();
  for (const path of ['composables', 'utils'].flatMap((directory) =>
    moduleFiles(root, `${app}/${directory}`))) {
    for (const tree of scriptTrees(path, readRegular(join(root, path))
      .toString('utf8'))) {
      for (const statement of tree.statements) {
        if (!ts.canHaveModifiers(statement) || !ts.getModifiers(statement)
          ?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword)) {
          continue;
        }
        const names = ts.isVariableStatement(statement)
          ? statement.declarationList.declarations.map((item) => item.name)
          : ts.isFunctionDeclaration(statement) ? [statement.name] : [];
        for (const name of names) {
          if (name && ts.isIdentifier(name)) {
            found.set(name.text, [...(found.get(name.text) ?? []), path]);
          }
        }
      }
    }
  }
  return found;
}

function moduleImports(
  path: string, text: string, autoImports: Map<string, string[]>,
): string[] {
  const imports: string[] = [];
  for (const tree of scriptTrees(path, text)) {
    const visit = (node: ts.Node): void => {
      if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node))
        && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
        imports.push(node.moduleSpecifier.text);
      }
      if (ts.isIdentifier(node)) {
        imports.push(...(autoImports.get(node.text) ?? [])
          .map((file) => `./${relative(dirname(path), file)}`));
      }
      ts.forEachChild(node, visit);
    };
    visit(tree);
  }
  const styles = path.endsWith('.css') ? [text] : [];
  if (path.endsWith('.vue')) {
    for (const match of text.replace(/<!--[\s\S]*?-->/gu, '')
      .matchAll(/<(script|style)\b([^>]*)>([\s\S]*?)<\/\1\s*>/giu)) {
      const src = /\bsrc\s*=\s*(['"])(.*?)\1/u.exec(match[2]!);
      if (src) imports.push(src[2]!);
      if (match[1]!.toLowerCase() === 'style') styles.push(match[3]!);
    }
  }
  for (const style of styles) {
    for (const match of style.replace(/\/\*[\s\S]*?\*\//gu, '')
      .matchAll(
        /@import\s+(?:url\(\s*)?(?:"([^"]+)"|'([^']+)'|([^\s;)]+))/giu,
      )) {
      imports.push(match[1] ?? match[2] ?? match[3]!);
    }
  }
  return imports;
}

function nuxtInputs(root: string): {
  entries: string[];
  aliases: Map<string, string | undefined>;
  app: string;
} {
  const config = 'apps/web/nuxt.config.ts';
  const tree = scriptTrees(config, readRegular(join(root, config))
    .toString('utf8'))[0]!;
  const exported = tree.statements.find(ts.isExportAssignment)?.expression;
  requireCondition(exported && ts.isCallExpression(exported), IMPORT_ERROR);
  const options = exported.arguments[0];
  requireCondition(options && ts.isObjectLiteralExpression(options),
    IMPORT_ERROR);
  const app = sourcePath(root, resolve(root, 'apps/web',
    staticStrings(property(options, 'srcDir'))[0] ?? 'app'));
  const aliases = new Map<string, string | undefined>([
    ['@', app], ['~', app], ['@@', 'apps/web'], ['~~', 'apps/web'],
  ]);
  const configured = property(options, 'alias');
  requireCondition(!configured || ts.isObjectLiteralExpression(configured),
    IMPORT_ERROR);
  if (configured && ts.isObjectLiteralExpression(configured)) {
    for (const item of configured.properties) {
      requireCondition(ts.isPropertyAssignment(item), IMPORT_ERROR);
      const name = item.name.getText().replace(/^['"]|['"]$/gu, '');
      requireCondition(ts.isStringLiteralLike(item.initializer)
        || name.startsWith('#'), IMPORT_ERROR);
      // Generated virtual modules use the config and schema source hashes.
      aliases.set(name, ts.isStringLiteralLike(item.initializer)
        ? sourcePath(root, resolve(root, 'apps/web', item.initializer.text))
        : undefined);
    }
  }
  const page = `${app}/pages/_harness/render.vue`;
  const entries = [`${app}/app.vue`, page];
  entries.push(...['plugins', 'middleware'].flatMap((directory) =>
    moduleFiles(root, `${app}/${directory}`).filter((path) =>
      directory !== 'middleware' || /\.global\.[cm]?[jt]s$/u.test(path))));
  entries.push(...['plugins', 'middleware'].flatMap((directory) =>
    moduleFiles(root, `apps/web/server/${directory}`)));
  let meta: ts.Node | undefined;
  for (const script of scriptTrees(page, readRegular(join(root, page))
    .toString('utf8'))) {
    const visit = (node: ts.Node): void => {
      if (ts.isCallExpression(node) && node.expression.getText()
        === 'definePageMeta') meta = node.arguments[0];
      ts.forEachChild(node, visit);
    };
    visit(script);
  }
  entries.push(...staticStrings(property(meta, 'middleware'))
    .map((name) => resolveSource(root, `${app}/middleware/${name}`)));
  const layout = property(meta, 'layout');
  const appText = readRegular(join(root, `${app}/app.vue`)).toString('utf8');
  if (/<NuxtLayout\b/u.test(appText)
    && layout?.kind !== ts.SyntaxKind.FalseKeyword) {
    const name = staticStrings(layout)[0]
      ?? /<NuxtLayout\b[^>]*\bname=['"]([^'"]+)['"]/u.exec(appText)?.[1]
      ?? 'default';
    entries.push(resolveSource(root, `${app}/layouts/${name}.vue`));
  }
  const plugins = property(options, 'plugins');
  requireCondition(!plugins || ts.isArrayLiteralExpression(plugins),
    IMPORT_ERROR);
  const extra = [...staticStrings(property(options, 'css')),
    ...(plugins?.elements.flatMap((item) => staticStrings(
      ts.isObjectLiteralExpression(item) ? property(item, 'src') : item,
    )) ?? [])];
  entries.push(...extra.map((path) => path.startsWith('.')
    ? sourcePath(root, resolve(root, 'apps/web', path))
    : path));
  return { entries, aliases, app };
}

export function sourcePaths(root: string): string[] {
  root = resolve(root);
  const paths = new Set<string>();
  const { entries, aliases, app } = nuxtInputs(root);
  const autoImports = autoImportSources(root, app);
  const dependencies = JSON.parse(readRegular(join(root,
    'apps/web/package.json')).toString('utf8'));
  const packages: Record<string, string> = {
    ...dependencies.dependencies, ...dependencies.devDependencies,
  };
  const imported = (
    importer: string, specifier: string,
  ): string | undefined => {
    const name = specifier.split(/[?#]/u)[0] || specifier;
    for (const [alias, target] of [...aliases].sort(([a], [b]) =>
      b.length - a.length)) {
      if (specifier !== alias && !specifier.startsWith(`${alias}/`)) continue;
      return target === undefined
        ? undefined
        : resolveSource(root, `${target}${name.slice(alias.length)}`);
    }
    if (name.startsWith('.')) {
      return resolveSource(root, join(dirname(importer), name));
    }
    if (isAbsolute(name)) return resolveSource(root, name);
    if (isBuiltin(name) || ['#app', '#imports'].includes(name)) {
      return undefined;
    }
    const packageName = name.split('/').slice(0, name.startsWith('@') ? 2 : 1)
      .join('/');
    const version = packages[packageName];
    requireCondition(version !== undefined, IMPORT_ERROR);
    if (!version.startsWith('file:')) return undefined;
    const directory = sourcePath(root, resolve(root, 'apps/web',
      version.slice(5)));
    const manifest = JSON.parse(readRegular(join(root, directory,
      'package.json')).toString('utf8'));
    const entry = manifest.exports?.[`.${name.slice(packageName.length)}`];
    const target = typeof entry === 'string' ? entry : entry?.default;
    requireCondition(typeof target === 'string', IMPORT_ERROR);
    return resolveSource(root, join(directory, target));
  };
  const walk = (path: string): void => {
    path = sourcePath(root, path);
    if (paths.has(path)) return;
    const base = path.split('/').at(-1)!;
    if (base === 'AGENTS.md' || base === 'CLAUDE.md') return;
    if (path === 'apps/web/app/assets/fonts/tools'
      || path === 'apps/web/app/assets/fonts/README.md') return;
    const full = join(root, path);
    assertParents(full);
    const info = lstatSync(full);
    if (info.isDirectory()) {
      const children = readdirSync(full).sort();
      requireCondition(children.length > 0, 'empty source directory');
      for (const child of children) walk(`${path}/${child}`);
    } else {
      requireCondition(info.isFile(), 'source must be a regular file');
      paths.add(path);
      for (const specifier of moduleImports(path,
        readRegular(full).toString('utf8'),
        path.startsWith(`${app}/`) ? autoImports : new Map())) {
        const dependency = imported(path, specifier);
        if (dependency !== undefined) walk(dependency);
      }
    }
  };
  for (const path of SOURCE_INPUTS) walk(path);
  for (const path of entries) {
    const dependency = path.startsWith('apps/')
      ? resolveSource(root, path)
      : imported('apps/web/nuxt.config.ts', path);
    if (dependency !== undefined) walk(dependency);
  }
  return [...paths].sort();
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
  } catch (error) {
    console.error(error instanceof Error && error.message === IMPORT_ERROR
      ? `template-pages: ${IMPORT_ERROR}`
      : 'template-pages: failed; check inputs and manifest hashes');
    process.exitCode = 1;
  }
}
