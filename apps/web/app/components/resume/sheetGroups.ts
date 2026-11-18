import type { Section } from '@aboutme/schema';

import type { ResolvedSection } from './resolveRenderModel';

export interface ResumeSheet {
  readonly header: boolean;
  readonly sections: readonly ResolvedSection[];
}

const SOLO_TYPES: ReadonlySet<Section['sectionType']> = new Set([
  'profile',
  'work',
  'education',
  'project',
]);
const SMALL_SECTIONS_PER_SHEET = 3;
const SOLO_CUSTOM_ENTRIES = 3;

const visibleEntries = (section: Section): number =>
  section.entries.filter((entry) => !entry.isHidden).length;

const standsAlone = (section: Section): boolean =>
  SOLO_TYPES.has(section.sectionType)
  || (section.sectionType === 'custom'
    && visibleEntries(section) >= SOLO_CUSTOM_ENTRIES);

export function groupSheets(
  sections: readonly ResolvedSection[],
): ResumeSheet[] {
  const visible = sections.filter(({ section }) =>
    visibleEntries(section) > 0);
  const summary = visible[0]?.section.sectionType === 'profile';
  const sheets: { header: boolean; sections: ResolvedSection[] }[] = [
    { header: true, sections: summary ? [visible[0]!] : [] },
  ];
  let run: ResolvedSection[] | undefined;
  for (const item of summary ? visible.slice(1) : visible) {
    if (standsAlone(item.section)) {
      sheets.push({ header: false, sections: [item] });
      run = undefined;
      continue;
    }
    if (run === undefined || run.length === SMALL_SECTIONS_PER_SHEET) {
      run = [];
      sheets.push({ header: false, sections: run });
    }
    run.push(item);
  }
  return sheets;
}

export function folioAttributes(
  fullName: string | undefined,
  index: number,
  count: number,
): Record<string, string> {
  return {
    ...(fullName === undefined || fullName === ''
      ? {}
      : { 'data-folio-name': fullName }),
    'data-folio-page': `${index + 1} / ${count}`,
  };
}
