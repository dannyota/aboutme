import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import AppRoot from '../../app/app.vue';
import { hasSiteFooter } from '../../app/components/app/footerRoutes';
import { registerCapabilities } from '../support/capabilities';
import { setSiteLocale } from '../support/locale';
import {
  declared, RETIRED_STYLE, sfcRules, webSource,
} from '../support/sfcStyles';

const FOOTER = 'app/landing/components/HomeFooter.vue';

registerCapabilities({ providerLogin: false, agentAccess: false });
registerEndpoint('/api/v1/me', (event) => {
  setResponseStatus(event, 401);
  return { error: { code: 'session_required', message: 'Sign in.' } };
});

const mounted: { unmount(): void }[] = [];

beforeEach(() => {
  setSiteLocale(undefined);
  clearNuxtData();
});

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

async function mountApp(route: string) {
  const wrapper = await mountSuspended(AppRoot, { route });
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

describe('site footer routes', () => {
  it.each([
    '/templates',
    '/templates/',
    '/templates/ats-plain',
    '/templates/engineer-compact',
    '/showcase',
    '/guide/mcp',
    '/terms',
    '/privacy',
    '/verify',
    '/login',
    '/login/second-factor',
    '/register',
    '/forgot-password',
    '/reset-password',
    '/verify-email',
  ])('ends %s with the footer band', (path) => {
    expect(hasSiteFooter(path)).toBe(true);
  });

  it.each([
    '/',
    '/app/new',
    '/app/resumes',
    '/app/resumes/resume-1',
    '/app/projects',
    '/app/projects/project-1',
    '/app/views',
    '/app/settings/sessions',
    '/app/import/linkedin',
    '/authorize',
    '/danny',
    '/guide',
    '/templates/a/b',
    '/templates/Bad_Id',
    '/terms-of-service',
    '/_harness/render',
    '/print/resume-1',
  ])('leaves %s without the footer band', (path) => {
    expect(hasSiteFooter(path)).toBe(false);
  });
});

describe('site footer placement', () => {
  it('follows the page content on a public page, once', async () => {
    const wrapper = await mountApp('/terms');
    const footers = wrapper.findAll('[data-testid="landing-footer"]');
    expect(footers).toHaveLength(1);
    const page = wrapper.get('[data-testid="terms-page"]').element;
    const footer = footers[0]!.element;
    expect(footer.tagName).toBe('FOOTER');
    expect(page.compareDocumentPosition(footer)
      & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(page.contains(footer)).toBe(false);
    const shell = wrapper.get('[data-testid="app-shell"]').element;
    expect(shell.contains(footer)).toBe(false);
  });

  it('keeps the link order and the site language', async () => {
    const vietnamese = await mountApp('/privacy');
    const footer = vietnamese.get('[data-testid="landing-footer"]');
    expect(footer.findAll('a').map((link) => [
      link.attributes('data-testid'), link.attributes('href'), link.text(),
    ])).toEqual([
      ['landing-terms-link', '/terms', 'Điều khoản sử dụng'],
      ['landing-privacy-link', '/privacy', 'Chính sách quyền riêng tư'],
      ['landing-verify-link', '/verify', 'Kiểm chứng'],
      ['landing-guide-link', '/guide/mcp', 'Kết nối AI'],
    ]);
    expect(footer.get('[data-testid="landing-license"]').text()).toBe(
      'aboutme.vn là phần mềm mã nguồn mở, phát hành theo giấy phép '
      + 'AGPL-3.0.',
    );

    setSiteLocale('en');
    const english = await mountApp('/privacy');
    const band = english.get('[data-testid="landing-footer"]');
    expect(band.findAll('a').map((link) => link.text())).toEqual([
      'Terms of Service', 'Privacy Policy', 'Verify', 'Connect AI',
    ]);
    expect(band.get('nav').attributes('aria-label')).toBe('aboutme.vn');
  });

  it('leaves the home page with its own footer alone', async () => {
    const wrapper = await mountApp('/');
    const footers = wrapper.findAll('[data-testid="landing-footer"]');
    expect(footers).toHaveLength(1);
    expect(footers[0]!.element.closest('[data-testid="landing-page"]'))
      .not.toBeNull();
  });
});

describe('site footer band', () => {
  const rules = () => sfcRules(FOOTER);

  it('carries its own inverse ring and logo mark off the home page', () => {
    expect(declared(rules(), '.footer')).toMatchObject({
      '--ring': 'var(--ring-inverse)',
      '--logo-mark': 'var(--logo-mark-inverse)',
      'background': 'var(--footer)',
      'color': 'var(--panel-foreground)',
    });
    const at = ['@media (forced-colors: none)'];
    for (const part of ['mark', 'dot']) {
      expect(declared(rules(),
        `.footer :deep([data-logo-part="${part}"])`, at), part)
        .toEqual({ color: 'var(--logo-mark-inverse)' });
    }
  });

  it('sets the home footer type itself', () => {
    expect(declared(rules(), '.foot')).toMatchObject({
      font: '500 15px/1.6 var(--font-chrome)',
    });
  });

  it('keeps hex and retired tokens out of the footer', () => {
    for (const path of [
      FOOTER,
      'app/components/app/SiteFooter.vue',
      'app/components/app/footerRoutes.ts',
    ]) {
      expect(webSource(path), path).not.toMatch(RETIRED_STYLE);
    }
  });
});
