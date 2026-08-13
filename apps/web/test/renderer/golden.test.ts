// @vitest-environment node

// The byte-exact HTML golden diff is the renderer review artifact.

import type { Resume, Section } from '@aboutme/schema';
import { JSDOM } from 'jsdom';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';
import { describe, expect, it } from 'vitest';

import { PaginationMeasureKey } from '../../app/components/resume/measure';
import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';
import {
  ResumeRenderError,
} from '../../app/components/resume/resolveRenderModel';
import {
  FIXED_PHOTO_DATA_URL,
  FIXED_PHOTO_SHA256,
} from '../../app/pages/_harness/photo-fixture';
import { neutralizationViolations } from '../sanitizer/neutralization';
import {
  assertSafeOutputDirectory,
  buildGoldenMatrix,
  buildStartingDocument,
  localeForFixture,
  renderGoldenCell,
  verifyFixedPhoto,
} from './golden.generate.mts';
import { syntheticMeasure } from './synthetic-measure';

for (const variable of [
  'UPDATE_GOLDEN',
  'PLAYWRIGHT_UPDATE_SNAPSHOTS',
] as const) {
  if (Object.hasOwn(process.env, variable)) {
    throw new Error(`${variable} is forbidden for the read-only golden suite.`);
  }
}

const webRoot = resolve(import.meta.dirname, '../..');
const workspaceRoot = resolve(webRoot, '../..');
const fixturesDirectory = resolve(workspaceRoot, 'packages/schema/fixtures');
const templatesDirectory = resolve(workspaceRoot, 'packages/schema/templates');
const goldenDirectory = resolve(import.meta.dirname, 'golden');

const fixture = (name: string): Resume =>
  JSON.parse(
    readFileSync(resolve(fixturesDirectory, `${name}.json`), 'utf8'),
  ) as Resume;

const renderWithContext = (
  document: Resume,
  context: { lng: string; mode: 'continuous' | 'paged'; photoUrl?: string },
): Promise<string> => {
  const app = createSSRApp({
    render: () => h(ResumeDocument, { document, context }),
  });
  app.provide(PaginationMeasureKey, syntheticMeasure);
  return renderToString(app);
};

describe('golden matrix contract', () => {
  const presetFilenames = readdirSync(templatesDirectory)
    .filter((name) => name.endsWith('.json'))
    .sort();
  const matrix = buildGoldenMatrix();
  const expectedNames = presetFilenames
    .flatMap((presetFilename) => {
      const preset = presetFilename.slice(0, -'.json'.length);
      return [1, 2].flatMap((start) =>
        ['continuous', 'paged'].map(
          (mode) => `${preset}--start-${start}--${mode}.html`,
        ),
      );
    })
    .sort();

  it('derives all 80 names from the preset directory', () => {
    expect(matrix).toHaveLength(presetFilenames.length * 4);
    expect(matrix).toHaveLength(80);
    expect(matrix.map((cell) => cell.filename).sort()).toEqual(expectedNames);
    expect(new Set(matrix.map((cell) => cell.filename)).size).toBe(
      presetFilenames.length * 4,
    );
  });

  it('builds the two populated starting placements in visual order', () => {
    const full = fixture('full');
    const visualOrder = [
      ...full.customization.layout.sections.main,
      ...full.customization.layout.sections.sidebar,
    ];

    expect(
      buildStartingDocument(full, 1).customization.layout.sections,
    ).toEqual({ main: visualOrder, sidebar: [] });
    expect(
      buildStartingDocument(full, 2).customization.layout.sections,
    ).toEqual(full.customization.layout.sections);
    expect(full.customization.layout.columns).toBe(2);
  });

  it('maps fixture languages without ambient locale input', () => {
    expect(localeForFixture('full')).toBe('en');
    expect(localeForFixture('vn-full')).toBe('vi');
    expect(() => localeForFixture('minimal')).toThrow(
      'Unsupported golden fixture locale: minimal',
    );
  });

  it('refuses reserved, tracked, outside, and non-ignored output paths', () => {
    expect(() => assertSafeOutputDirectory(goldenDirectory)).toThrow(
      'reserved tracked golden directory',
    );
    expect(() =>
      assertSafeOutputDirectory(resolve(goldenDirectory, 'nested')),
    ).toThrow('reserved tracked golden directory');
    expect(() =>
      assertSafeOutputDirectory(resolve(webRoot, 'package.json')),
    ).toThrow('must not contain tracked files');
    expect(() =>
      assertSafeOutputDirectory(resolve(tmpdir(), 'aboutme-golden-outside')),
    ).toThrow('must stay inside the workspace');
    expect(() =>
      assertSafeOutputDirectory(resolve(workspaceRoot, 'golden-task-9-output')),
    ).toThrow('must be ignored by Git');
    expect(() =>
      assertSafeOutputDirectory(
        resolve(workspaceRoot, '.dev/golden-task-9-output'),
      ),
    ).not.toThrow();
  });

  it('resolves symlink aliases before Git checks without writing', () => {
    const rootAlias = '/proc/self/root';
    expect(() =>
      assertSafeOutputDirectory(
        resolve(rootAlias, goldenDirectory.slice(1), 'nested'),
      ),
    ).toThrow('reserved tracked golden directory');
    expect(() =>
      assertSafeOutputDirectory(
        resolve(rootAlias, tmpdir().slice(1), 'aboutme-golden-output'),
      ),
    ).toThrow('must stay inside the workspace');
  });

  it.each(matrix)('$filename is deterministic and byte-exact', async (cell) => {
    const first = await renderGoldenCell(cell);
    const second = await renderGoldenCell(cell);
    expect(second).toBe(first);

    const snapshot = resolve(goldenDirectory, cell.filename);
    expect(
      existsSync(snapshot),
      `missing committed golden ${cell.filename}`,
    ).toBe(true);
    expect(first).toBe(readFileSync(snapshot, 'utf8'));
  });
});

