import type { Section } from '@aboutme/schema';
import type { Locale } from '../../i18n/locale';
import {
  editorSectionsCopy,
  sectionIconKeys,
  sectionTypeLabelsForLocale,
} from '../../i18n/editor-sections';
import type { SectionIconKey } from '../../i18n/editor-sections';

type SectionType = Section['sectionType'];

export { sectionTypeLabelsForLocale };

export const defaultSectionNames: Readonly<Record<SectionType, string>> = {
  profile: 'Summary',
  work: 'Experience',
  education: 'Education',
  skill: 'Skills',
  language: 'Languages',
  certificate: 'Certifications',
  project: 'Projects',
  custom: 'Custom section',
};

export const defaultSectionIcons: Readonly<
  Record<SectionType, string | null>
> = {
  profile: 'user',
  work: 'briefcase',
  education: 'graduation-cap',
  skill: 'code',
  language: 'languages',
  certificate: 'award',
  project: 'folder',
  custom: null,
};

type SectionIconOption = {
  readonly value: SectionIconKey;
  readonly label: string;
};

export const sectionIconOptions: readonly SectionIconOption[] = sectionIconKeys
  .map((value) => ({
    value,
    label: editorSectionsCopy.en.iconLabels[value],
  }));

export function sectionIconOptionsForLocale(
  locale: Locale,
): readonly SectionIconOption[] {
  const copy = editorSectionsCopy[locale];
  return sectionIconOptions.map((option) => ({
    value: option.value,
    label: copy.iconLabels[option.value] ?? option.label,
  }));
}

export function entryLabel(
  value: object,
  index: number,
  locale: Locale = 'en',
): string {
  const entry = value as Record<string, unknown>;
  for (const field of ['jobTitle', 'degree', 'name', 'title'] as const) {
    const text = entry[field];
    if (typeof text === 'string' && text !== '') return text;
  }
  return editorSectionsCopy[locale].entry(index);
}
