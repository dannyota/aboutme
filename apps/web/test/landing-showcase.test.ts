import { describe, expect, it } from 'vitest';
import { SHOWCASE } from '../app/landing/showcase';
import { galleryTemplate, matchesFilter } from '../app/templates/catalog';

// The homepage template showcase pins four templates, one per filter chip
// (DESIGN.md; ADR 0050). Each must exist, carry a sample in both site
// languages, and actually belong to the filter it advertises.
describe('landing showcase', () => {
  it('names templates that exist in the gallery catalog', () => {
    for (const entry of SHOWCASE) {
      expect(galleryTemplate(entry.id)).toBeDefined();
    }
  });

  it('has a sample in both site languages for every entry', () => {
    for (const entry of SHOWCASE) {
      const template = galleryTemplate(entry.id)!;
      expect(template.sampleLanguages).toEqual(
        expect.arrayContaining(['en', 'vi']),
      );
    }
  });

  it('belongs to the filter it advertises', () => {
    for (const entry of SHOWCASE) {
      const template = galleryTemplate(entry.id)!;
      expect(matchesFilter(template, entry.filter)).toBe(true);
    }
  });

  it('has no duplicate template across the showcase', () => {
    const ids = SHOWCASE.map((entry) => entry.id);
    expect(new Set(ids).size).toBe(ids.length);
  });
});
