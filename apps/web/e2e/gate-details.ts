import { expect, type Page } from '@playwright/test';

import { measureTextContainment } from './display-assertions';
import { expectStyleRules, type StyleRule } from './screen-expectations';
import { visiblePaint } from './screen-paint';

const FAMILY = '"Be Vietnam Pro", Inter, system-ui, sans-serif';

export const GATE_TYPE: readonly StyleRule[] = [
  {
    selector: '.gate-title', message: 'Gate title typography',
    css: { 'font-family': FAMILY, 'font-size': '20px', 'font-weight': '700',
      'line-height': '26px', 'letter-spacing': 'normal',
      'text-transform': 'none', 'text-align': 'start' },
  },
  {
    selector: '.gate-text', message: 'Gate body typography',
    css: { 'font-family': FAMILY, 'font-size': '15px', 'font-weight': '400',
      'line-height': '22.5px', 'letter-spacing': 'normal',
      'text-transform': 'none', 'text-align': 'start' },
  },
  {
    selector: '.gate-provider', message: 'Gate provider typography',
    css: { 'font-family': FAMILY, 'font-size': '15px', 'font-weight': '500',
      'line-height': 'normal', 'letter-spacing': 'normal',
      'text-transform': 'none', 'text-align': 'start' },
  },
];

export async function expectGateProviders(page: Page): Promise<void> {
  // The gate offers text links, with no provider artwork.
  await expect(page.locator('.gate-provider svg'), 'Gate provider artwork.')
    .toHaveCount(0);
}

export function inviteType(placement: 'bar' | 'card'): readonly StyleRule[] {
  return [
    {
      selector: '.join-invite p', message: 'Invite body typography',
      css: { 'font-family': FAMILY, 'font-size': '14px', 'font-weight': '400',
        'line-height': placement === 'card' ? '21px' : 'normal',
        'letter-spacing': 'normal', 'text-transform': 'none',
        'text-align': 'start' },
    },
    {
      selector: '.join-invite a', message: 'Invite action typography',
      css: { 'font-family': FAMILY,
        'font-size': placement === 'card' ? '16px' : '14px',
        'font-weight': '500',
        'line-height': placement === 'card' ? '40px' : 'normal',
        'letter-spacing': 'normal', 'text-transform': 'none',
        'text-align': placement === 'card' ? 'center' : 'start' },
    },
  ];
}

export function inviteText(language: 'vi' | 'en'): string {
  return language === 'vi'
    ? 'Tự tạo CV của bạn trên aboutme.vn: miễn phí, mã nguồn mở, chia sẻ '
    + 'bằng một đường dẫn.'
    : 'Make your own resume on aboutme.vn: free, open source, shared '
      + 'with one link.';
}

export async function expectInviteText(
  page: Page, language: 'vi' | 'en', placement: 'bar' | 'card',
): Promise<void> {
  const text = page.locator('.join-invite p');
  const message = 'Invite paragraph containment.';
  await expect.poll(() => visiblePaint(text), message).toBe(true);
  await expect.poll(() => text.evaluate((node) => {
    const box = node.getBoundingClientRect();
    const region = node.closest('.join-invite')!.getBoundingClientRect();
    return box.left >= region.left - 1 && box.right <= region.right + 1
      && box.top >= region.top - 1 && box.bottom <= region.bottom + 1;
  }), message).toBe(true);
  if (placement === 'card') {
    await expect.poll(() => measureTextContainment(text), message)
      .toEqual({ visible: true, contained: true });
  } else {
    await expect(text, message).toHaveCSS('text-overflow', 'ellipsis');
    await expect(text, message).toHaveCSS('white-space', 'nowrap');
  }
  await expect(text, 'Invite full accessible paragraph.').toMatchAriaSnapshot(
    `- paragraph: ${JSON.stringify(inviteText(language))}`,
  );
}

/** Freeze before navigation so the invite mounts at this exact timer origin. */
export async function pauseInviteClock(page: Page): Promise<void> {
  await page.clock.install({ time: new Date('2026-01-01T00:00:00Z') });
  await page.clock.pauseAt(new Date('2026-01-01T00:00:01Z'));
}

export async function expectGateDetails(page: Page): Promise<void> {
  await expectStyleRules(page, GATE_TYPE);
  await expectGateProviders(page);
}
