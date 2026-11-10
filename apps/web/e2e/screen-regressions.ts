import { expect, test } from '@playwright/test';

import {
  expectScreenClass, measureTextContainment, resumeContract,
  type ScreenContract,
} from './display-assertions';
import { expectSvg } from './screen-paint';
import { measurePaintedText } from './screen-containment';
import { expectStyleRule } from './screen-expectations';
import { applyStyleMutation, proveMutation } from './style-mutation';

const CONTRACT: ScreenContract = {
  root: '#surface', text: '#surface p', flow: '#surface p',
  type: ['#surface', 'sans-serif', 'sans-serif'],
  paint: ['#surface', 'color', 'rgb(0, 0, 0)'],
  copy: ['#surface p:first-child', 'First line'],
  aria: ['#surface', '- generic'],
};

export function screenRegressionTests(): void {
  test('screen styles recognize normal letter spacing as zero', async ({
    page,
  }) => {
    await page.setContent('<p>First line</p>');
    const assertion = () => expectStyleRule(page, {
      selector: 'p', message: 'Screen zero letter spacing',
      css: { 'letter-spacing': '0px' },
    });
    await proveMutation(assertion, () => applyStyleMutation(page.locator('p'), {
      'letter-spacing': '1px',
    }), 'Screen zero letter spacing letter-spacing.');
  });

  test('screen text allows clipped decorative siblings', async ({ page }) => {
    await page.setContent('<main id="surface"><div id="clip" '
      + 'style="position:relative;height:24px;overflow:hidden">'
      + '<p style="margin:0">First line</p><span aria-hidden="true" '
      + 'style="position:absolute;top:100px">Decoration</span></div></main>');
    const assertion = () => expectScreenClass(page, CONTRACT, 'containment');
    await assertion();
    await proveMutation(assertion, () => applyStyleMutation(
      page.locator('p'), {
        transform: ['translateY(100px)', 'matrix(1, 0, 0, 1, 0, 100)'],
      },
    ), 'Screen ancestor clipping.');
  });

  test('screen text allows font leading but rejects clipped ink', async ({
    page,
  }) => {
    await page.setContent('<main id="surface"><p style="margin:0;'
      + 'font:16px/12px Arial">xxxx</p></main>');
    const text = page.locator('p');
    const assertion = () => expect.poll(() => measureTextContainment(text),
      'Screen containment.').toEqual({ visible: true, contained: true });
    await assertion();
    await proveMutation(assertion, () => applyStyleMutation(text, {
      height: '1px', overflow: 'hidden',
    }), 'Screen containment.');
  });

  test('screen blocks contain descenders and stacked accents', async ({
    page,
  }) => {
    await page.setContent('<main id="surface"><p style="margin:0;'
      + 'font:32px/48px Arial">gẤ<br>gẤ</p></main>');
    const text = page.locator('p');
    const assertion = () => expect.poll(() => measureTextContainment(text),
      'Screen painted ink containment.')
      .toEqual({ visible: true, contained: true });
    await proveMutation(assertion, () => applyStyleMutation(text, {
      'line-height': '1px',
    }), 'Screen painted ink containment.');
  });

  test('screen blocks reject overlapping painted lines', async ({ page }) => {
    await page.setContent('<main id="surface"><p style="margin:0;'
      + 'height:160px;padding:48px 8px;font:32px/48px Arial">'
      + 'gẤ<br>gẤ</p></main>');
    const text = page.locator('p');
    const assertion = () => expect.poll(async () =>
      (await measurePaintedText(text)).separated,
    'Screen painted line separation.').toBe(true);
    await proveMutation(assertion, async () => {
      const restore = await applyStyleMutation(text, { 'line-height': '1px' });
      const painted = await measurePaintedText(text);
      expect(painted.visible).toBe(true);
      expect(painted.contained).toBe(true);
      return restore;
    }, 'Screen painted line separation.');
  });

  test('screen ink uses the glyphs on each individual line', async ({
    page,
  }) => {
    await page.setContent('<main id="surface"><p style="margin:0;'
      + 'height:128px;width:300px;text-indent:100px;'
      + 'font:32px/28px Arial">x<br>gẤ</p></main>');
    const text = page.locator('p');
    const assertion = () => expect.poll(() => measureTextContainment(text),
      'Screen per-line ink containment.')
      .toEqual({ visible: true, contained: true });
    await proveMutation(assertion, async () => {
      const original = await text.innerHTML();
      await text.evaluate((node) => {
        node.innerHTML = 'gẤ<br>x';
      });
      await expect(text).toHaveText('gẤx');
      expect(await text.evaluate((node) =>
        node.scrollHeight <= node.clientHeight + 1)).toBe(true);
      return async () => {
        await text.evaluate((node, html) => {
          node.innerHTML = html;
        }, original);
      };
    }, 'Screen per-line ink containment.');
  });

  test('screen ink measures each wrapped line with its shaping', async ({
    page,
  }) => {
    await page.setContent('<main id="surface"><p style="margin:0;'
      + 'width:150px;padding:8px;font:italic small-caps 32px/48px Arial;'
      + 'letter-spacing:2px;word-spacing:3px;overflow-wrap:anywhere">'
      + 'gẤ gẤ gẤ gẤ gẤ gẤ</p></main>');
    const text = page.locator('p');
    const assertion = () => expect.poll(() => measureTextContainment(text),
      'Screen shaped ink containment.')
      .toEqual({ visible: true, contained: true });
    await proveMutation(assertion, () => applyStyleMutation(text, {
      'line-height': '1px',
    }), 'Screen shaped ink containment.');
  });

  test('screen text rejects a fixed height with visible overflow', async ({
    page,
  }) => {
    await page.setContent('<main id="surface"><p style="margin:0;'
      + 'font:16px/24px Arial">First line<br>Last line</p></main>');
    const assertion = () => expectScreenClass(page, CONTRACT, 'containment');
    await assertion();
    await proveMutation(assertion, () => applyStyleMutation(
      page.locator('p'), { height: '24px' },
    ), 'Screen containment.');
  });

  test('renderer text excludes wrappers with graphical or absent levels',
    async ({ page }) => {
      await page.goto('/_harness/render?fixture=print-probe-widgets-dots'
        + '&template=modern-sidebar&mode=continuous&lng=vi');
      await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
      const document = page.locator(
        '.resume-document:not(.pagination-measurement)',
      );
      await expect(document.locator('.level-widget > .level-dots'))
        .toHaveCount(12);
      const contract = resumeContract('modern-sidebar', 'vi');
      const assertion = () => expectScreenClass(page, contract, 'containment');
      await assertion();
      await proveMutation(assertion, () => applyStyleMutation(
        page.locator('.entry-title').first(), { opacity: '0' },
      ), 'Screen containment.');
    });

  test('screen text restores scrolling after a named class check', async ({
    page,
  }) => {
    await page.setContent('<style>p{height:20px;margin:0}</style>'
      + '<main id="surface"><div id="sheet-body" '
      + 'style="height:24px;overflow-y:auto">'
      + '<p>First line</p><p>Last line</p></div></main>');
    await expectScreenClass(page, {
      ...CONTRACT, scrolling: ['#sheet-body'],
    }, 'containment');
    expect(await page.locator('#sheet-body').evaluate((node) => node.scrollTop))
      .toBe(0);
  });

  test('screen containment rejects ancestor clipping before scrolling', async ({
    page,
  }) => {
    await page.setContent('<main id="surface"><div id="list">'
      + '<p>First line</p><p>Last line</p></div></main>');
    const assertion = () => expectScreenClass(page, CONTRACT, 'containment');
    await assertion();
    const restore = await applyStyleMutation(page.locator('#list'), {
      height: '24px', overflow: 'hidden',
    });
    expect(await page.locator('#list').evaluate((node) =>
      node.scrollHeight > node.clientHeight)).toBe(true);
    await expect(assertion()).rejects.toThrow('Screen ancestor clipping.');
    expect(await page.locator('#list').evaluate((node) => node.scrollTop))
      .toBe(0);
    await restore();
    await assertion();
  });

  for (const selector of ['img', '#frame']) {
    test(`screen images reject opacity on ${selector}`, async ({ page }) => {
      await page.setContent('<main id="surface"><div id="frame">'
        + '<img width="20" height="20" src="data:image/svg+xml,'
        + '%3Csvg xmlns=\'http://www.w3.org/2000/svg\' width=\'20\' '
        + 'height=\'20\'%3E%3Crect width=\'20\' height=\'20\' '
        + 'fill=\'red\'/%3E%3C/svg%3E"></div></main>');
      const assertion = () => expectScreenClass(page, {
        ...CONTRACT, image: 'img',
      }, 'images');
      await assertion();
      const restore = await applyStyleMutation(page.locator(selector), {
        opacity: '0',
      });
      await expect(assertion()).rejects.toThrow('Screen images.');
      await restore();
      await assertion();
    });
  }

  test('screen SVG rejects missing geometry with its identity intact', async ({
    page,
  }) => {
    await page.setContent('<svg class="lucide-check" width="24" height="24" '
      + 'viewBox="0 0 24 24" fill="none" stroke="rgb(35, 57, 154)" '
      + 'stroke-width="2"><path d="m9 12 2 2 4-4"/></svg>');
    const svg = page.locator('svg');
    const assertion = () => expectSvg(svg, {
      viewBox: '0 0 24 24',
      primitives: [{ selector: 'path', attributes: { d: 'm9 12 2 2 4-4' },
        stroke: 'rgb(35, 57, 154)', fill: 'none', width: '2px' }],
    }, 'Screen check icon.');
    await assertion();
    await svg.locator('path').evaluate((node) => node.remove());
    await expect(svg).toHaveClass('lucide-check');
    await expect(svg.locator('path')).toHaveCount(0);
    await expect(assertion()).rejects.toThrow('Screen check icon.');
  });

  test('screen containment allows only scrolling sheet bodies', async ({
    page,
  }) => {
    await page.setContent('<style>p{height:20px;margin:0}</style>'
      + '<main id="surface"><div id="sheet-body" '
      + 'style="height:24px;overflow-y:auto">'
      + '<p>First line</p><p>Last line</p></div></main>');
    const assertion = () => expectScreenClass(page, {
      ...CONTRACT, scrolling: ['#sheet-body'],
    }, 'containment');
    await assertion();
    expect(await page.locator('#sheet-body').evaluate((node) =>
      node.scrollHeight > node.clientHeight)).toBe(true);
    await proveMutation(assertion, () => applyStyleMutation(
      page.locator('#sheet-body'), { overflow: 'hidden' },
    ), 'Screen ancestor clipping.');
  });
}
