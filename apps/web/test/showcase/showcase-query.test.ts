import { describe, expect, it } from 'vitest';

import {
  isShowcaseRole,
  parseOwnerShowcase,
  parseShowcaseListing,
  SHOWCASE_ROLES,
  showcaseCardUrl,
} from '../../app/lib/showcaseContract';
import {
  hasFilters,
  parseShowcaseQuery,
  showcaseRouteQuery,
  showcaseSearch,
} from '../../app/lib/showcaseQuery';
import { GALLERY, ROLES } from '../../app/templates/catalog';

// The filters live in the page URL (AC-SHOW-005); an unknown value counts as
// All and is never sent (docs/design/ui/landing-and-library.md).

const presetId = GALLERY[0]!.id;

describe('showcase query', () => {
  it('reads every known filter and the page', () => {
    expect(parseShowcaseQuery({
      role: 'data-ai',
      lang: 'en',
      template: presetId,
      page: '7',
    })).toEqual({ role: 'data-ai', lang: 'en', template: presetId, page: 7 });
  });

  it('accepts custom as a template and other as a role', () => {
    expect(parseShowcaseQuery({ role: 'other', template: 'custom' }))
      .toEqual({ role: 'other', template: 'custom', page: 1 });
  });

  it('counts an unknown value as All', () => {
    const query = parseShowcaseQuery({
      role: 'wizard',
      lang: 'other',
      template: 'no-such-preset',
    });
    expect(query).toEqual({ page: 1 });
    expect(hasFilters(query)).toBe(false);
    expect(showcaseSearch(query)).toBe('');
  });

  it('treats a repeated key as unknown', () => {
    expect(parseShowcaseQuery({ role: ['backend', 'qa'], page: ['2', '3'] }))
      .toEqual({ page: 1 });
  });

  it.each([
    ['0', 1],
    ['-1', 1],
    ['101', 1],
    ['1000', 1],
    ['2.5', 1],
    ['01', 1],
    ['abc', 1],
    ['', 1],
    ['1', 1],
    ['100', 100],
    ['42', 42],
  ])('reads page %j as %i', (raw, page) => {
    expect(parseShowcaseQuery({ page: raw }).page).toBe(page);
  });

  it('round-trips through the route query and the API search', () => {
    const query = parseShowcaseQuery({
      role: 'backend',
      lang: 'vi',
      template: 'custom',
      page: '3',
    });
    expect(parseShowcaseQuery(showcaseRouteQuery(query) as never))
      .toEqual(query);
    expect(showcaseSearch(query))
      .toBe('role=backend&lang=vi&template=custom&page=3');
  });

  it('omits page 1 and absent filters from a link', () => {
    expect(showcaseRouteQuery({ page: 1 })).toEqual({});
    expect(showcaseRouteQuery({ role: 'qa', page: 1 })).toEqual({ role: 'qa' });
    expect(showcaseRouteQuery({ page: 2 })).toEqual({ page: '2' });
  });

  it('never carries an unrelated key into a link or request', () => {
    const query = parseShowcaseQuery({
      role: 'qa',
      utm_source: 'mail',
      ref: 'x',
    });
    expect(showcaseSearch(query)).toBe('role=qa');
    expect(Object.keys(showcaseRouteQuery(query))).toEqual(['role']);
  });
});

describe('showcase contract', () => {
  it('lists the Library roles in chip order, then Other', () => {
    expect([...SHOWCASE_ROLES]).toEqual([...ROLES, 'other']);
    expect(isShowcaseRole('other')).toBe(true);
    expect(isShowcaseRole('wizard')).toBe(false);
    expect(isShowcaseRole(null)).toBe(false);
  });

  it('builds the versioned card URL on the same origin', () => {
    expect(showcaseCardUrl({
      slug: 'ada-lovelace',
      cardVersion: '0123456789abcdef',
      imageText: 'x',
      language: 'en',
      templateId: null,
      role: null,
    })).toBe('/api/v1/public/resumes/ada-lovelace/og/0123456789abcdef.png');
  });

  const item = {
    slug: 'ada-lovelace',
    cardVersion: '0123456789abcdef',
    imageText: 'Ada Lovelace, Engineer',
    language: 'en',
    templateId: null,
    role: null,
  };

  it('keeps only the closed item fields', () => {
    const listing = parseShowcaseListing({
      items: [{ ...item, email: 'ada@example.com', phone: '+84 1' }],
      page: 1,
      pageCount: 1,
      total: 1,
    });
    expect(Object.keys(listing.items[0]!).sort()).toEqual([
      'cardVersion',
      'imageText',
      'language',
      'role',
      'slug',
      'templateId',
    ]);
  });

  it('shows no chip for a role this build does not know', () => {
    const listing = parseShowcaseListing({
      items: [{ ...item, role: 'astronaut' }],
      page: 1,
      pageCount: 1,
      total: 1,
    });
    expect(listing.items[0]!.role).toBeNull();
  });

  it.each([
    ['no items', { page: 1, pageCount: 1, total: 0 }],
    ['a string page', { items: [], page: '1', pageCount: 1, total: 0 }],
    ['a missing total', { items: [], page: 1, pageCount: 1 }],
    ['an unknown language', {
      items: [{ ...item, language: 'fr' }], page: 1, pageCount: 1, total: 1,
    }],
    ['a numeric role', {
      items: [{ ...item, role: 3 }], page: 1, pageCount: 1, total: 1,
    }],
    ['a missing slug', {
      items: [{ ...item, slug: undefined }], page: 1, pageCount: 1, total: 1,
    }],
    ['more than twelve items', {
      items: Array.from({ length: 13 }, () => item),
      page: 1,
      pageCount: 2,
      total: 13,
    }],
    ['a non-object', 'nope'],
  ])('rejects a listing with %s', (_name, value) => {
    expect(() => parseShowcaseListing(value)).toThrow();
  });

  it('reads the owner resource field, null when absent or unknown', () => {
    expect(parseOwnerShowcase({ state: 'listed', role: 'qa' }))
      .toEqual({ state: 'listed', role: 'qa' });
    expect(parseOwnerShowcase({ state: 'listed' }))
      .toEqual({ state: 'listed', role: null });
    expect(parseOwnerShowcase({ state: 'pending', role: null })).toBeNull();
    expect(parseOwnerShowcase({ state: 'declined' })).toBeNull();
    expect(parseOwnerShowcase(null)).toBeNull();
    expect(parseOwnerShowcase(undefined)).toBeNull();
    expect(parseOwnerShowcase({ state: 'off', role: null })).toBeNull();
  });
});
