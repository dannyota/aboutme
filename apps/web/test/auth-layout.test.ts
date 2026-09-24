import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { beforeEach, describe, expect, it } from 'vitest';

import LoginPage from '../app/pages/login.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

// `AuthLayout` frames every account page with a form-first shell and a
// brand panel shown only from 1024 px (DESIGN.md "Authenticated chrome and
// editor"; ADR 0050 keeps this chrome off the renderer).

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
      'CV của bạn luôn riêng tư cho đến khi bạn đăng.',
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
