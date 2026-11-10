import { expect, test, type Page } from '@playwright/test';

import {
  expectScreen, measureTextContainment, resumeContract,
} from './display-assertions';
import { applyStyleMutation, proveMutation } from './style-mutation';
import { containedPaint } from './screen-paint';

async function assertWidgets(
  page: Page, mode: 'bar' | 'dots' | 'tag' | 'text',
  engineer = false,
): Promise<void> {
  const entries = page.locator('.resume-document .entry');
  await expect(entries, 'Widget entry count.').toHaveCount(14);
  const solid = engineer ? 'rgb(13, 90, 115)' : 'rgb(15, 107, 125)';
  const trackColor = (prefix: string) => engineer
    ? 'rgb(207, 222, 227)'
    : prefix === 'SKILL'
      ? 'rgb(207, 225, 229)'
      : 'rgb(185, 210, 217)';
  for (const prefix of ['SKILL', 'LANGUAGE']) {
    for (let level = 0; level <= 6; level++) {
      const entry = entries.filter({
        has: page.getByText(
          `${prefix}-${level === 6 ? 'NONE' : level}`, { exact: true }),
      });
      const widget = entry.locator('.level-widget');
      await expect(entry, 'Widget entry identity.').toHaveCount(1);
      if (level === 6 || mode === 'text') {
        await expect(widget, 'Widget absence.').toHaveCount(0);
        continue;
      }
      await expect(widget, 'Widget accessible level.').toHaveAccessibleName(
        `${prefix}-${level}: ${level} of 5`);
      await expect(widget, 'Widget accessible role.').toHaveRole('img');
      await expect(widget, 'Widget visibility.').toBeVisible();
      await expect(widget, 'Widget visibility.').toHaveCSS('opacity', '1');
      await expect.poll(() => containedPaint(widget, '.entry'),
        'Widget painted containment.').toBe(true);
      if (mode === 'bar') {
        const track = widget.locator('.level-track');
        const fill = widget.locator('.level-fill');
        await expect(track, 'Widget track height.').toHaveCSS('height', '4px');
        await expect(track, 'Widget track color.').toHaveCSS('background-color',
          trackColor(prefix));
        await expect(fill, 'Widget fill color.')
          .toHaveCSS('background-color', solid);
        for (const part of [track, fill]) {
          await expect.poll(() => containedPaint(part, '.level-widget',
            part === fill && level === 0),
          'Widget painted containment.').toBe(true);
        }
        const geometry = await widget.evaluate((element) => {
          const track = element.querySelector('.level-track')!
            .getBoundingClientRect();
          const fill = element.querySelector('.level-fill')!
            .getBoundingClientRect();
          return {
            width: track.width, fill: fill.width,
            left: fill.left - track.left,
          };
        });
        expect(Math.abs(geometry.fill - geometry.width * level / 5),
          'Widget bar length.').toBeLessThanOrEqual(1);
        expect(Math.abs(geometry.left), 'Widget bar start.')
          .toBeLessThanOrEqual(1);
      } else if (mode === 'dots') {
        const dots = widget.locator('.level-dot');
        await expect(dots, 'Widget dot count.').toHaveCount(5);
        for (let index = 0; index < 5; index++) {
          await expect.poll(() => containedPaint(dots.nth(index),
            '.level-widget'), 'Widget painted containment.').toBe(true);
          await expect(dots.nth(index), 'Widget dot diameter.')
            .toHaveCSS('width', '7px');
          await expect(dots.nth(index), 'Widget dot diameter.')
            .toHaveCSS('height', '7px');
          await expect(dots.nth(index), 'Widget dot paint.').toHaveCSS(
            'background-color', index < level
              ? solid
              : trackColor(prefix));
        }
        await expect(widget.locator('.level-dots'), 'Widget dot gap.')
          .toHaveCSS('gap', '4px');
      } else {
        await expect(widget, 'Widget tag label.').toHaveText(`${level}/5`);
        await expect(widget, 'Widget tag paint.')
          .toHaveCSS('background-color', solid);
        await expect(widget, 'Widget tag ink.')
          .toHaveCSS('color', 'rgb(255, 255, 255)');
        const padding = await widget.evaluate((element) => {
          const css = getComputedStyle(element);
          return {
            x: parseFloat(css.paddingLeft),
            y: parseFloat(css.paddingTop),
          };
        });
        const size = engineer ? 11.7 : 12.6;
        expect(Math.abs(padding.x - size * 0.5), 'Widget tag padding.')
          .toBeLessThanOrEqual(0.1);
        expect(Math.abs(padding.y - size * 0.15), 'Widget tag padding.')
          .toBeLessThanOrEqual(0.1);
        await expect(widget, 'Widget tag border.')
          .toHaveCSS('border-top-width', '0px');
        await expect(widget, 'Widget tag radius.')
          .toHaveCSS('border-radius', '3px');
        await expect.poll(() => measureTextContainment(widget),
          'Widget tag containment.')
          .toEqual({ visible: true, contained: true });
      }
    }
  }
}