describe('fixed photo and render context', () => {
  it('pins decoded PNG bytes by SHA-256', () => {
    expect(FIXED_PHOTO_DATA_URL).toMatch(/^data:image\/png;base64,/);
    expect(FIXED_PHOTO_SHA256).toMatch(/^[0-9a-f]{64}$/);
    expect(() => verifyFixedPhoto()).not.toThrow();
  });

  it('preserves typed photo metadata and URL mismatch failures', async () => {
    await expect(
      renderWithContext(fixture('full'), {
        lng: 'en',
        mode: 'continuous',
      }),
    ).rejects.toMatchObject({
      name: 'ResumeRenderError',
      code: 'photo_url_required',
    });
    await expect(
      renderWithContext(fixture('minimal'), {
        lng: 'en',
        mode: 'continuous',
        photoUrl: FIXED_PHOTO_DATA_URL,
      }),
    ).rejects.toMatchObject({
      name: 'ResumeRenderError',
      code: 'unexpected_photo_url',
    });
    expect(
      new ResumeRenderError('photo_url_required', 'missing'),
    ).toBeInstanceOf(ResumeRenderError);
  });

  it('uses a DOM-free synthetic provider with the exact request shape', () => {
    expect(globalThis).not.toHaveProperty('window');
    expect(globalThis).not.toHaveProperty('document');

    const document = buildStartingDocument(fixture('full'), 1);
    const context = {
      lng: 'en',
      mode: 'paged',
      photoUrl: FIXED_PHOTO_DATA_URL,
    } as const;
    const request = {
      document,
      context,
      columns: 1 as const,
      blocks: [
        {
          sectionKey: 'profile',
          kind: 'heading' as const,
          column: 'main' as const,
        },
        {
          sectionKey: 'profile',
          kind: 'entry' as const,
          entryIndex: 0,
          column: 'main' as const,
        },
      ],
      page: {
        format: 'a4' as const,
        widthPx: 794,
        heightPx: 1123,
        marginXmm: 15,
        marginYmm: 15,
      },
    };
    const measured = syntheticMeasure(request);

    expect(Object.keys(request).sort()).toEqual([
      'blocks',
      'columns',
      'context',
      'document',
      'page',
    ]);
    expect(measured.columns).toBe(request.columns);
    expect(
      measured.blocks.map((block) => ({
        sectionKey: block.sectionKey,
        kind: block.kind,
        ...(block.entryIndex === undefined
          ? {}
          : { entryIndex: block.entryIndex }),
        column: block.column,
      })),
    ).toEqual(request.blocks);
    expect(globalThis).not.toHaveProperty('window');
    expect(globalThis).not.toHaveProperty('document');
  });
});

type SectionOf<T extends Section['sectionType']> = Extract<
  Section,
  { sectionType: T }
>;

