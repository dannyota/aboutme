import { expect } from '@playwright/test';
import type { Resume } from '@aboutme/schema';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import { measurePdfLayout } from './display-assertions';
import {
  expectedEntryIds, pdfEntryIds, printedDisplayPages, printedTextPages,
  PRINT_CASES,
} from './pdf-display';
import {
  assertDomInputContent, assertPdfInputOccurrences, overflowPdfChunks,
  expectedInputOccurrences, pdfInputOccurrences, samplePdfChunks,
  samplePdfContentMatches,
} from './pdf-input-content';
import { assertOverflowTextStyle } from './pdf-text-style';
import { applyStyleMutation, mutationTest } from './style-mutation';
import { denyExternalRequests, waitForImages } from './support';

export function sampleInputMutationTests(): void {
  for (const mutation of ['accents', 'replacement', 'reorder',
    'hide', 'duplicate'] as const) {
    mutationTest(`sample PDF input expectations detect ${mutation}`, async ({
      page,
    }) => {
      const external = await denyExternalRequests(page);
      await page.emulateMedia({ media: 'print' });
      const sample: Resume = JSON.parse(await readFile(resolve(
        import.meta.dirname,
        '../../../packages/schema/samples/modern-sidebar.vi.json',
      ), 'utf8'));
      const margin = sample.customization.spacing.pageMargin!;
      const margins = { x: margin.x * 72 / 25.4, y: margin.y * 72 / 25.4 };
      await page.setViewportSize({
        width: Math.round((210 - 2 * margin.x) * 96 / 25.4), height: 1123,
      });
      const url = '/_harness/render?fixture=sample-modern-sidebar-vi'
        + '&template=modern-sidebar&mode=continuous&print=1';
      expect((await page.goto(url))?.ok()).toBe(true);
      await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
      await waitForImages(page);
      const layout = await measurePdfLayout(page, margins);
      const expected = samplePdfChunks(sample, 'vi');
      const baseline = await printedTextPages(page);
      expect(baseline).toHaveLength(1);
      const check = (parts: typeof baseline) => {
        if (mutation === 'duplicate') {
          assertPdfInputOccurrences(parts, expected, layout);
        } else {
          expect(samplePdfContentMatches(parts, expected, layout),
            'PDF sample input content, occurrences and column order.')
            .toBe(true);
        }
      };
      expect(samplePdfContentMatches(baseline, expected, layout),
        'PDF sample input content, occurrences and column order.').toBe(true);
      check(baseline);
      const section = page.locator('.resume-main .resume-section').nth(1);
      await expect(section.locator('.entry')).toHaveCount(3);
      if (mutation === 'hide') {
        await applyStyleMutation(section.locator('.entry').first(), {
          display: 'none',
        });
      }
      await section.evaluate((element, change) => {
        const first = element.querySelector('.entry')!;
        if (change === 'reorder') {
          first.before(first.nextElementSibling!);
        } else if (change === 'duplicate') {
          first.after(first.cloneNode(true));
        } else if (change !== 'hide') {
          const body = first.querySelector('.entry-body')!;
          if (change === 'replacement') {
            body.textContent = 'Nội dung thay thế.';
          } else {
            const walker = document.createTreeWalker(
              body, NodeFilter.SHOW_TEXT,
            );
            while (walker.nextNode()) {
              const node = walker.currentNode;
              node.textContent = node.textContent!.normalize('NFD')
                .replace(/\p{M}/gu, '').replace(/đ/gu, 'd').replace(/Đ/gu, 'D');
            }
          }
        }
      }, mutation);
      const altered = await printedTextPages(page);
      expect(altered).not.toEqual(baseline);
      if (mutation === 'duplicate') {
        const before = expectedInputOccurrences(expected);
        const after = pdfInputOccurrences(altered, expected, layout);
        expect(after.every((count, index) => count >= before[index]!))
          .toBe(true);
        const work = sample.content.work!;
        expect(work.sectionType).toBe('work');
        const title = work.sectionType === 'work'
          ? work.entries.find((entry) => !entry.isHidden)!.jobTitle
          : '';
        const index = expected.findIndex(({ text }) => text === title);
        expect(index).toBeGreaterThanOrEqual(0);
        expect(after[index]).toBe(before[index]! + 1);
        expect(() => check(altered)).toThrow('PDF input text occurrences.');
      } else {
        expect(() => check(altered))
          .toThrow('PDF sample input content, occurrences and column order.');
      }
      expect(samplePdfContentMatches(altered, expected, layout),
        'PDF sample input content, occurrences and column order.').toBe(false);
      if (mutation === 'duplicate') {
        expect(altered.length).toBeGreaterThan(0);
      } else {
        expect(altered).toHaveLength(1);
      }
      expect(external).toEqual([]);
    });
  }
}

export function overflowInputMutationTests(): void {
  for (const printCase of PRINT_CASES) {
    for (const field of ['name', 'contact'] as const) {
      mutationTest(
        `${printCase.fixture} input content detects changed ${field}`,
        async ({ page }) => {
          const external = await denyExternalRequests(page);
          await page.emulateMedia({ media: 'print' });
          await page.setViewportSize({ width: 688, height: 1123 });
          const url = `/_harness/render?fixture=${printCase.fixture}`
            + '&template=modern-sidebar&mode=continuous&lng=vi';
          expect((await page.goto(url))?.ok()).toBe(true);
          await expect(page.locator('[data-fonts-ready="true"]'))
            .toHaveCount(1);
          await waitForImages(page);
          const margins = { x: 14 * 72 / 25.4, y: 13 * 72 / 25.4 };
          const expected = await overflowPdfChunks(printCase, 'vi');
          const layout = await measurePdfLayout(page, margins);
          const baseline = await printedDisplayPages(page);
          const text = baseline.map((part) => part.text);
          assertDomInputContent(layout, expected);
          assertPdfInputOccurrences(text, expected, layout);
          assertOverflowTextStyle(layout, {
            fills: [], images: [],
            text: baseline.flatMap(({ paint }) => paint.text),
          }, expected);
          const target = field === 'name'
            ? page.locator('.resume-name')
            : page.locator('.resume-header a[href^="mailto:"]');
          const replacement = field === 'name'
            ? 'Trần Thị Minh Châu'
            : 'changed@example.com';
          await target.evaluate((element, value) => {
            element.textContent = value;
          }, replacement);
          await expect(target).toHaveText(replacement);
          const altered = await printedDisplayPages(page);
          const alteredText = altered.map((part) => part.text);
          expect(alteredText.map((part) => part.text).join(' '))
            .toContain(replacement);
          const changedLayout = await measurePdfLayout(page, margins);
          expect(() => assertDomInputContent(changedLayout, expected))
            .toThrow('Print DOM text and occurrences from input.');
          expect(() => assertPdfInputOccurrences(alteredText, expected, layout))
            .toThrow('PDF input text occurrences.');
          const flow = printCase.prefix === 'MAIN-ENTRY-' ? 'MAIN' : 'SIDEBAR';
          for (const column of ['MAIN', 'SIDEBAR'] as const) {
            expect(pdfEntryIds(alteredText, column))
              .toEqual(column === flow ? expectedEntryIds(printCase) : []);
          }
          expect(external).toEqual([]);
        });
    }
  }
}
