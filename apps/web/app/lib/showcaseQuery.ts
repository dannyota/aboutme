// The showcase page's URL query: role, lang, template, and page. A value the
// page does not know counts as All and is never sent to the API; a page
// outside 1 to 100 counts as 1 (docs/design/showcase.md, Order and filters;
// docs/design/ui/showcase.md, Active filters).
import type { LocationQuery, LocationQueryRaw } from 'vue-router';

import { GALLERY } from '../templates/catalog';
import {
  SHOWCASE_CUSTOM_TEMPLATE,
  SHOWCASE_FILTER_LANGUAGES,
  SHOWCASE_MAX_PAGE,
  SHOWCASE_ROLES,
  type ShowcaseFilterLanguage,
  type ShowcaseRole,
} from './showcaseContract';

export interface ShowcaseQuery {
  readonly role?: ShowcaseRole;
  readonly lang?: ShowcaseFilterLanguage;
  /** A preset ID or `custom`. */
  readonly template?: string;
  readonly page: number;
}

export type ShowcaseFilters = Omit<ShowcaseQuery, 'page'>;

const TEMPLATE_IDS: ReadonlySet<string> = new Set([
  ...GALLERY.map((template) => template.id),
  SHOWCASE_CUSTOM_TEMPLATE,
]);

/** The single string of a query value; a repeated key counts as unknown. */
function single(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

export function parseShowcaseQuery(query: LocationQuery): ShowcaseQuery {
  const role = SHOWCASE_ROLES.find((known) => known === single(query.role));
  const lang = SHOWCASE_FILTER_LANGUAGES.find(
    (known) => known === single(query.lang),
  );
  const template = single(query.template);
  const rawPage = single(query.page);
  const page = rawPage !== undefined && /^[1-9][0-9]{0,2}$/u.test(rawPage)
    ? Number(rawPage)
    : 1;
  return {
    ...(role === undefined ? {} : { role }),
    ...(lang === undefined ? {} : { lang }),
    ...(template !== undefined && TEMPLATE_IDS.has(template)
      ? { template }
      : {}),
    page: page <= SHOWCASE_MAX_PAGE ? page : 1,
  };
}

export function hasFilters(query: ShowcaseFilters): boolean {
  return query.role !== undefined
    || query.lang !== undefined
    || query.template !== undefined;
}

export type ShowcaseFilterKind = 'role' | 'lang' | 'template';

export interface ActiveFilter {
  readonly kind: ShowcaseFilterKind;
  /** The role, the language filter value, or the template ID. */
  readonly value: string;
}

/** The filters that are on, in chip order: role, language, template. */
export function activeFilterList(query: ShowcaseFilters): ActiveFilter[] {
  return [
    ...(query.role === undefined
      ? []
      : [{ kind: 'role' as const, value: query.role }]),
    ...(query.lang === undefined
      ? []
      : [{ kind: 'lang' as const, value: query.lang }]),
    ...(query.template === undefined
      ? []
      : [{ kind: 'template' as const, value: query.template }]),
  ];
}

/** How many filters are on, 0 to 3; the Filters button badge shows it. */
export function activeFilterCount(query: ShowcaseFilters): number {
  return activeFilterList(query).length;
}

/** The route query for a link: known filters, and `page` above 1. */
export function showcaseRouteQuery(query: ShowcaseQuery): LocationQueryRaw {
  return {
    ...(query.role === undefined ? {} : { role: query.role }),
    ...(query.lang === undefined ? {} : { lang: query.lang }),
    ...(query.template === undefined ? {} : { template: query.template }),
    ...(query.page > 1 ? { page: String(query.page) } : {}),
  };
}

/** The listing request's search string: only known values, page above 1. */
export function showcaseSearch(query: ShowcaseQuery): string {
  return new URLSearchParams(
    showcaseRouteQuery(query) as Record<string, string>,
  ).toString();
}
