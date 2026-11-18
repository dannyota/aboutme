// @vitest-environment node

import type { Resume } from '@aboutme/schema';
import { TEMPLATES } from '@aboutme/schema/templates';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { createSSRApp, h } from 'vue';
import { renderToString } from 'vue/server-renderer';

import {
  DARK_GROUND_COLORS,
  withDarkGround,
} from '../../app/pages/_harness/dark-ground-fixture';
import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';

const lightness = (hex: string): number => {
  const [r, g, b] = [1, 3, 5].map((start) => {
    const value = Number.parseInt(hex.slice(start, start + 2), 16) / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  }) as [number, number, number];
  const l = Math.cbrt(0.4122214708 * r + 0.5363770262 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s;
};

const fixture = (): Resume => {
  const document = JSON.parse(readFileSync(
    resolve(process.cwd(), '../../packages/schema/fixtures/full.json'), 'utf8',
  )) as Resume;
  delete document.personalDetails.photo;
  return document;
};

describe('already-dark harness ground', () => {
  it('authors a background darker than OKLCH lightness 0.5', () => {
    expect(lightness(DARK_GROUND_COLORS.background)).toBeLessThan(0.5);
  });

  it('is not the ground of any preset', () => {
    for (const template of TEMPLATES) {
      expect(template.customization.colors.background.toLowerCase(),
        template.id).not.toBe(DARK_GROUND_COLORS.background);
    }
  });

  it('replaces the colors and drops any tinted surface', () => {
    const document = fixture();
    document.customization.colors.surface = '#eeeeee';
    withDarkGround(document);
    expect(document.customization.colors).toEqual(DARK_GROUND_COLORS);
  });

  it('keeps the authored ground as the paper in both schemes', async () => {
    for (const colorScheme of [undefined, 'dark'] as const) {
      const document = fixture();
      withDarkGround(document);
      const html = await renderToString(createSSRApp({
        render: () => h(ResumeDocument, {
          document,
          context: {
            lng: 'en',
            mode: 'sheets',
            ...(colorScheme === undefined ? {} : { colorScheme }),
          },
        }),
      }));
      expect(html, String(colorScheme)).toContain(
        `--paper-color-surface:${DARK_GROUND_COLORS.background};`,
      );
      if (colorScheme === 'dark') {
        expect(html).toContain(
          `--dark-color-surface:${DARK_GROUND_COLORS.background};`,
        );
      }
    }
  });
});
