import { expect, type Page } from '@playwright/test';

import { expectStyleRules, type StyleRule } from './screen-expectations';
import { expectSvg } from './screen-paint';

export function cardType(name: string, headline: boolean): StyleRule[] {
  const size = name === 'long-name-vi' ? 52 : name === 'no-name' ? 24 : 72;
  return [
    {
      selector: '.preview-card-name', message: 'Card name typography',
      css: { 'line-height': `${size * 1.2}px`,
        'letter-spacing': `${size * -0.01}px`, 'text-align': 'center' },
    },
    ...(headline
      ? [{
          selector: '.preview-card-headline',
          message: 'Card headline typography',
          css: { 'font-weight': '400', 'line-height': '43.2px',
            'letter-spacing': 'normal', 'text-align': 'center' },
        }]
      : []),
    {
      selector: '.preview-card-footer-link', message: 'Card footer typography',
      css: { 'line-height': '32px', 'letter-spacing': 'normal',
        'font-weight': '500' },
    },
  ];
}

export async function expectCardLogo(page: Page): Promise<void> {
  const logo = page.getByRole('img', { name: 'aboutme.vn', exact: true });
  const stroke = 'rgb(204, 38, 73)';
  const fill = 'none';
  await expectSvg(logo, {
    viewBox: '0 0 30 32',
    primitives: [
      { selector: '[data-logo-ring="outer"]',
        attributes: { cx: '15', cy: '16', r: '13.4' },
        fill, stroke, width: '2.6px' },
      { selector: '[data-logo-ring="inner"]',
        attributes: { cx: '15', cy: '16', r: '9.6' },
        fill, stroke, width: '1px' },
      { selector: 'g > g > circle',
        attributes: { cx: '13.4', cy: '17.2', r: '3.7' },
        fill, stroke, width: '3.2px' },
      { selector: 'path', attributes: { d: 'M17.1 13.2v7.8' },
        fill, stroke, width: '3.2px' },
    ],
  }, 'Card logo primitives.');
  await expect(logo.locator('[data-logo-part="mark"]'), 'Card logo tilt.')
    .toHaveAttribute('transform', 'rotate(-8 15 16)');
}

export async function expectCardDetails(
  page: Page, name: string, headline: boolean,
): Promise<void> {
  await expectStyleRules(page, cardType(name, headline));
  await expectCardLogo(page);
}
