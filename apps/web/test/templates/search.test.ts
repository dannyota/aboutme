import { describe, expect, it } from 'vitest';

import { galleryTemplate, MEMBERS } from '../../app/templates/catalog';
import {
  matchesTemplateSearch,
  normalizeSearchText,
  templateSearchText,
  type TemplateSearchSubject,
} from '../../app/templates/search';

// The editor's template panel search box matches a template's name, its
// purpose, sample tags, and the labels of any role or Library filter chip it
// belongs to, in both site languages, case- and diacritic-insensitively, with
// every whitespace-separated query term required (DESIGN.md, editor
// Templates panel).

// role: backend
const onePageTight = galleryTemplate('one-page-tight')!;
// role: frontend
const engineerCompact = galleryTemplate('engineer-compact')!;
// role: devops
const nordicMuted = galleryTemplate('nordic-muted')!;
// role: fresher
const minimalAir = galleryTemplate('minimal-air')!;
// role: brse
const internationalLang = galleryTemplate('international-lang')!;
// role: security
const consultingFormal = galleryTemplate('consulting-formal')!;
// no role, no sample, but an ATS filter chip member
const classicSerif = galleryTemplate('classic-serif')!;
// no role, but a first-job filter chip member
const graduateFriendly = galleryTemplate('graduate-friendly')!;

describe('normalizeSearchText', () => {
  it('lowercases', () => {
    expect(normalizeSearchText('BACKEND')).toBe('backend');
  });

  it('strips Vietnamese diacritics', () => {
    expect(normalizeSearchText('Kỹ sư')).toBe('ky su');
  });

  it('maps đ and Đ to d, including where NFD leaves it untouched', () => {
    expect(normalizeSearchText('Đà Nẵng')).toBe('da nang');
  });

  it('trims surrounding whitespace', () => {
    expect(normalizeSearchText('  Ábc  ')).toBe('abc');
  });
});

describe('templateSearchText', () => {
  it('includes the name and both languages of the purpose', () => {
    const text = templateSearchText(classicSerif);
    expect(text).toContain('classic serif');
    expect(text).toContain('traditional and formal');
    expect(text).toContain('trang trong, truyen thong');
  });

  it('includes sample tags and role labels, in both languages', () => {
    const text = templateSearchText(nordicMuted);
    expect(text).toContain('senior devops engineer');
    expect(text).toContain('devops/sre');
  });

  it('never includes a role label for a role the template is not the '
    + 'sample for', () => {
    expect(templateSearchText(engineerCompact)).not.toContain('backend');
  });

  it('includes the Library filter chip labels a template belongs to, in '
    + 'both languages', () => {
    const text = templateSearchText(classicSerif);
    expect(text).toContain('ats-friendly');
    expect(text).toContain('chuan ats');
  });

  it('never includes a filter chip label for a filter the template is not '
    + 'a member of', () => {
    expect(templateSearchText(classicSerif)).not.toContain('one page');
  });

  it('handles a template with no sample tags', () => {
    const subject: TemplateSearchSubject = {
      id: 'fixture',
      name: 'Fixture Template',
      purpose: { en: 'A fixture', vi: 'Một mẫu thử' },
    };
    expect(templateSearchText(subject)).toBe(
      'fixture template a fixture mot mau thu',
    );
  });
});

describe('matchesTemplateSearch', () => {
  it('matches every template on an empty or blank query', () => {
    expect(matchesTemplateSearch(classicSerif, '')).toBe(true);
    expect(matchesTemplateSearch(classicSerif, '   ')).toBe(true);
  });

  it('matches the name case-insensitively', () => {
    expect(matchesTemplateSearch(onePageTight, 'ONE-PAGE')).toBe(true);
  });

  it('matches purpose text regardless of diacritics', () => {
    // "kỹ năng" (skill) appears only in the Vietnamese purpose text.
    expect(matchesTemplateSearch(engineerCompact, 'ky nang')).toBe(true);
  });

  it('requires every whitespace-separated term, across different fields',
    () => {
      expect(matchesTemplateSearch(nordicMuted, 'Nordic DevOps')).toBe(true);
      expect(matchesTemplateSearch(nordicMuted, 'nordic security')).toBe(
        false,
      );
    });

  it('matches a role chip label named in the current UI language', () => {
    expect(matchesTemplateSearch(onePageTight, 'Backend')).toBe(true);
    expect(matchesTemplateSearch(nordicMuted, 'DevOps')).toBe(true);
    expect(matchesTemplateSearch(minimalAir, 'Fresher')).toBe(true);
    expect(matchesTemplateSearch(internationalLang, 'BrSE')).toBe(true);
  });

  it('does not match a role chip the template is not the sample for', () => {
    expect(matchesTemplateSearch(engineerCompact, 'Backend')).toBe(false);
  });

  it('matches a role chip label named in the other site language, since '
    + 'the two languages disagree for this role', () => {
    expect(matchesTemplateSearch(consultingFormal, 'security')).toBe(true);
    expect(matchesTemplateSearch(consultingFormal, 'bảo mật')).toBe(true);
  });

  it('matches purpose text in either site language', () => {
    expect(matchesTemplateSearch(classicSerif, 'banking')).toBe(true);
    expect(matchesTemplateSearch(classicSerif, 'ngan hang')).toBe(true);
  });

  it('returns every ATS-friendly template for an "ats" query', () => {
    for (const id of MEMBERS.ats) {
      expect(matchesTemplateSearch(galleryTemplate(id)!, 'ats')).toBe(true);
    }
  });

  it('matches a filter chip label naming a template unreachable by its own '
    + 'name or purpose', () => {
    expect(matchesTemplateSearch(graduateFriendly, 'first job')).toBe(true);
  });

  it('does not match a filter chip label for a filter the template is not '
    + 'a member of', () => {
    expect(matchesTemplateSearch(classicSerif, 'one-page')).toBe(false);
  });

  it('matches a Vietnamese filter chip label without diacritics, in the '
    + 'style of a short, informal query', () => {
    // "Kỹ thuật" (technical) names the filter chip; a job seeker types it
    // without diacritics as "ky thuat".
    expect(matchesTemplateSearch(engineerCompact, 'ky thuat')).toBe(true);
  });
});
