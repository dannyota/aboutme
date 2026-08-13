import type { Content, Customization } from '@aboutme/schema';
import type { TemplatePreset } from '@aboutme/schema/templates';
import Ajv2020 from 'ajv/dist/2020.js';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

import {
  TemplateApplyError,
  applyTemplate,
} from '../../app/components/resume/applyTemplate';

const schema = JSON.parse(
  readFileSync('../../packages/schema/resume.schema.json', 'utf8'),
);
const ajv = new Ajv2020({ allErrors: true, strict: true });
const validateCustomization = ajv.compile({
  $schema: schema.$schema,
  $defs: schema.$defs,
  $ref: '#/$defs/customization',
});

const content: Content = {
  workA: { sectionType: 'work', entries: [] },
  customA: { sectionType: 'custom', entries: [] },
  skillA: { sectionType: 'skill', entries: [] },
  workB: { sectionType: 'work', entries: [] },
  languageA: { sectionType: 'language', entries: [] },
};

const current = {
  font: { family: 'inter', baseSizePx: 14 },
  colors: {
    primary: '#111111',
    text: '#222222',
    background: '#ffffff',
  },
  spacing: { sectionGap: 16, entryGap: 8, lineHeight: 1.4 },
  heading: { style: 'normal', showRule: false },
  layout: {
    columns: 2,
    sections: {
      main: ['workA', 'customA', 'skillA'],
      sidebar: ['workB', 'languageA'],
    },
  },
  sectionDisplay: {
    skill: { style: 'text' },
    language: { style: 'text' },
  },
  pageFormat: 'a4',
  dateFormat: 'MM/YYYY',
} satisfies Customization;

function preset(
  placement: 'keep' | 'byType',
  selectors?: readonly ('skill' | 'language')[],
): TemplatePreset {
  return {
    id: 'test',
    name: 'Test',
    description: 'Test preset',
    customization: {
      ...structuredClone(current),
      font: { family: 'source-sans-3', baseSizePx: 12 },
      layout: {
        columns: 2,
        placement,
        ...(selectors === undefined
          ? {}
          : { sidebarSectionTypes: selectors }),
      },
    },
  };
}

const selectorTypes = [
  'profile',
  'work',
  'education',
  'skill',
  'language',
  'certificate',
  'project',
] as const;
const contentTypes = [...selectorTypes, 'custom'] as const;
const propertyKeys = [
  'alpha',
  'beta',
  'gamma',
  'delta',
  'epsilon',
  'zeta',
  'eta',
  'theta',
];

function propertyPreset(
  placement: 'keep' | 'byType',
  selectors?: readonly (typeof selectorTypes)[number][],
): TemplatePreset {
  return {
    id: 'property',
    name: 'Property',
    description: 'Property preset',
    customization: {
      font: { family: 'source-sans-3', baseSizePx: 12 },
      colors: {
        primary: '#0f172a',
        text: '#1e293b',
        background: '#f8fafc',
      },
      spacing: { sectionGap: 24, entryGap: 12, lineHeight: 1.5 },
      heading: { style: 'uppercase', showRule: true },
      header: {
        align: 'center',
        detailsLayout: 'inline',
        iconStyle: 'outline',
      },
      layout: {
        columns: 1,
        surfaceTarget: 'header',
        placement,
        ...(placement === 'byType'
          ? { sidebarSectionTypes: selectors ?? [] }
          : {}),
      },
      sectionDisplay: {
        skill: { style: 'tag' },
        language: { style: 'bar' },
      },
      pageFormat: 'letter',
      dateFormat: 'YYYY',
    },
  };
}

function mulberry32(seed: number): () => number {
  let state = seed | 0;
  return () => {
    state = (state + 0x6d2b79f5) | 0;
    let value = Math.imul(state ^ (state >>> 15), 1 | state);
    value = (value + Math.imul(value ^ (value >>> 7), 61 | value)) ^ value;
    return ((value ^ (value >>> 14)) >>> 0) / 4294967296;
  };
}