export function rendererWidgetTests(): void {
  for (const template of ['modern-sidebar', 'engineer-compact']) {
    for (const language of ['vi', 'en'] as const) {
      for (const mode of ['bar', 'dots', 'tag', 'text'] as const) {
        test(`screen widget inputs ${template} ${language} ${mode}`, async ({
          page,
        }) => {
          await page.setViewportSize({ width: 794, height: 1123 });
          await page.goto(`/_harness/render?template=${template}`
            + '&mode=continuous'
            + `&fixture=print-probe-widgets-${mode}&lng=${language}`);
          await expect(page.locator('[data-fonts-ready="true"]'))
            .toHaveCount(1);
          const heading = language === 'vi' ? 'Kỹ năng' : 'Skills';
          const selector = '.resume-main .section-heading h2';
          await expectScreen(page, {
            ...resumeContract(template, language), image: undefined,
            copy: [selector, heading],
            aria: [selector, `- heading "${heading}" [level=2]`],
          });
          await assertWidgets(page, mode, template === 'engineer-compact');
        });
      }
    }
  }

  for (const mode of ['bar', 'dots', 'tag'] as const) {
    for (const defect of ['wrapper opacity', 'part opacity', 'clipping']) {
      test(`screen ${mode} widgets reject ${defect}`, async ({ page }) => {
        await page.goto('/_harness/render?template=modern-sidebar'
          + `&mode=continuous&fixture=print-probe-widgets-${mode}`);
        await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
        const widget = page.getByRole('img', {
          name: 'SKILL-4: 4 of 5', exact: true,
        });
        const part = widget.locator(mode === 'bar'
          ? '.level-fill'
          : mode === 'dots'
            ? '.level-dot:first-child'
            : ':scope > span');
        const wrapper = widget.locator('..');
        const assertion = () => assertWidgets(page, mode);
        await proveMutation(assertion, () => applyStyleMutation(
          defect === 'part opacity' ? part : wrapper,
          defect === 'clipping'
            ? { height: '1px', overflow: 'hidden' }
            : { opacity: '0' },
        ), mode === 'tag' && defect === 'part opacity'
          ? 'Widget tag containment.'
          : 'Widget painted containment.');
      });
    }
  }

  for (const mode of ['bar', 'dots', 'tag'] as const) {
    test(`screen widgets reject changed ${mode}`, async ({ page }) => {
      await page.goto('/_harness/render?template=modern-sidebar&mode=continuous'
        + `&fixture=print-probe-widgets-${mode}`);
      await expect(page.locator('[data-fonts-ready="true"]')).toHaveCount(1);
      const assertion = () => assertWidgets(page, mode);
      await assertion();
      const widget = page.getByRole('img', {
        name: 'SKILL-4: 4 of 5', exact: true,
      });
      let restore: () => Promise<void>;
      if (mode === 'tag') {
        await widget.locator('span').evaluate((node) => {
          node.textContent = '3/5';
        });
        await expect(widget).toHaveText('3/5');
        restore = async () => {
          await widget.locator('span').evaluate((node) => {
            node.textContent = '4/5';
          });
        };
      } else {
        const target = widget.locator(mode === 'bar'
          ? '.level-fill'
          : '.level-dot:last-child');
        restore = await applyStyleMutation(target, mode === 'bar'
          ? { width: ['20%', /^\d+(?:\.\d+)?px$/u] }
          : { 'background-color': 'rgb(15, 107, 125)' });
      }
      await expect(assertion()).rejects.toThrow(mode === 'bar'
        ? 'Widget bar length.'
        : mode === 'dots'
          ? 'Widget dot paint.'
          : 'Widget tag label.');
      await restore();
      await assertion();
    });
  }
}
