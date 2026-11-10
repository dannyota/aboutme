import { expect, test } from '@playwright/test';

import { measurePdfLayout } from './display-assertions';
import { assertPhotoCrop, assertPrintDom } from './pdf-fonts';
import { assertOverflowTextStyle } from './pdf-text-style';
import { overflowPdfChunks } from './pdf-input-content';
import { sampleInputMutationTests, overflowInputMutationTests }
  from './pdf-input-mutations';
import { applyStyleMutation } from './style-mutation';
import {
  checkMembership,
  expectedEntryIds,
  EXPECTED_PAGES,
  pdfEntryIds,
  pdfColumnsMatch,
  pdfGlyphGeometry,
  pdfMembership,
  pdfSidebarPaint,
  pdfWidgetMatches,
  pdfWidgetDiagnostic,
  printedDisplayPages,
  printedTextPages,
  PRINT_CASES as CASES,
} from './pdf-display';
import {
  fixturePhotoIdentity, pdfImageDiagnostic, pdfImageMatches,
} from './pdf-paint';
import { denyExternalRequests, waitForImages } from './support';
import { assertPdfPageGeometry } from './sample-pdf-content';

sampleInputMutationTests();
overflowInputMutationTests();

test('PDF columns reject a main entry moved into the page-two sidebar',
  async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    await page.setViewportSize({
      width: Math.round((210 - 28) * 96 / 25.4), height: 1123,
    });
    expect((await page.goto('/_harness/render?fixture=print-main-overflow'
      + '&template=modern-sidebar&mode=continuous&lng=en&paper=a4'))?.ok())
      .toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    const margins = { x: 14 * 72 / 25.4, y: 13 * 72 / 25.4 };
    const layout = await measurePdfLayout(page, margins);
    const baseline = await printedTextPages(page);
    expect(baseline).toHaveLength(2);
    const check = (parts: typeof baseline) => {
      expect(pdfColumnsMatch(parts[1]!, layout, margins, false),
        'PDF column positions.').toBe(true);
    };
    check(baseline);
    const last = page.locator('.resume-main .entry').last();
    await expect(last).toContainText('MAIN-ENTRY-09');
    const shift = Math.round((layout.sidebar!.left - margins.x) / 0.75);
    await applyStyleMutation(last, {
      transform: [`translateX(${shift}px)`, `matrix(1, 0, 0, 1, ${shift}, 0)`],
      width: '100px',
    });
    const altered = await printedTextPages(page);
    expect(altered).not.toEqual(baseline);
    expect(() => check(altered)).toThrow('PDF column positions.');
    expect(altered).toHaveLength(2);
    expect(altered[1]!.text).toContain('MAIN-ENTRY-09');
    expect(pdfMembership(altered, CASES[1]!))
      .toEqual(pdfMembership(baseline, CASES[1]!));
    expect(pdfColumnsMatch(altered[1]!, layout, margins, false)).toBe(false);
    expect(external).toEqual([]);
  });