function shuffle<T>(values: readonly T[], random: () => number): T[] {
  const result = [...values];
  for (let index = result.length - 1; index > 0; index -= 1) {
    const other = Math.floor(random() * (index + 1));
    [result[index], result[other]] = [result[other], result[index]];
  }
  return result;
}

function propertyBaseCustomization(): Customization {
  return {
    font: { family: 'inter', baseSizePx: 14 },
    colors: {
      primary: '#111111',
      text: '#222222',
      background: '#ffffff',
    },
    spacing: { sectionGap: 16, entryGap: 8, lineHeight: 1.4 },
    heading: { style: 'normal', showRule: false },
    header: { align: 'left', detailsLayout: 'stacked', iconStyle: 'none' },
    layout: { columns: 2, sections: { main: [], sidebar: [] } },
    sectionDisplay: {
      skill: { style: 'text' },
      language: { style: 'text' },
    },
    pageFormat: 'a4',
    dateFormat: 'MM/YYYY',
  };
}

describe('applyTemplate adversarial placement contract', () => {
  it(
    'keeps validated placement byte-for-byte and replaces other tokens',
    () => {
      const currentBefore = structuredClone(current);
      const contentBefore = structuredClone(content);
      const result = applyTemplate(current, preset('keep'), content);

      expect(result.layout.sections).toEqual(current.layout.sections);
      expect(result.font.family).toBe('source-sans-3');
      expect(current).toEqual(currentBefore);
      expect(content).toEqual(contentBefore);
    },
  );

  it('orders selected types by selector rank then current visual order', () => {
    const result = applyTemplate(
      current,
      preset('byType', ['language', 'skill']),
      content,
    );

    expect(result.layout.sections).toEqual({
      main: ['workA', 'customA', 'workB'],
      sidebar: ['languageA', 'skillA'],
    });
  });

  it('does not depend on content object insertion order', () => {
    const reversed = Object.fromEntries(Object.entries(content).reverse());
    expect(
      applyTemplate(
        current,
        preset('byType', ['skill', 'language']),
        reversed,
      ).layout.sections,
    ).toEqual({
      main: ['workA', 'customA', 'workB'],
      sidebar: ['skillA', 'languageA'],
    });
  });

  it.each([
    ['missing key', { main: ['workA'], sidebar: [] }],
    [
      'duplicate key',
      {
        main: ['workA', 'customA', 'skillA', 'workB'],
        sidebar: ['workB', 'languageA'],
      },
    ],
    [
      'unknown key',
      {
        main: ['workA', 'customA', 'skillA', 'unknown'],
        sidebar: ['workB', 'languageA'],
      },
    ],
  ])('rejects invalid current placement: %s', (_name, sections) => {
    const invalid = structuredClone(current);
    invalid.layout.sections = sections;
    expect(() => applyTemplate(invalid, preset('keep'), content)).toThrowError(
      expect.objectContaining({ code: 'invalid_current_placement' }),
    );
  });

  it.each([
    ['duplicate selectors', ['skill', 'skill']],
    ['custom selector', ['custom']],
  ])('rejects invalid byType preset placement: %s', (_name, selectors) => {
    const invalidPreset = preset(
      'byType',
      selectors as unknown as readonly ('skill' | 'language')[],
    );
    expect(() => applyTemplate(current, invalidPreset, content)).toThrowError(
      expect.objectContaining({ code: 'invalid_preset_placement' }),
    );
  });

  it('rejects selectors on keep', () => {
    const valid = preset('keep');
    const invalidPreset: TemplatePreset = {
      ...valid,
      customization: {
        ...valid.customization,
        layout: {
          ...valid.customization.layout,
          sidebarSectionTypes: ['skill'],
        },
      },
    };
    expect(() => applyTemplate(current, invalidPreset, content)).toThrow(
      TemplateApplyError,
    );
  });

  it('rejects an unknown preset placement', () => {
    const valid = propertyPreset('keep');
    const invalidPreset: TemplatePreset = {
      ...valid,
      customization: {
        ...valid.customization,
        layout: {
          columns: 2,
          placement: 'bytype',
          sidebarSectionTypes: ['skill'],
        },
      },
    };
    expect(() => applyTemplate(current, invalidPreset, content)).toThrowError(
      expect.objectContaining({ code: 'invalid_preset_placement' }),
    );
  });

  it('maps empty content to two empty arrays', () => {
    const empty = structuredClone(current);
    empty.layout.sections = { main: [], sidebar: [] };
    expect(
      applyTemplate(empty, preset('byType', ['skill']), {}).layout.sections,
    ).toEqual({ main: [], sidebar: [] });
  });

  it('satisfies seeded placement properties and schema validity', () => {
    const random = mulberry32(0x51a17e3d);
    for (let caseIndex = 0; caseIndex < 256; caseIndex += 1) {
      const keyCount = Math.floor(random() * (propertyKeys.length + 1));
      const keys = propertyKeys.slice(0, keyCount);
      const propertyContent: Content = {};
      for (const key of keys) {
        const sectionType = contentTypes[
          Math.floor(random() * contentTypes.length)
        ];
        propertyContent[key] = {
          sectionType,
          entries: [],
        } as Content[string];
      }

      const visualOrder = shuffle(keys, random);
      const split = Math.floor(random() * (visualOrder.length + 1));
      const main = visualOrder.slice(0, split);
      const sidebar = visualOrder.slice(split);
      const propertyCurrent = propertyBaseCustomization();
      propertyCurrent.layout.sections = { main, sidebar };

      const shuffledSelectors = shuffle(selectorTypes, random);
      const selectorCount = Math.floor(random() * (selectorTypes.length + 1));
      const selectors = shuffledSelectors.slice(0, selectorCount);
      const propertyPresetForCase = propertyPreset('byType', selectors);

      const currentBefore = structuredClone(propertyCurrent);
      const contentBefore = structuredClone(propertyContent);
      const result = applyTemplate(
        propertyCurrent,
        propertyPresetForCase,
        propertyContent,
      );

      const expectedSidebar: string[] = [];
      for (const selector of selectors) {
        for (const key of visualOrder) {
          if (propertyContent[key]?.sectionType === selector) {
            expectedSidebar.push(key);
          }
        }
      }
      const expectedSidebarKeys = new Set(expectedSidebar);
      const expectedMain = visualOrder.filter(
        (key) => !expectedSidebarKeys.has(key),
      );

      expect(result.layout.sections).toEqual({
        main: expectedMain,
        sidebar: expectedSidebar,
      });
      const allKeys = [
        ...result.layout.sections.main,
        ...result.layout.sections.sidebar,
      ];
      expect(allKeys).toHaveLength(keys.length);
      expect(new Set(allKeys)).toEqual(new Set(keys));
      const presetCustomization = propertyPresetForCase.customization;
      expect(result.font).toEqual(presetCustomization.font);
      expect(result.colors).toEqual(presetCustomization.colors);
      expect(result.spacing).toEqual(presetCustomization.spacing);
      expect(result.heading).toEqual(presetCustomization.heading);
      expect(result.sectionDisplay).toEqual(
        presetCustomization.sectionDisplay,
      );
      expect(result.pageFormat).toBe(presetCustomization.pageFormat);
      expect(result.dateFormat).toBe(presetCustomization.dateFormat);
      if (presetCustomization.header !== undefined) {
        expect(result.header).toEqual(presetCustomization.header);
      }
      expect(result.layout.columns).toBe(presetCustomization.layout.columns);
      expect(result.layout.surfaceTarget).toBe(
        presetCustomization.layout.surfaceTarget,
      );
      expect(propertyCurrent).toEqual(currentBefore);
      expect(propertyContent).toEqual(contentBefore);
      expect(
        validateCustomization(result),
        ajv.errorsText(validateCustomization.errors),
      ).toBe(true);
    }
  });
});