const section = <T extends Section['sectionType']>(
  document: Resume,
  key: string,
  type: T,
): SectionOf<T> => {
  const value = document.content[key];
  if (value?.sectionType !== type) throw new Error(`expected ${key}=${type}`);
  return value as SectionOf<T>;
};

const hostileDocument = (html: string): Resume => {
  const document = fixture('full');
  delete document.personalDetails.photo;
  document.personalDetails.details = [];
  section(document, 'profile', 'profile').entries = [
    {
      id: '018f0000-0000-7000-8000-000000000201',
      isHidden: false,
      text: html,
    },
  ];
  section(document, 'work', 'work').entries = [
    {
      id: '018f0000-0000-7000-8000-000000000202',
      isHidden: false,
      description: html,
    },
  ];
  section(document, 'education', 'education').entries = [
    {
      id: '018f0000-0000-7000-8000-000000000203',
      isHidden: false,
      description: html,
    },
  ];
  section(document, 'skill', 'skill').entries = [
    {
      id: '018f0000-0000-7000-8000-000000000204',
      isHidden: false,
      name: 'Kỹ năng',
      infoHtml: html,
    },
  ];
  section(document, 'certificate', 'certificate').entries = [
    {
      id: '018f0000-0000-7000-8000-000000000205',
      isHidden: false,
      description: html,
    },
  ];
  section(document, 'project', 'project').entries = [
    {
      id: '018f0000-0000-7000-8000-000000000206',
      isHidden: false,
      description: html,
    },
  ];
  section(document, 'a6a0a5fa-7fe4-4d52-be40-0da2db95de12', 'custom').entries
    = [
      {
        id: '018f0000-0000-7000-8000-000000000207',
        isHidden: false,
        description: html,
      },
    ];
  document.customization.layout.columns = 1;
  document.customization.layout.sections = {
    main: [
      'profile',
      'work',
      'education',
      'skill',
      'certificate',
      'project',
      'a6a0a5fa-7fe4-4d52-be40-0da2db95de12',
    ],
    sidebar: [],
  };
  return document;
};

describe('hostile document SSR surface', () => {
  const corpus = JSON.parse(
    readFileSync(
      resolve(
        workspaceRoot,
        'apps/server/internal/sanitize/testdata/corpus-output.golden.json',
      ),
      'utf8',
    ),
  ) as Record<string, string>;

  it.each(Object.entries(corpus))(
    '%s stays neutralized inside every rich-text container',
    async (_name, payload) => {
      const html = await renderWithContext(hostileDocument(payload), {
        lng: 'en',
        mode: 'continuous',
      });
      const parsed = new JSDOM(html).window.document;
      const containers = [
        ...parsed.querySelectorAll<HTMLElement>('.rich-text'),
      ];
      const expectedCount = payload === '' ? 0 : 7;
      expect(containers).toHaveLength(expectedCount);

      const payloadDocument = new JSDOM('').window.document;
      const payloadTemplate = payloadDocument.createElement('template');
      payloadTemplate.innerHTML = payload;
      const canonicalPayload = payloadTemplate.innerHTML;
      for (const container of containers) {
        expect(container.innerHTML).toBe(canonicalPayload);
      }

      const previousDOMParser = globalThis.DOMParser;
      globalThis.DOMParser = new JSDOM('').window.DOMParser;
      try {
        for (const container of containers) {
          expect(neutralizationViolations(container.innerHTML)).toEqual([]);
        }
      } finally {
        if (previousDOMParser === undefined) delete globalThis.DOMParser;
        else globalThis.DOMParser = previousDOMParser;
      }

      const payloadElements = [
        ...payloadTemplate.content.querySelectorAll<HTMLElement>('*'),
      ];
      const payloadTags = new Set(
        payloadElements.map((element) => element.tagName.toLowerCase()),
      );
      const payloadAttributes = new Set(
        payloadElements.flatMap((element) =>
          [...element.attributes].map(
            (attribute) => `${attribute.name}=${attribute.value}`,
          ),
        ),
      );
      for (const element of parsed.querySelectorAll<HTMLElement>('*')) {
        if (payloadTags.has(element.tagName.toLowerCase())) {
          expect(element.closest('.rich-text')).not.toBeNull();
        }
        for (const attribute of element.attributes) {
          if (payloadAttributes.has(`${attribute.name}=${attribute.value}`)) {
            expect(element.closest('.rich-text')).not.toBeNull();
          }
        }
      }
      if (payload !== '') {
        expect(html.split(payload)).toHaveLength(8);
      }
    },
  );
});
