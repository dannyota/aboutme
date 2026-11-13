import { describe, expect, it } from 'vitest';
import { SHOWCASE } from '../app/landing/showcase';
import { galleryTemplate, matchesFilter } from '../app/templates/catalog';

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
