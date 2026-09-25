import type { Resume } from '@aboutme/schema';
import { loadSample, SAMPLES } from '@aboutme/schema/samples';
import { TEMPLATES } from '@aboutme/schema/templates';
import { describe, expect, it } from 'vitest';

import { isLocalizedPath } from '../../app/i18n/locale';
import { isIndexablePath } from '../../app/i18n/meta';
import { galleryCopy } from '../../app/i18n/templates';
import { atsText, richText } from '../../app/templates/atsText';
import {
  FILTERS,
  filterMembers,
  GALLERY,
  galleryTemplate,
  matchesFilter,
  matchesRole,
  parseFilter,
  parseRole,
  ROLE_MEMBERS,
  ROLES,
  roleMembers,
  withRole,
} from '../../app/templates/catalog';
import { galleryDocument } from '../../app/templates/documents';
import {
  galleryStructuredData,
  templateStructuredData,
} from '../../app/templates/structuredData';

// The template gallery's catalog, filters, documents, and search data
// (DESIGN.md, template gallery).

const ids = TEMPLATES.map((template) => template.id);

describe('gallery catalog', () => {
  it('lists all 20 templates, tech role samples first, then the rest', () => {
    expect(GALLERY.map(({ id }) => id).sort()).toEqual([...ids].sort());
    expect(GALLERY.slice(0, 13).map(({ id }) => id)).toEqual([
      'one-page-tight',
      'engineer-compact',
      'creative-accent',
      'nordic-muted',
      'elegant-serif-two',
      'mono-print',
      'minimal-air',
      'international-lang',
      'consulting-formal',
      'ats-plain',
      'graduate-friendly',
      'executive-band',
      'modern-sidebar',
    ]);
    const rest = GALLERY.slice(13).map(({ name }) => name);
    expect(rest).toEqual([...rest].sort((a, b) => a.localeCompare(b, 'en')));
  });

  it('gives each tech role exactly one real template with a sample', () => {
    const members = roleMembers();
    expect(members).toBe(ROLE_MEMBERS);
    const flat = ROLES.flatMap((role) => members[role]);
    expect(flat).toHaveLength(ROLES.length);
    expect(new Set(flat).size).toBe(ROLES.length);
    expect(flat).toEqual(GALLERY.slice(0, ROLES.length).map(({ id }) => id));
    for (const role of ROLES) {
      const [id] = members[role];
      const template = galleryTemplate(id!);
      expect(template, role).toBeDefined();
      expect(template?.sampleLanguages.length, role).toBeGreaterThan(0);
    }
  });

  it('names only real templates in every filter', () => {
    for (const members of Object.values(filterMembers())) {
      expect(members.filter((id) => !ids.includes(id))).toEqual([]);
      expect(new Set(members).size).toBe(members.length);
    }
    expect(filterMembers().photo).toHaveLength(14);
  });

  it('gives each template copy, and each sample a tag and page count', () => {
    for (const template of GALLERY) {
      expect(template.purpose.vi, template.id).not.toBe('');
      expect(template.purpose.en, template.id).not.toBe('');
      for (const lng of template.sampleLanguages) {
        const tags = template.sampleTags?.[lng];
        expect(tags?.vi, template.id).toMatch(/^CV mẫu: /u);
        expect(tags?.en, template.id).toMatch(/^Sample: /u);
      }
      expect(template.samplePages === undefined)
        .toBe(template.sampleLanguages.length === 0);
    }
    expect(new Set(SAMPLES.map(({ templateId }) => templateId)))
      .toEqual(new Set(GALLERY.filter((template) =>
        template.sampleLanguages.length > 0).map(({ id }) => id)));
  });

  it('filters by one chip at a time from the query', () => {
    expect(parseFilter('ats')).toBe('ats');
    expect(parseFilter('nope')).toBeUndefined();
    expect(parseFilter(['ats'])).toBeUndefined();
    expect(parseFilter(undefined)).toBeUndefined();
    expect(GALLERY.filter((template) => matchesFilter(template, 'sample')))
      .toHaveLength(13);
    for (const filter of FILTERS) {
      expect(galleryCopy.vi.filters[filter]).not.toBe('');
      expect(galleryCopy.en.filters[filter]).not.toBe('');
    }
    expect(galleryTemplate('constructor')).toBeUndefined();
  });

  it('filters by one tech role chip at a time from the query', () => {
    for (const role of ROLES) {
      expect(parseRole(role)).toBe(role);
    }
    expect(parseRole('nope')).toBeUndefined();
    expect(parseRole(['backend'])).toBeUndefined();
    expect(parseRole(undefined)).toBeUndefined();

    const backendMatches = GALLERY.filter((template) =>
      matchesRole(template, 'backend'));
    expect(backendMatches.map(({ id }) => id)).toEqual(['one-page-tight']);

    for (const role of ROLES) {
      expect(galleryCopy.vi.roles[role]).not.toBe('');
      expect(galleryCopy.en.roles[role]).not.toBe('');
    }
    expect(galleryCopy.vi.allRoles).not.toBe('');
    expect(galleryCopy.en.allRoles).not.toBe('');
    expect(galleryCopy.vi.rolesLabel).not.toBe('');
    expect(galleryCopy.en.rolesLabel).not.toBe('');
  });

  it('keeps the filter and sets or clears the role in the query', () => {
    const original = { filter: 'ats' };
    expect(withRole(original, 'backend')).toEqual({
      filter: 'ats',
      role: 'backend',
    });
    expect(withRole({ filter: 'ats', role: 'backend' }, undefined))
      .toEqual({ filter: 'ats' });
    expect(withRole({}, 'frontend')).toEqual({ role: 'frontend' });
    expect(original).toEqual({ filter: 'ats' });
  });

  it('serves the gallery pages in the site language and to search', () => {
    for (const path of ['/templates', '/templates/ats-plain']) {
      expect(isLocalizedPath(path)).toBe(true);
      expect(isIndexablePath(path)).toBe(true);
    }
    for (const path of ['/templates/../app', '/templates/a/b', '/app/new']) {
      expect(isIndexablePath(path)).toBe(false);
    }
  });
});