for (const mutation of ['margin', 'coincident text',
  'cross-column collision'] as const) {
  test(`PDF glyph bounds reject ${mutation}`, async ({ page }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    await page.setViewportSize({
      width: Math.round((210 - 28) * 96 / 25.4), height: 1123,
    });
    expect((await page.goto('/_harness/render?fixture=print-main-overflow'
      + '&template=modern-sidebar&mode=continuous&lng=en&paper=a4'))?.ok())
      .toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    const margins = { x: 14 * 72 / 25.4, y: 13 * 72 / 25.4 };
    const baseline = await printedTextPages(page);
    expect(baseline).toHaveLength(2);
    expect(pdfGlyphGeometry(baseline[0]!, margins))
      .toEqual({ contained: true, separated: true });
    const check = (parts: typeof baseline) => {
      const geometry = pdfGlyphGeometry(parts[0]!, margins);
      expect(mutation === 'margin' ? geometry.contained : geometry.separated,
        'PDF glyph bounds.').toBe(true);
    };
    check(baseline);
    const title = page.locator('.resume-main .entry-title').first();
    if (mutation === 'margin') {
      await applyStyleMutation(title, {
        transform: ['translateX(-8px)', 'matrix(1, 0, 0, 1, -8, 0)'],
      });
    } else if (mutation === 'coincident text') {
      await title.evaluate((element) => {
        const clone = element.cloneNode(true) as HTMLElement;
        clone.dataset.overlapProbe = 'true';
        element.append(clone);
      });
      await applyStyleMutation(title, { position: 'relative' });
      await applyStyleMutation(title.locator('[data-overlap-probe]'), {
        position: 'absolute', inset: '0px',
      });
    } else {
      const target = await title.boundingBox();
      expect(target).not.toBeNull();
      const sidebar = page.locator('.resume-sidebar .entry-title').first();
      const origin = await sidebar.boundingBox();
      expect(origin).not.toBeNull();
      const x = Math.round(target!.x - origin!.x);
      const y = Math.round(target!.y - origin!.y);
      await applyStyleMutation(sidebar, {
        transform: [`translate(${x}px, ${y}px)`,
          `matrix(1, 0, 0, 1, ${x}, ${y})`],
      });
    }
    const altered = await printedTextPages(page);
    expect(altered).not.toEqual(baseline);
    expect(() => check(altered)).toThrow('PDF glyph bounds.');
    expect(altered).toHaveLength(2);
    const geometry = pdfGlyphGeometry(altered[0]!, margins);
    expect(mutation === 'margin' ? geometry.contained : geometry.separated,
      'PDF glyph bounds.').toBe(false);
    expect(external).toEqual([]);
  });
}

