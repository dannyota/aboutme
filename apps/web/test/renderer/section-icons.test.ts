import type { Resume, Section } from '@aboutme/schema';
import { mount } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import { sectionIconOptions } from '../../app/components/editor/sectionTypes';
import {
  hasIcon,
  sectionIconKey,
} from '../../app/components/resume/icons';
import SectionHeading from
  '../../app/components/resume/primitives/SectionHeading.vue';
import { resolveRenderModel } from
  '../../app/components/resume/resolveRenderModel';

// The schema accepts any kebab-case Lucide name as a section iconKey; the
// renderer draws a curated set and falls back to the section type's icon
// (docs/design/templates/contract.md §5.3).

function section(
  sectionType: Section['sectionType'],
  iconKey?: string,
): Section {
  return {
    sectionType,
    entries: [],
    ...(iconKey === undefined ? {} : { iconKey }),
  } as Section;
}

describe('section heading icons', () => {
  it('draws every icon the editor offers', () => {
    const offered = sectionIconOptions
      .map(({ value }) => value)
      .filter((value) => value !== '');
    expect(offered.length).toBeGreaterThan(40);
    expect(new Set(offered).size).toBe(offered.length);
    expect(offered.filter((value) => !hasIcon(value))).toEqual([]);
  });

  it.each(['star', 'target', 'wrench', 'landmark'])(
    'draws %s, which the schema accepts',
    (key) => {
      expect(hasIcon(key)).toBe(true);
      const heading = mount(SectionHeading, {
        props: { displayName: 'Section', iconKey: key },
      });
      expect(heading.find('svg.resume-icon').exists()).toBe(true);
    },
  );

  it.each([
    ['profile', 'user'],
    ['work', 'briefcase'],
    ['education', 'graduation-cap'],
    ['skill', 'code'],
    ['language', 'languages'],
    ['certificate', 'award'],
    ['project', 'folder'],
    ['custom', 'bookmark'],
  ] as const)('falls back to the %s icon for a name it lacks', (type, icon) => {
    expect(sectionIconKey(section(type, 'not-a-real-icon'))).toBe(icon);
    expect(hasIcon(icon)).toBe(true);
  });

  it('keeps known, absent, and empty keys as they are', () => {
    expect(sectionIconKey(section('custom', 'star'))).toBe('star');
    expect(sectionIconKey(section('custom'))).toBeUndefined();
    expect(sectionIconKey(section('custom', ''))).toBe('');
    // Own properties only: an Object.prototype name is not an icon.
    expect(sectionIconKey(section('work', 'constructor'))).toBe('briefcase');
  });

  it('resolves headings with the fallback and keeps other sections as is',
    () => {
      const document = JSON.parse(readFileSync(resolve(
        process.cwd(),
        '../../packages/schema/fixtures/full.json',
      ), 'utf8')) as Resume;
      delete document.personalDetails.photo;
      const [firstKey] = document.customization.layout.sections.main;
      const first = document.content[firstKey!]!;
      first.iconKey = 'unknown-lucide-name';
      const model = resolveRenderModel(document, {
        lng: 'en',
        mode: 'continuous',
      });
      const resolved = [...model.main, ...model.sidebar];
      const fallback = resolved.find(({ key }) => key === firstKey)!;
      expect(fallback.section.iconKey)
        .toBe(sectionIconKey({ ...first, iconKey: 'unknown-lucide-name' }));
      expect(hasIcon(fallback.section.iconKey!)).toBe(true);
      for (const { key, section: item } of resolved) {
        if (key !== firstKey) expect(item).toBe(document.content[key]);
      }
    });
});
