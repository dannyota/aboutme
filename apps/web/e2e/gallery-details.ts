import { expect, type Page } from '@playwright/test';
import { TEMPLATES } from '@aboutme/schema/templates';
import type { Resume } from '@aboutme/schema';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import { expectStyleRules, type StyleRule } from './screen-expectations';

export const GALLERY_PAGE_TYPE: readonly StyleRule[] = [
  {
    selector: '.resume-document .section-heading h2',
    message: 'Gallery preview heading typography',
    css: { 'font-size': '16.5px', 'font-weight': '700',
      'line-height': '19.8px', 'letter-spacing': '0.99px',
      'text-transform': 'uppercase' },
  },
  {
    selector: '.resume-document .entry-title',
    message: 'Gallery preview entry typography',
    css: { 'font-size': '15px', 'font-weight': '700',
      'line-height': '22.5px', 'letter-spacing': 'normal' },
  },
  {
    selector: '.resume-document .entry-body p, .resume-document .entry-body li',
    message: 'Gallery preview body typography',
    css: { 'font-size': '15px', 'font-weight': '400',
      'line-height': '22.5px', 'letter-spacing': 'normal',
      'text-align': 'left' },
  },
];

export const GALLERY_ATS_TYPE: readonly StyleRule[] = [{
  selector: '[data-ats-text]', message: 'Gallery ATS typography',
  css: { 'font-size': '14px', 'font-weight': '400',
    'line-height': '24px', 'letter-spacing': 'normal' },
}];

export async function expectGalleryOrder(
  page: Page, locale: 'vi' | 'en',
): Promise<void> {
  const fixture = JSON.parse(await readFile(resolve(import.meta.dirname,
    `../../../packages/schema/samples/creative-accent.${locale}.json`),
  'utf8')) as Resume;
  const sections = fixture.customization.layout.sections!.main
    .map((key) => fixture.content[key]!);
  await expect(page.locator('.resume-document .section-heading h2'),
    'Gallery preview section order.').toHaveText(sections.map((section) =>
    section.displayName));
  const titles = sections.flatMap((section) => section.entries.flatMap(
    (entry) => entry.isHidden
      ? []
      : 'jobTitle' in entry
        ? [entry.jobTitle]
        : 'degree' in entry
          ? [entry.degree]
          : 'name' in entry ? [entry.name] : [],
  ));
  await expect(page.locator('.resume-document .entry-title'),
    'Gallery preview entry order.').toHaveText(titles);
}

export async function expectGalleryDetails(
  page: Page, locale: 'vi' | 'en', tab: 'page' | 'pdf' | 'ats',
): Promise<void> {
  if (tab === 'page') {
    await expectGalleryOrder(page, locale);
    await expectStyleRules(page, GALLERY_PAGE_TYPE);
  } else if (tab === 'ats') {
    await expectStyleRules(page, GALLERY_ATS_TYPE);
  }
}

export async function expectLibraryStructure(
  page: Page, filtered: boolean,
): Promise<void> {
  const grid = page.locator('.gallery-grid');
  const message = 'Library accessible list.';
  const count = filtered ? 1 : 20;
  await expect(grid, message).toHaveRole('list');
  await expect(grid.getByRole('listitem'), message).toHaveCount(count);
  await expect(grid.getByRole('link'), message).toHaveCount(count);
  await expect(grid, message).toMatchAriaSnapshot('- list:\n'
    + Array.from({ length: count }, () =>
      '  - listitem:\n    - link').join('\n'));
  const ids = filtered
    ? ['one-page-tight']
    : [
        'one-page-tight', 'engineer-compact', 'creative-accent', 'nordic-muted',
        'elegant-serif-two', 'mono-print', 'minimal-air', 'international-lang',
        'consulting-formal', 'ats-plain', 'graduate-friendly', 'executive-band',
        'modern-sidebar', 'academic-dense', 'classic-serif', 'designer-tag',
        'editorial-wide', 'government-formal', 'high-contrast', 'startup-bold',
      ];
  const cards = grid.locator('[data-template]:visible');
  for (const [index, id] of ids.entries()) {
    const fixture = TEMPLATES.find((template) => template.id === id)!;
    const card = cards.nth(index);
    await expect(card, message).toHaveRole('link');
    await expect(card.locator(':scope > span > span:first-child'), message)
      .toHaveText(fixture.name);
    const escaped = fixture.name.replace(/[.*+?^${}()|[\]\\]/gu, '\\$&');
    await expect(card, message).toHaveAccessibleName(new RegExp(escaped, 'u'));
  }
}
