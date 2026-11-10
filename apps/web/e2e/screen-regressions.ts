import { expect, test } from '@playwright/test';

import {
  expectScreenClass, type ScreenContract,
} from './display-assertions';
import { expectSvg } from './screen-paint';
import { applyStyleMutation, proveMutation } from './style-mutation';

const CONTRACT: ScreenContract = {
  root: '#surface', text: '#surface p', flow: '#surface p',
  type: ['#surface', 'sans-serif', 'sans-serif'],
  paint: ['#surface', 'color', 'rgb(0, 0, 0)'],
  copy: ['#surface p:first-child', 'First line'],
  aria: ['#surface', '- generic'],
};

export function screenRegressionTests(): void {
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
