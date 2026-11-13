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
  readonly template?: string;
  readonly page: number;
}

export type ShowcaseFilters = Omit<ShowcaseQuery, 'page'>;

const TEMPLATE_IDS: ReadonlySet<string> = new Set([
  ...GALLERY.map((template) => template.id),
  SHOWCASE_CUSTOM_TEMPLATE,
]);

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
  readonly value: string;
}

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

export function activeFilterCount(query: ShowcaseFilters): number {
  return activeFilterList(query).length;
}

export function showcaseRouteQuery(query: ShowcaseQuery): LocationQueryRaw {
  return {
    ...(query.role === undefined ? {} : { role: query.role }),
    ...(query.lang === undefined ? {} : { lang: query.lang }),
    ...(query.template === undefined ? {} : { template: query.template }),
    ...(query.page > 1 ? { page: String(query.page) } : {}),
  };
}

export function showcaseSearch(query: ShowcaseQuery): string {
  return new URLSearchParams(
    showcaseRouteQuery(query) as Record<string, string>,
  ).toString();
}