test('PDF content measurements detect entry and accent mutations', async ({
  page,
}) => {
  test.setTimeout(60_000);
  const external = await denyExternalRequests(page);
  const printCase = CASES[1]!;
  const expected = expectedEntryIds(printCase);
  for (const mutation of ['hide', 'duplicate', 'reorder', 'accents'] as const) {
    const response = await page.goto(
      '/_harness/render?fixture=print-main-overflow'
      + '&template=modern-sidebar&mode=continuous&lng=vi&paper=a4',
    );
    expect(response?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    await page.emulateMedia({ media: 'print' });
    const baseline = await printedTextPages(page);
    expect(pdfEntryIds(baseline, 'MAIN')).toEqual(expected);
    expect(baseline.map((part) => part.text).join(' '))
      .toContain('Nội dung kiểm tra phân trang bản in');
    const check = (parts: typeof baseline) => {
      if (mutation === 'accents') {
        expect(parts.map((part) => part.text).join(' '),
          'PDF accented body text.')
          .toContain('Nội dung kiểm tra phân trang bản in');
      } else {
        expect(pdfEntryIds(parts, 'MAIN'), 'PDF entry occurrences and order.')
          .toEqual(expected);
      }
    };
    check(baseline);
    const entries = page.locator('.resume-main .entry');
    await expect(entries).toHaveCount(printCase.total);
    if (mutation === 'hide') {
      await applyStyleMutation(entries.first(), { display: 'none' });
    }
    await entries.first().evaluate((first, change) => {
      const next = first.nextElementSibling;
      if (change === 'duplicate') {
        first.after(first.cloneNode(true));
      }
      if (change === 'reorder' && next !== null) {
        first.before(next);
      }
      if (change === 'accents') {
        first.closest('.resume-main')!.querySelectorAll('.entry-body')
          .forEach((body) => {
            body.textContent = (body.textContent ?? '')
              .normalize('NFD').replace(/[\u0300-\u036f]/g, '')
              .replace(/đ/g, 'd').replace(/Đ/g, 'D');
          });
      }
    }, mutation);
    const mutated = await printedTextPages(page);
    expect(mutated).not.toEqual(baseline);
    expect(() => check(mutated)).toThrow(mutation === 'accents'
      ? 'PDF accented body text.'
      : 'PDF entry occurrences and order.');
    if (mutation === 'accents') {
      expect(mutated.map((part) => part.text).join(' '))
        .not.toContain('Nội dung kiểm tra phân trang bản in');
    } else {
      expect(pdfEntryIds(mutated, 'MAIN')).not.toEqual(expected);
    }
  }
  expect(external).toEqual([]);
});

test('PDF membership detects forced break', async ({ page }) => {
  test.setTimeout(30_000);
  const external = await denyExternalRequests(page);
  const printCase = CASES[1]!;
  const response = await page.goto(
    '/_harness/render?fixture=print-main-overflow'
    + '&template=modern-sidebar&mode=continuous&lng=vi&paper=a4',
  );
  expect(response?.ok()).toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  await waitForImages(page);
  await page.emulateMedia({ media: 'print' });
  const baseline = pdfMembership(await printedTextPages(page), printCase);
  checkMembership(baseline, 'print-main-overflow-vi-a4-light');
  expect(baseline.pageCount).toBe(EXPECTED_PAGES);
  const entries = page.locator('.resume-main .entry');
  await expect(entries).toHaveCount(printCase.total);
  await applyStyleMutation(entries.nth(3), { 'break-before': 'page' });
  const altered = pdfMembership(await printedTextPages(page), printCase);
  expect(altered).not.toEqual(baseline);
  expect(() => checkMembership(altered, 'print-main-overflow-vi-a4-light'))
    .toThrow('PDF approved page membership.');
  expect(external).toEqual([]);
});

test('PDF membership detects unbreakable entries', async ({ page }) => {
  const external = await denyExternalRequests(page);
  await page.emulateMedia({ media: 'print' });
  expect((await page.goto('/_harness/render?fixture=print-probe-breakable-entry'
    + '&template=modern-sidebar&mode=continuous&lng=vi&paper=a4'))?.ok())
    .toBe(true);
  await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
  const entries = page.locator('.layout-one-column .entry');
  await expect(entries).toHaveCount(2);
  await expect(entries.first()).toHaveCSS('break-inside', 'auto');
  const baselinePages = await printedTextPages(page);
  expect(baselinePages).toHaveLength(2);
  expect(baselinePages[0]!.text).toContain('MAIN-ENTRY-01');
  expect(baselinePages[0]!.text).not.toContain('BREAK-LAST');
  expect(baselinePages[1]!.text).toContain('BREAK-LAST');
  expect(baselinePages[1]!.text).toContain('MAIN-ENTRY-02');
  expect(pdfEntryIds(baselinePages, 'MAIN'))
    .toEqual(['MAIN-ENTRY-01', 'MAIN-ENTRY-02']);
  const baseline = pdfMembership(baselinePages, CASES[1]!);
  const check = (parts: typeof baselinePages) => {
    expect(pdfMembership(parts, CASES[1]!).pages[0]!.main.first,
      'PDF breakable entry membership.').toBe('MAIN-ENTRY-01');
  };
  check(baselinePages);
  await applyStyleMutation(page.locator('.entry'), {
    'break-inside': 'avoid',
  });
  await expect(entries.first()).toHaveCSS('break-inside', 'avoid');
  const alteredPages = await printedTextPages(page);
  expect(alteredPages).not.toEqual(baselinePages);
  expect(() => check(alteredPages)).toThrow('PDF breakable entry membership.');
  expect(alteredPages).toHaveLength(2);
  expect(alteredPages[0]!.text).toContain('BOUNDARY-ANCHOR');
  expect(alteredPages[0]!.text).not.toContain('MAIN-ENTRY-01');
  expect(alteredPages[1]!.text).toContain('MAIN-ENTRY-01');
  expect(alteredPages[1]!.text).toContain('BREAK-LAST');
  expect(pdfEntryIds(alteredPages, 'MAIN'))
    .toEqual(['MAIN-ENTRY-01', 'MAIN-ENTRY-02']);
  const altered = pdfMembership(alteredPages, CASES[1]!);
  expect(altered).not.toEqual(baseline);
  expect(altered.pages[0]!.main.first).toBeNull();
  expect(altered.pages[1]!.main.first).toBe('MAIN-ENTRY-01');
  expect(external).toEqual([]);
});

const PAINT_MUTATIONS = [
  'sidebar position', 'continuation tint', 'level bar',
  'photo', 'substituted photo',
] as const;
for (const mutation of PAINT_MUTATIONS) {
  test(`PDF measurements detect a changed ${mutation}`, async ({ page }) => {
    test.setTimeout(30_000);
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    await page.setViewportSize({
      width: Math.round((210 - 28) * 96 / 25.4), height: 1123,
    });
    expect((await page.goto('/_harness/render?fixture=print-sidebar-overflow'
      + '&template=modern-sidebar&mode=continuous&lng=vi&paper=a4'))?.ok())
      .toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    const margins = { x: 14 * 72 / 25.4, y: 13 * 72 / 25.4 };
    const layout = await measurePdfLayout(page, margins);
    const sidebarHeadings = layout.text.filter((item) =>
      item.role === 'heading'
      && item.left >= layout.sidebar!.left - 1);
    expect(sidebarHeadings).toHaveLength(1);
    const baseline = await printedDisplayPages(page);
    expect(baseline).toHaveLength(2);
    const columns = (pages: typeof baseline) => pdfColumnsMatch(
      pages[1]!.text, layout, margins, false,
    );
    const tint = (pages: typeof baseline, index: number) => pdfSidebarPaint(
      pages[index]!.paint, pages[index]!.text, layout.sidebar!,
      margins.y, index === 0, '#e3ecf0', {
        body: 'Nội dung kiểm tra phân trang',
        headings: index === 0 ? sidebarHeadings : [],
      },
    );
    const identity = await fixturePhotoIdentity();
    const photo = (pages: typeof baseline) => {
      const images = pages[0]!.paint.images;
      return images.length === 1
        && pdfImageMatches(images[0], identity, layout.photo!);
    };
    expect(columns(baseline)).toBe(true);
    expect(tint(baseline, 0)).toBe(true);
    expect(tint(baseline, 1)).toBe(true);
    expect(photo(baseline), pdfImageDiagnostic(baseline[0]!.paint.images[0],
      identity, layout.photo!)).toBe(true);
    const message = mutation === 'sidebar position'
      ? 'PDF column positions.'
      : mutation === 'continuation tint'
        ? 'PDF continuation sidebar tint.'
        : mutation === 'level bar'
          ? 'PDF sidebar paint and level bars.'
          : 'PDF photo identity.';
    const check = (parts: typeof baseline) => {
      const valid = mutation === 'sidebar position'
        ? columns(parts)
        : mutation === 'continuation tint'
          ? tint(parts, 1)
          : mutation === 'level bar'
            ? tint(parts, 0)
            : photo(parts);
      expect(valid, message).toBe(true);
    };
    check(baseline);
    if (mutation === 'sidebar position') {
      await applyStyleMutation(
        page.locator('.resume-sidebar .entry:nth-of-type(n+12)'), {
          transform: ['translateX(-300px)', 'matrix(1, 0, 0, 1, -300, 0)'],
        },
      );
    } else if (mutation === 'continuation tint') {
      const firstHeight = (baseline[0]!.text.height - margins.y
        - layout.sidebar!.top) / 0.75;
      const sidebar = page.locator('.resume-sidebar');
      await applyStyleMutation(sidebar, {
        'background-color': 'rgba(0, 0, 0, 0)', 'position': 'relative',
        'background-image': 'none',
      });
      const width = await sidebar.evaluate((element) =>
        getComputedStyle(element).width);
      await applyStyleMutation(sidebar, {
        'content': '""', 'position': 'absolute', 'top': '0px', 'left': '0px',
        'width': ['100%', width],
        'height': [`${firstHeight}px`, /^\d+(?:\.\d+)?px$/u],
        'background-color': 'rgb(227, 236, 240)',
        'background-image': 'none',
      }, '::before');
      await applyStyleMutation(sidebar.locator('.resume-section'), {
        position: 'relative',
      });
    } else if (mutation === 'level bar') {
      await applyStyleMutation(page.locator('.level-fill'), {
        width: ['20%', /^\d+(?:\.\d+)?px$/u],
      });
    } else if (mutation === 'photo') {
      await applyStyleMutation(page.locator('.resume-photo img'), {
        visibility: 'hidden',
      });
    } else {
      await page.locator('.resume-photo img').evaluate(async (element) => {
        const canvas = document.createElement('canvas');
        canvas.width = 192;
        canvas.height = 192;
        const context = canvas.getContext('2d')!;
        context.fillStyle = '#ffffff';
        context.fillRect(0, 0, 192, 192);
        const image = element as HTMLImageElement;
        image.src = canvas.toDataURL();
        await image.decode();
      });
    }
    const altered = await printedDisplayPages(page);
    expect(altered).not.toEqual(baseline);
    expect(() => check(altered)).toThrow(message);
    expect(altered).toHaveLength(2);
    if (mutation === 'sidebar position') {
      expect(columns(altered)).toBe(false);
    } else if (mutation === 'continuation tint') {
      expect(tint(altered, 0)).toBe(true);
      expect(tint(altered, 1)).toBe(false);
    } else if (mutation === 'level bar') {
      expect(tint(altered, 0), 'PDF sidebar paint and level bars.').toBe(false);
    } else {
      expect(photo(altered)).toBe(false);
    }
    expect(external).toEqual([]);
  });
}

for (const mutation of ['bar', 'dots', 'tag', 'overpaint'] as const) {
  test(`PDF ${mutation} detects a level-four mutation`, async ({ page }) => {
    test.setTimeout(30_000);
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    await page.setViewportSize({
      width: Math.round((210 - 28) * 96 / 25.4), height: 1123,
    });
    const mode = mutation === 'overpaint' ? 'bar' : mutation;
    expect((await page.goto('/_harness/render?template=modern-sidebar'
      + `&mode=continuous&paper=a4&fixture=print-probe-widgets-${mode}`))?.ok())
      .toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    const margins = { x: 14 * 72 / 25.4, y: 13 * 72 / 25.4 };
    const layout = await measurePdfLayout(page, margins);
    const widget = layout.widgets.find(({ name }) => name === 'SKILL-4');
    expect(widget).toBeDefined();
    const baseline = await printedDisplayPages(page);
    expect(baseline).toHaveLength(1);
    const correct = (pages: typeof baseline) => pdfWidgetMatches(
      pages[0]!, widget!, 4, mode, {
        solid: '#0f6b7d', track: '#cfe1e5', onAccent: '#ffffff',
        fontSize: 14 * 0.9,
      },
    );
    expect(correct(baseline), pdfWidgetDiagnostic(baseline[0]!, widget!))
      .toBe(true);
    const check = (parts: typeof baseline) => {
      expect(correct(parts), 'PDF level widget. '
      + pdfWidgetDiagnostic(parts[0]!, widget!)).toBe(true);
    };
    check(baseline);
    if (mutation === 'overpaint') {
      const track = page.locator('.level-track');
      await applyStyleMutation(track, { position: 'relative' });
      await applyStyleMutation(track, {
        'content': '""', 'position': 'absolute', 'inset': '0px',
        'background-color': 'rgba(255, 255, 255, 0.99)',
        'background-image': 'none',
      }, '::after');
    } else if (mode === 'bar') {
      const fill = page.locator('.level-fill');
      await applyStyleMutation(fill, {
        width: ['20%', /^\d+(?:\.\d+)?px$/u],
      });
      expect(await fill.evaluateAll((elements) => elements.every((element) =>
        Math.abs(element.getBoundingClientRect().width
          - element.parentElement!.getBoundingClientRect().width * 0.2) <= 1),
      )).toBe(true);
    } else if (mode === 'dots') {
      await applyStyleMutation(page.locator('.level-dot'), {
        'background-color': 'rgb(15, 107, 125)',
        'background-image': 'none',
      });
    } else {
      await page.getByRole('img', { name: 'SKILL-4: 4 of 5', exact: true })
        .locator('span').evaluate((label) => { label.textContent = '3/5'; });
    }
    const altered = await printedDisplayPages(page);
    expect(altered).not.toEqual(baseline);
    expect(() => check(altered)).toThrow('PDF level widget.');
    expect(altered).toHaveLength(1);
    expect(correct(altered), pdfWidgetDiagnostic(altered[0]!, widget!))
      .toBe(false);
    expect(external).toEqual([]);
  });
}

for (const fixture of ['print-main-overflow', 'sample-modern-sidebar-vi']) {
  for (const defect of ['clipping', 'overflow', 'overlap', 'spacing', 'font',
    'unloaded font', 'image', 'columns']) {
    if (fixture.startsWith('sample-') && defect === 'image') {
      continue;
    }
    test(`${fixture} print DOM detects ${defect}`, async ({ page }) => {
      const external = await denyExternalRequests(page);
      await page.emulateMedia({ media: 'print', colorScheme: 'dark' });
      await page.setViewportSize({ width: 688, height: 1123 });
      expect((await page.goto(`/_harness/render?fixture=${fixture}`
        + '&template=modern-sidebar&mode=continuous&print=1'))?.ok())
        .toBe(true);
      await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
      await waitForImages(page);
      const check = (assertion?: string) =>
        assertPrintDom(page, 'Be Vietnam Pro', 2, undefined, assertion);
      await check();
      let message: string;
      if (defect === 'clipping') {
        const body = page.locator('.entry-body').first();
        await applyStyleMutation(body, { height: '1px', overflow: 'hidden' });
        expect(await body.evaluate((element) =>
          element.scrollHeight > element.clientHeight + 1)).toBe(true);
        message = 'Print text containment.';
      } else if (defect === 'overflow') {
        const body = page.locator('.entry-body p').first();
        await body.evaluate((element) => {
          element.textContent = 'x'.repeat(2048);
        });
        await expect(body).toHaveText('x'.repeat(2048));
        await applyStyleMutation(body, { 'white-space': 'nowrap' });
        message = 'Print horizontal overflow.';
      } else if (defect === 'unloaded font') {
        expect(await page.evaluate(() => {
          const faces = [...document.fonts].filter((face) =>
            face.family.replaceAll('"', '') === 'Be Vietnam Pro');
          faces.forEach((face) => document.fonts.delete(face));
          return faces.length;
        })).toBeGreaterThan(0);
        message = 'Print requested fonts loaded.';
      } else if (defect === 'overlap') {
        const entries = page.locator('.resume-main .entry');
        const gap = await entries.nth(1).evaluate((element) =>
          element.getBoundingClientRect().top
          - element.previousElementSibling!.getBoundingClientRect().bottom);
        const shift = Math.ceil(gap + 4);
        await applyStyleMutation(entries.nth(1), {
          transform: [`translateY(-${shift}px)`,
            `matrix(1, 0, 0, 1, 0, -${shift})`],
        });
        message = 'Print sibling separation.';
      } else if (defect === 'spacing') {
        await applyStyleMutation(page.locator('.resume-main .entry').nth(2), {
          'margin-top': '14px',
        });
        message = 'Print entry and section spacing.';
      } else if (defect === 'font') {
        await applyStyleMutation(page.locator('.resume-document'), {
          'font-family': 'serif',
        });
        message = 'Print requested font family.';
      } else if (defect === 'image') {
        await applyStyleMutation(page.locator('.resume-photo img'), {
          visibility: 'hidden',
        });
        message = 'Print images decoded and visible.';
      } else {
        await applyStyleMutation(page.locator('.resume-sidebar'), {
          transform: ['translateX(4px)', 'matrix(1, 0, 0, 1, 4, 0)'],
        });
        message = 'Print column geometry.';
      }
      await expect(check(message)).rejects.toThrow(message);
      expect(external).toEqual([]);
    });
  }
}

for (const fixture of ['print-main-overflow', 'sample-modern-sidebar-vi']) {
  for (const defect of ['count', 'size'] as const) {
    test(`${fixture} PDF page geometry detects changed ${defect}`, async ({
      page,
    }) => {
      const external = await denyExternalRequests(page);
      await page.emulateMedia({ media: 'print' });
      expect((await page.goto(`/_harness/render?fixture=${fixture}`
        + '&template=modern-sidebar&mode=continuous&print=1'))?.ok())
        .toBe(true);
      await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
      await waitForImages(page);
      const count = fixture.startsWith('sample-') ? 1 : 2;
      const baseline = await printedTextPages(page);
      assertPdfPageGeometry(baseline, count, 'a4');
      if (defect === 'count') {
        await page.locator('.resume-document').evaluate((element) => {
          const extra = document.createElement('div');
          extra.textContent = 'EXTRA-PAGE';
          extra.style.breakBefore = 'page';
          element.append(extra);
        });
        await expect(page.getByText('EXTRA-PAGE', { exact: true }))
          .toHaveCount(1);
      } else {
        await page.addStyleTag({ content: '@page { size: 180mm 320mm; }' });
      }
      const altered = await printedTextPages(page);
      expect(altered).not.toEqual(baseline);
      if (defect === 'count') {
        expect(() => assertPdfPageGeometry(altered, count, 'a4'))
          .toThrow('PDF page count.');
      } else {
        expect(altered[0]!.width).not.toBe(baseline[0]!.width);
        expect(() => assertPdfPageGeometry(altered, altered.length, 'a4'))
          .toThrow('PDF page size.');
      }
      expect(external).toEqual([]);
    });
  }
}

for (const defect of ['font size', 'color', 'icon color', 'crop'] as const) {
  test(`overflow print contract detects changed ${defect}`, async ({
    page,
  }) => {
    const external = await denyExternalRequests(page);
    await page.emulateMedia({ media: 'print' });
    await page.setViewportSize({ width: 688, height: 1123 });
    expect((await page.goto('/_harness/render?fixture=print-main-overflow'
      + '&template=modern-sidebar&mode=continuous'))?.ok()).toBe(true);
    await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
    await waitForImages(page);
    const margins = { x: 14 * 72 / 25.4, y: 13 * 72 / 25.4 };
    const crop = { x: 0.1, y: 0.05, width: 0.8, height: 0.8 };
    const baseline = await printedDisplayPages(page);
    const expected = await overflowPdfChunks(CASES[1]!, 'vi');
    const check = async (focused = false) => {
      if (focused && defect === 'crop') {
        await assertPhotoCrop(page, crop);
        return;
      }
      const layout = await measurePdfLayout(page, margins);
      assertOverflowTextStyle(layout, {
        fills: [], images: [],
        text: baseline.flatMap(({ paint }) => paint.text),
      }, expected, focused && defect !== 'crop' ? defect : undefined);
      if (!focused) {
        await assertPhotoCrop(page, crop);
      }
    };
    await check();
    if (defect === 'crop') {
      await applyStyleMutation(page.locator('.resume-photo img'), {
        left: '0px', top: '0px',
      });
    } else if (defect === 'icon color') {
      await applyStyleMutation(page.locator('.resume-icon'), {
        color: 'rgb(0, 0, 0)',
      });
    } else {
      await applyStyleMutation(page.locator('.resume-document'),
        defect === 'color'
          ? { color: 'rgb(0, 0, 0)' }
          : { 'font-size': '16px' });
    }
    await expect(check(true)).rejects.toThrow(defect === 'crop'
      ? 'Print photo crop.'
      : defect === 'icon color'
        ? 'Print icon color.'
        : defect === 'color'
          ? 'Print text color.'
          : 'Print font size.');
    expect(external).toEqual([]);
  });
}
