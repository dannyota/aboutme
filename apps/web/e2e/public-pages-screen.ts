import { expect, type Page } from '@playwright/test';

import { resumeContract, type ScreenContract } from './display-assertions';
import { SCROLLER } from './public-pages';

export const PUBLIC_PAGE_ROOTS = '.public-page .resume-page';
export const PHONE_POSITION = 'Public phone position.';

export function pagesContract(
  template: string, language: 'vi' | 'en',
): ScreenContract {
  const base = resumeContract(template, language);
  const root = `:nth-match(${PUBLIC_PAGE_ROOTS}, 1)`;
  const name = language === 'vi' ? 'Nguyễn Thị Minh Châu' : 'Ada Lovelace';
  return {
    ...base,
    root,
    text: `${PUBLIC_PAGE_ROOTS} :is(.resume-name,.resume-headline,`
      + '.entry-title,.entry-subtitle,.entry-meta > span:not(.level-widget),'
      + '.level-tag,.entry-body p,.entry-body li,.section-heading h2)',
    flow: `${PUBLIC_PAGE_ROOTS} .pagination-atomic`,
    constrained: PUBLIC_PAGE_ROOTS,
    type: [root, base.type[1], base.type[2]],
    paint: [root, base.paint[1], base.paint[2]],
    copy: [`${PUBLIC_PAGE_ROOTS} .resume-name`, name],
    aria: [`${PUBLIC_PAGE_ROOTS} .resume-name`,
      `- heading "${name}" [level=1]`],
    image: `${PUBLIC_PAGE_ROOTS} .resume-photo img`,
    scrolling: [SCROLLER],
  };
}

export async function expectPhonePosition(page: Page): Promise<void> {
  const measured = await page.evaluate((selector) => {
    const box = (element: Element) => {
      const rect = element.getBoundingClientRect();
      return {
        left: rect.left, right: rect.right, top: rect.top,
        bottom: rect.bottom, width: rect.width,
      };
    };
    const scroller = document.querySelector(selector)!;
    const sections = [...scroller.querySelectorAll(':scope > .public-page')];
    const index = Number(document.querySelector('.public-pages')!
      .getAttribute('data-page-index'));
    return {
      viewport: document.documentElement.clientWidth,
      index,
      count: sections.length,
      sheet: box(sections[index]!.querySelector('.public-page-sheet')!),
      status: box(document.querySelector('.public-pager-status')!),
      text: document.querySelector('.public-pager-position')!
        .textContent?.trim() ?? '',
      onSheets: sections.map((section) => getComputedStyle(
        section.querySelector('.public-page-position')!).display),
    };
  }, SCROLLER);
  const { status } = measured;
  expect({
    hiddenOnSheets: measured.onSheets.every((display) => display === 'none'),
    text: measured.text,
    belowSheet: status.top >= measured.sheet.bottom - 1,
    centered: Math.abs(status.left + status.width / 2
      - measured.viewport / 2) <= 2,
    inside: status.width > 0 && status.left >= -1
      && status.right <= measured.viewport + 1,
  }, PHONE_POSITION).toEqual({
    hiddenOnSheets: true,
    text: `${measured.index + 1} / ${measured.count}`,
    belowSheet: true,
    centered: true,
    inside: true,
  });
}
