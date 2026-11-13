import type { SampleLanguage } from '@aboutme/schema/samples';
import type { Locale } from '../i18n/locale';
import { galleryCopy } from '../i18n/templates';
import {
  FILTERS,
  MEMBERS,
  ROLE_MEMBERS,
  ROLES,
  type GalleryFilter,
  type GalleryTemplate,
} from './catalog';

const FILTER_CHIPS: readonly Exclude<GalleryFilter, 'sample'>[]
  = FILTERS.filter(
    (chip): chip is Exclude<GalleryFilter, 'sample'> => chip !== 'sample',
  );

const LOCALES: readonly Locale[] = ['en', 'vi'];

export type TemplateSearchSubject = Pick<
  GalleryTemplate, 'id' | 'name' | 'purpose' | 'sampleTags'
>;

// NFD leaves đ/Đ as is: U+0111 and U+0110 have no Unicode decomposition.
export function normalizeSearchText(value: string): string {
  return value
    .toLowerCase()
    .replace(/đ/g, 'd')
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .trim();
}

export function templateSearchText(template: TemplateSearchSubject): string {
  const parts: string[] = [template.name];
  for (const locale of LOCALES) {
    parts.push(template.purpose[locale]);
  }
  const { sampleTags } = template;
  if (sampleTags !== undefined) {
    for (const sampleLanguage of Object.keys(sampleTags) as SampleLanguage[]) {
      for (const locale of LOCALES) {
        parts.push(sampleTags[sampleLanguage][locale]);
      }
    }
  }
  for (const role of ROLES) {
    if (ROLE_MEMBERS[role].includes(template.id)) {
      for (const locale of LOCALES) {
        parts.push(galleryCopy[locale].roles[role]);
      }
    }
  }
  for (const filter of FILTER_CHIPS) {
    if (MEMBERS[filter].includes(template.id)) {
      for (const locale of LOCALES) {
        parts.push(galleryCopy[locale].filters[filter]);
      }
    }
  }
  return normalizeSearchText(parts.join(' '));
}

function searchTerms(query: string): readonly string[] {
  return normalizeSearchText(query)
    .split(/\s+/)
    .filter((term) => term !== '');
}

export function matchesTemplateSearch(
  template: TemplateSearchSubject,
  query: string,
): boolean {
  const terms = searchTerms(query);
  if (terms.length === 0) return true;
  const haystack = templateSearchText(template);
  return terms.every((term) => haystack.includes(term));
}
