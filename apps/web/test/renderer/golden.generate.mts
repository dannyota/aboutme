import { createHash } from 'node:crypto';
import {
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  realpathSync,
  writeFileSync,
} from 'node:fs';
import { dirname, isAbsolute, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';

import { applyTemplate } from '../../app/components/resume/applyTemplate';
import { PaginationMeasureKey } from '../../app/components/resume/measure';
import {
  FIXED_PHOTO_DATA_URL,
  FIXED_PHOTO_SHA256,
} from '../../app/pages/_harness/photo-fixture';
import { syntheticMeasure } from './synthetic-measure';

const webRoot = resolve(import.meta.dirname, '../..');
const workspaceRoot = resolve(webRoot, '../..');
const fixturesDirectory = resolve(workspaceRoot, 'packages/schema/fixtures');
const templatesDirectory = resolve(workspaceRoot, 'packages/schema/templates');
const reservedGoldenDirectory = resolve(import.meta.dirname, 'golden');

/**
 * @typedef {object} GoldenCell
 * @property {string} presetFilename
 * @property {string} presetId
 * @property {1 | 2} start
 * @property {'continuous' | 'paged'} mode
 * @property {string} filename
 */

/** @param {string} path */
const readJson = (path) => JSON.parse(readFileSync(path, 'utf8'));

const presetFilenames = () =>
  readdirSync(templatesDirectory)
    .filter((name) => name.endsWith('.json'))
    .sort();

/** @returns {GoldenCell[]} */
export function buildGoldenMatrix() {
  return presetFilenames().flatMap((presetFilename) => {
    const presetId = presetFilename.slice(0, -'.json'.length);
    return [1, 2].flatMap((start) =>
      ['continuous', 'paged'].map((mode) => ({
        presetFilename,
        presetId,
        start,
        mode,
        filename: `${presetId}--start-${start}--${mode}.html`,
      })),
    );
  });
}

/** @param {string} name */
export function localeForFixture(name) {
  if (name === 'full') return 'en';
  if (name === 'vn-full') return 'vi';
  throw new Error(`Unsupported golden fixture locale: ${name}`);
}

/**
 * @param {import('@aboutme/schema').Resume} source
 * @param {1 | 2} start
 */
export function buildStartingDocument(source, start) {
  const document = structuredClone(source);
  if (start === 1) {
    document.customization.layout.columns = 1;
    document.customization.layout.sections = {
      main: [
        ...source.customization.layout.sections.main,
        ...source.customization.layout.sections.sidebar,
      ],
      sidebar: [],
    };
    return document;
  }

  document.customization.layout.columns = 2;
  document.customization.layout.sections = structuredClone(
    source.customization.layout.sections,
  );
  return document;
}

export function verifyFixedPhoto() {
  const prefix = 'data:image/png;base64,';
  if (!FIXED_PHOTO_DATA_URL.startsWith(prefix)) {
    throw new Error('Fixed photo must be an inline PNG data URL.');
  }
  const bytes = Buffer.from(
    FIXED_PHOTO_DATA_URL.slice(prefix.length),
    'base64',
  );
  const pngSignature = bytes.subarray(0, 8).toString('hex');
  if (pngSignature !== '89504e470d0a1a0a') {
    throw new Error('Fixed photo data does not decode to a PNG.');
  }
  const hash = createHash('sha256').update(bytes).digest('hex');
  if (hash !== FIXED_PHOTO_SHA256) {
    throw new Error(
      'Fixed photo SHA-256 mismatch: '
      + `expected ${FIXED_PHOTO_SHA256}, got ${hash}.`,
    );
  }
}

/** @param {string} path */
const canonicalPath = (path) => {
  /** @type {string[]} */
  const tail = [];
  let ancestor = resolve(path);
  while (!existsSync(ancestor)) {
    const parent = dirname(ancestor);
    if (parent === ancestor) break;
    tail.unshift(ancestor.slice(parent.length + 1));
    ancestor = parent;
  }
  return resolve(realpathSync.native(ancestor), ...tail);
};

/** @param {string} parent @param {string} child */
const isWithin = (parent, child) => {
  const path = relative(parent, child);
  return path === '' || (!path.startsWith('..') && !isAbsolute(path));
};

/** @param {readonly string[]} args */
const git = (args) =>
  spawnSync('git', [...args], {
    cwd: workspaceRoot,
    encoding: 'utf8',
    env: { ...process.env, GIT_OPTIONAL_LOCKS: '0' },
  });

/** @param {string} output */
export function assertSafeOutputDirectory(output) {
  const target = canonicalPath(output);
  const root = realpathSync.native(workspaceRoot);
  const reserved = canonicalPath(reservedGoldenDirectory);
  if (!isWithin(root, target)) {
    throw new Error('Golden output directory must stay inside the workspace.');
  }
  if (isWithin(reserved, target)) {
    throw new Error(
      'Refusing the reserved tracked golden directory or an alias.',
    );
  }

  const relativeTarget = relative(root, target);
  const tracked = git(['ls-files', '--', relativeTarget]);
  if (tracked.status !== 0) {
    throw new Error(`Could not inspect tracked output path: ${tracked.stderr}`);
  }
  if (tracked.stdout.trim() !== '') {
    throw new Error('Golden output directory must not contain tracked files.');
  }

  const ignored = git(['check-ignore', '--quiet', '--', relativeTarget]);
  if (ignored.status !== 0) {
    if (ignored.status !== 1) {
      throw new Error(
        `Could not inspect ignored output path: ${ignored.stderr}`,
      );
    }
    throw new Error('Golden output directory must be ignored by Git.');
  }
  return target;
}

const loadResumeDocument = async () =>
  (await import('../../app/components/resume/ResumeDocument.vue')).default;

/** @param {GoldenCell} cell */
export async function renderGoldenCell(cell) {
  verifyFixedPhoto();
  /** @type {import('@aboutme/schema').Resume} */
  const source = readJson(resolve(fixturesDirectory, 'full.json'));
  const startingDocument = buildStartingDocument(source, cell.start);
  /** @type {import('@aboutme/schema/templates').TemplatePreset} */
  const preset = readJson(resolve(templatesDirectory, cell.presetFilename));
  if (preset.id !== cell.presetId) {
    throw new Error(
      `Preset filename ${cell.presetFilename} does not match id ${preset.id}.`,
    );
  }
  /** @type {import('@aboutme/schema').Resume} */
  const document = {
    ...startingDocument,
    customization: applyTemplate(
      startingDocument.customization,
      preset,
      startingDocument.content,
    ),
  };
  /**
   * @type {
   *   {import('../../app/components/resume/resolveRenderModel').RenderContext}
   * }
   */
  const context = {
    lng: localeForFixture('full'),
    mode: cell.mode,
    photoUrl: FIXED_PHOTO_DATA_URL,
  };
  const ResumeDocument = await loadResumeDocument();
  const app = createSSRApp({
    render: () => h(ResumeDocument, { document, context }),
  });
  app.provide(PaginationMeasureKey, syntheticMeasure);
  return renderToString(app);
}

/** @param {string} output */
export async function generateGoldenFiles(output) {
  const target = assertSafeOutputDirectory(output);
  verifyFixedPhoto();
  mkdirSync(target, { recursive: true });

  /** @type {string[]} */
  const written = [];
  for (const cell of buildGoldenMatrix()) {
    const html = await renderGoldenCell(cell);
    const path = resolve(target, cell.filename);
    writeFileSync(path, html, 'utf8');
    written.push(path);
  }
  return written;
}

const isDirectExecution
  = process.argv[1] !== undefined
    && resolve(process.argv[1]) === fileURLToPath(import.meta.url);

const runCli = async () => {
  const args = process.argv.slice(2);
  if (args.length !== 1) {
    throw new Error(
      'Usage: npx --no-install tsx '
      + 'test/renderer/golden.generate.mts <output-dir>',
    );
  }

  const [{ createServer }, { default: vue }] = await Promise.all([
    import('vite'),
    import('@vitejs/plugin-vue'),
  ]);
  const server = await createServer({
    root: webRoot,
    configFile: false,
    appType: 'custom',
    plugins: [vue()],
    server: { middlewareMode: true },
  });
  try {
    process.env.ABOUTME_GOLDEN_VITE_CHILD = '1';
    const loaded = await server.ssrLoadModule(
      '/test/renderer/golden.generate.mts',
    );
    const written = await loaded.generateGoldenFiles(args[0]);
    process.stdout.write(`Wrote ${written.length} golden HTML files.\n`);
  } finally {
    delete process.env.ABOUTME_GOLDEN_VITE_CHILD;
    await server.close();
  }
};

if (isDirectExecution && process.env.ABOUTME_GOLDEN_VITE_CHILD !== '1') {
  await runCli();
}
