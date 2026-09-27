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

/**
 * Search over the template gallery catalog (DESIGN.md, editor Templates
 * panel: the search box finds a template by name, style, sample tag, role,
 * or Library filter chip). A template's display name is a single,
 * unlocalized string, so only its purpose, sample tags, and chip labels
 * vary by site language; those are matched in both languages regardless of
 * the reader's current UI language, so a query in either language still
 * finds a match.
 */

const FILTER_CHIPS: readonly Exclude<GalleryFilter, 'sample'>[]
  = FILTERS.filter(
    (chip): chip is Exclude<GalleryFilter, 'sample'> => chip !== 'sample',
  );

const LOCALES: readonly Locale[] = ['en', 'vi'];

/** The catalog fields a search reads from a template. */
export type TemplateSearchSubject = Pick<
  GalleryTemplate, 'id' | 'name' | 'purpose' | 'sampleTags'
>;

/**
 * Lowercases, removes Vietnamese diacritics (đ/Đ included, which NFD does
 * not decompose), and trims, so search matching ignores case and accents.
 */
export function normalizeSearchText(value: string): string {
  return value
    .toLowerCase()
    .replace(/đ/g, 'd')
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .trim();
}

/**
 * The normalized text a template offers for search: its name, its purpose
 * and sample tags in both languages, and the labels of any role chip it is
 * the sample for, in both languages.
 */
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

/**
 * A search query split into normalized, non-empty, whitespace-separated
 * terms.
 */
function searchTerms(query: string): readonly string[] {
  return normalizeSearchText(query)
    .split(/\s+/)
    .filter((term) => term !== '');
}

/**
 * Whether a template matches a search query: every term must appear
 * somewhere in its searchable text. An empty or blank query matches every
 * template.
 */
export function matchesTemplateSearch(
  template: TemplateSearchSubject,
  query: string,
): boolean {
  const terms = searchTerms(query);
  if (terms.length === 0) return true;
  const haystack = templateSearchText(template);
  return terms.every((term) => haystack.includes(term));
}