describe('gallery documents', () => {
  it('shows a template its own sample, else the filler in its style',
    async () => {
      const sample = await galleryDocument(galleryTemplate('ats-plain')!, 'vi');
      expect(sample.isSample).toBe(true);
      expect(sample.document).toEqual(await loadSample('ats-plain', 'vi'));

      const classic = galleryTemplate('classic-serif')!;
      const filler = await galleryDocument(classic, 'en');
      expect(filler.isSample).toBe(false);
      expect(filler.document.customization.font)
        .toEqual(classic.preset.customization.font);
    });
});

describe('what an ATS reads', () => {
  it('reads the header, then the main column, then the sidebar', async () => {
    const document = (await loadSample('engineer-compact', 'en'))!;
    const text = atsText(document, 'en');
    const lines = text.split('\n');
    expect(lines[0]).toBe(document.personalDetails.fullName);
    const { main, sidebar } = document.customization.layout.sections;
    const heading = (key: string) =>
      lines.indexOf(document.content[key]!.displayName!.toUpperCase());
    expect(heading(main[0]!)).toBeGreaterThan(0);
    expect(heading(main.at(-1)!)).toBeLessThan(heading(sidebar[0]!));
    expect(text).not.toMatch(/<[a-z/]/u);
  });

  it('turns sanitized rich text into plain lines', () => {
    expect(richText('<p>One &amp; two</p><ul><li>A&#39;s</li><li>B</li></ul>'))
      .toBe('One & two\n- A\'s\n- B');
    expect(richText(undefined)).toBeUndefined();
  });

  it('leaves hidden entries and details out', () => {
    const document = {
      personalDetails: {
        fullName: 'Ada',
        details: [
          {
            id: '1',
            type: 'email',
            value: 'hidden@example.com',
            isHidden: true,
          },
        ],
      },
      customization: {
        dateFormat: 'YYYY',
        layout: { sections: { main: ['work'], sidebar: [] } },
      },
      content: {
        work: {
          sectionType: 'work',
          displayName: 'Work',
          entries: [{ id: '2', isHidden: true, jobTitle: 'Secret' }],
        },
      },
    } as unknown as Resume;
    expect(atsText(document, 'en')).toBe('Ada');
  });
});

describe('gallery search data', () => {
  it('lists every template page and escapes markup', () => {
    const data = JSON.parse(galleryStructuredData('T', 'D', GALLERY));
    expect(data['@type']).toBe('CollectionPage');
    expect(data.mainEntity.itemListElement).toHaveLength(20);
    expect(data.mainEntity.itemListElement[0].url)
      .toBe('https://aboutme.vn/templates/one-page-tight');
    const detail = templateStructuredData(
      galleryTemplate('ats-plain')!,
      '</script><script>',
      'vi',
    );
    expect(detail).not.toContain('</script>');
    expect(JSON.parse(detail).description).toBe('</script><script>');
  });
});
