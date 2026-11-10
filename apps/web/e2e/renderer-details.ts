import { expect, type Page } from '@playwright/test';

import { RESUME_STYLES } from './display-assertions';
import type { StyleRule } from './screen-expectations';

const PAINT: Record<string, readonly [string, string]> = {
  'classic-serif': ['rgb(18, 21, 26)', 'rgb(255, 255, 255)'],
  'engineer-compact': ['rgb(16, 57, 77)', 'rgb(255, 255, 255)'],
  'modern-sidebar': ['rgb(14, 63, 82)', 'rgb(227, 236, 240)'],
  'executive-band': ['rgb(22, 39, 61)', 'rgb(22, 39, 61)'],
  'consulting-formal': ['rgb(20, 48, 79)', 'rgb(255, 255, 255)'],
  'academic-dense': ['rgb(18, 41, 63)', 'rgb(255, 255, 255)'],
  'editorial-wide': ['rgb(58, 44, 30)', 'rgb(250, 248, 243)'],
  'creative-accent': ['rgb(194, 58, 20)', 'rgb(255, 240, 235)'],
};

export function rendererTypeRules(template: string): readonly StyleRule[] {
  const base = RESUME_STYLES[template as keyof typeof RESUME_STYLES][2];
  const upper = !['consulting-formal', 'editorial-wide'].includes(template);
  return [{
    selector: '.resume-document:not(.pagination-measurement) '
      + '.section-heading h2',
    message: 'Renderer heading spacing',
    css: { 'letter-spacing': `${upper ? base * 1.1 * 0.06 : 0}px` },
  }];
}

export async function expectRendererPaint(
  page: Page, template: string,
): Promise<void> {
  const [ink, surface] = PAINT[template]!;
  const roots = page.locator('.resume-document:not(.pagination-measurement)');
  for (const heading of await roots.locator('.section-heading').all()) {
    await expect(heading, 'Renderer heading paint.').toHaveCSS('color', ink);
  }
  for (const heading of await roots.locator('.section-heading h2').all()) {
    await expect(heading, 'Renderer heading paint.').toHaveCSS('color', ink);
  }
  const region = template === 'modern-sidebar'
    ? '.resume-sidebar'
    : '.resume-header';
  for (const node of await roots.locator(region).all()) {
    await expect(node, 'Renderer region paint.')
      .toHaveCSS('background-color', surface);
  }
  const headerInk = template === 'executive-band'
    ? 'rgb(255, 255, 255)'
    : ink;
  await expect(roots.locator('.resume-name'), 'Renderer name paint.')
    .toHaveCSS('color', headerInk);
}
