import { existsSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import {
  EDITOR_PREVIEW_TEMPLATE,
  SHOWCASE,
} from '../app/landing/showcase';
import { galleryTemplate, matchesFilter } from '../app/templates/catalog';
import { samplePageImage } from '../app/templates/pageImages';

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

  it('has a shipped first page in both languages for the fan and editor',
    () => {
      for (const id of [
        ...SHOWCASE.map((entry) => entry.id), EDITOR_PREVIEW_TEMPLATE,
      ]) {
        for (const lng of ['vi', 'en'] as const) {
          const image = samplePageImage(id, lng);
          expect(image?.src, `${id} ${lng}`)
            .toBe(`/templates/pages/${id}-${lng}-p1.png`);
          expect(existsSync(`public${image!.src}`), `${id} ${lng}`)
            .toBe(true);
        }
      }
    });
});
