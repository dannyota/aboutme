import type { Resume } from '@aboutme/schema';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

import {
  resolveRenderModel,
} from '../../app/components/resume/resolveRenderModel';

// docs/design/public-page-theme.md, "How it reaches the page".
const full = (): Resume =>
  JSON.parse(
    readFileSync('../../packages/schema/fixtures/full.json', 'utf8'),
  ) as Resume;

const PHOTO = 'data:image/png;base64,AA==';

const darkKeys = (scope: Record<string, string> | undefined): string[] =>
  Object.keys(scope ?? {}).filter((key) => key.startsWith('--dark-color-'));

describe('render context color scheme', () => {
  it('adds no dark role without a scheme', () => {
    const model = resolveRenderModel(full(), {
      lng: 'en',
      mode: 'continuous',
      photoUrl: PHOTO,
    });
    expect(darkKeys(model.styles.root)).toEqual([]);
    expect(darkKeys(model.styles.sidebar)).toEqual([]);
  });

  it.each(['dark', 'system'] as const)(
    'adds dark roles at the article and the tinted sidebar for %s',
    (colorScheme) => {
      const model = resolveRenderModel(full(), {
        lng: 'en',
        mode: 'continuous',
        photoUrl: PHOTO,
        colorScheme,
      });
      expect(darkKeys(model.styles.root).length).toBeGreaterThan(0);
      // full.json tints its sidebar.
      expect(darkKeys(model.styles.sidebar).length).toBeGreaterThan(0);
    },
  );

  it('ignores the scheme in paged mode', () => {
    const model = resolveRenderModel(full(), {
      lng: 'en',
      mode: 'paged',
      photoUrl: PHOTO,
      colorScheme: 'dark',
    });
    expect(darkKeys(model.styles.root)).toEqual([]);
    expect(darkKeys(model.styles.sidebar)).toEqual([]);
  });

  it('leaves the light roles identical in every mode', () => {
    const light = resolveRenderModel(full(), {
      lng: 'en',
      mode: 'continuous',
      photoUrl: PHOTO,
    });
    const dark = resolveRenderModel(full(), {
      lng: 'en',
      mode: 'continuous',
      photoUrl: PHOTO,
      colorScheme: 'dark',
    });
    for (const [name, value] of Object.entries(light.styles.root)) {
      expect(dark.styles.root[name], name).toBe(value);
    }
    expect(dark.styles.page).toEqual(light.styles.page);
  });
});
