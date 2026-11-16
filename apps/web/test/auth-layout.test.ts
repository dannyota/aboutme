import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { beforeEach, describe, expect, it } from 'vitest';

import LoginPage from '../app/pages/login.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

registerCapabilities();

const appRoot = join(process.cwd(), 'app');
const FOCUSABLE
  = 'a[href], button, input, select, textarea, [tabindex]';
const authPages = [
  'pages/login.vue',
  'pages/register.vue',
  'pages/forgot-password.vue',
  'pages/reset-password.vue',
  'pages/verify-email.vue',
  'pages/login/second-factor.vue',
] as const;
const testids: Readonly<Record<(typeof authPages)[number], string>> = {
  'pages/login.vue': 'login-page',
  'pages/register.vue': 'register-page',
  'pages/forgot-password.vue': 'forgot-password-page',
  'pages/reset-password.vue': 'reset-password-page',
  'pages/verify-email.vue': 'verify-email-page',
  'pages/login/second-factor.vue': 'second-factor-page',
};

describe('AuthLayout', () => {
  beforeEach(() => setSiteLocale('en'));

  it('shows the English brand statement', async () => {
    const wrapper = await mountSuspended(LoginPage);
    await flushPromises();
    expect(wrapper.get('[data-testid="auth-brand-panel"]').text()).toContain(
      'Your resume stays private until you publish it.',
    );
  });

  it('shows the Vietnamese brand statement', async () => {
    setSiteLocale('vi');
    const wrapper = await mountSuspended(LoginPage);
    await flushPromises();
    expect(wrapper.get('[data-testid="auth-brand-panel"]').text()).toContain(
      'CV của bạn luôn riêng tư cho đến khi bạn xuất bản.',
    );
  });

  it('places the brand panel inside login-page, after the form, hidden by '
    + 'default, shown from lg, with no focusable element', async () => {
    const wrapper = await mountSuspended(LoginPage);
    await flushPromises();

    const page = wrapper.get('[data-testid="login-page"]');
    const form = page.get('[data-testid="login-form"]');
    const panel = page.get('[data-testid="auth-brand-panel"]');

    expect(
      (form.element.compareDocumentPosition(panel.element)
        & Node.DOCUMENT_POSITION_FOLLOWING) !== 0,
    ).toBe(true);
    expect(panel.classes()).toContain('hidden');
    expect(panel.classes()).toContain('lg:flex');
    expect(panel.findAll(FOCUSABLE)).toHaveLength(0);
  });
});

describe('AuthLayout identity', () => {
  beforeEach(() => setSiteLocale('en'));

  it('lays out the form beside a panel with no outer card', async () => {
    const wrapper = await mountSuspended(LoginPage);
    await flushPromises();
    const page = wrapper.get('[data-testid="login-page"]');
    const main = page.classes();

    for (const name of [
      'max-w-[26rem]', 'lg:grid', 'lg:max-w-[79rem]',
      'lg:grid-cols-[5fr_6fr]',
    ]) {
      expect(main, name).toContain(name);
    }
    for (const node of [page, ...page.findAll('div')]) {
      const classes = node.classes().join(' ');
      expect(classes).not.toMatch(
        /shadow-\[var\(--shadow-product\)\]|radius-feature|lg:bg-card/u,
      );
      expect(classes).not.toMatch(/(?:^|\s)lg:border(?:\s|$)/u);
    }
    const form = page.get('[data-auth-form]');
    expect(form.classes()).toContain('lg:col-start-2');
    expect(form.get('div').classes()).toContain('max-w-[26rem]');
  });

  it('paints the brand panel on --panel with the panel radius', async () => {
    const wrapper = await mountSuspended(LoginPage);
    await flushPromises();
    const panel = wrapper.get('[data-testid="auth-brand-panel"]');

    for (const name of [
      'bg-panel', 'text-panel-foreground', 'rounded-[var(--radius-panel)]',
      'overflow-hidden', 'hidden', 'lg:flex', 'lg:col-start-1',
    ]) {
      expect(panel.classes(), name).toContain(name);
    }
    expect(panel.get('[data-logo-size]').attributes('data-logo-size'))
      .toBe('md');
    expect(panel.find('[data-app-seal]').exists()).toBe(false);
    const statement = panel.get('[data-auth-statement]');
    for (const name of ['text-2xl', 'font-extrabold', 'tracking-[-0.02em]']) {
      expect(statement.classes(), name).toContain(name);
    }
    const points = panel.findAll('li');
    expect(points).toHaveLength(3);
    for (const point of points) {
      const coin = point.get('[data-auth-coin]');
      for (const name of [
        'size-7', 'rounded-full', 'bg-coin', 'text-coin-ink',
      ]) {
        expect(coin.classes(), name).toContain(name);
      }
      expect(coin.find('svg').exists()).toBe(true);
    }
    const art = panel.get('[data-auth-illustration]');
    expect(art.attributes('aria-hidden')).toBe('true');
    expect(art.get('[data-auth-sheet]').classes()).toContain('bg-paper');
    expect(art.get('[data-auth-sheet]').classes()).toContain('-rotate-3');
    expect(art.get('[data-auth-lock]').classes()).toContain('bg-coin');
    expect(art.classes()).toContain('mt-auto');
    expect(art.classes()).not.toContain('absolute');
    expect(art.element.previousElementSibling?.tagName).toBe('UL');
    expect(art.element.nextElementSibling).toBeNull();
  });

  it('gives the panel logo its inverse mark and drops removed tokens', () => {
    const source = readFileSync(
      join(appRoot, 'components/auth/AuthLayout.vue'), 'utf8',
    );
    expect(source).toContain('color: var(--logo-mark-inverse);');
    expect(source).not.toMatch(
      /surface-blue|brand-indigo|brand-ochre|gradient|bg-white/u,
    );
    expect(source).not.toMatch(/shadow-product|radius-feature/u);
    expect(source).not.toMatch(/#[\da-f]{3,8}\b/iu);
  });

  it('sets every heading at 32/1.3 weight 800 with no rule and full-width '
    + '48 px submit pills', () => {
    for (const path of authPages) {
      const source = readFileSync(join(appRoot, path), 'utf8');
      const heading = /<h1\s+class="([^"]+)"/u.exec(source)?.[1] ?? '';
      expect(heading, path).toBe('text-2xl font-extrabold tracking-[-0.02em]');
      expect(source, path).not.toMatch(/\bh-9\b/u);
      for (const match of source.matchAll(/<Button\s[^>]*?type="submit"/gu)) {
        expect(match[0], path).toMatch(/class="[^"]*\bh-12 w-full\b/u);
      }
    }
  });
});

describe('auth page source guard', () => {
  it('uses AuthLayout with its page testid and drops text-primary', () => {
    for (const path of authPages) {
      const source = readFileSync(join(appRoot, path), 'utf8');
      expect(source, path).toContain(`testid="${testids[path]}"`);
      expect(source, path).toMatch(/<AuthLayout\b/u);
      expect(source, path).not.toContain('text-primary');
    }
  });
});
