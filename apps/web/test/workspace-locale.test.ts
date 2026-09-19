import { mountSuspended } from '@nuxt/test-utils/runtime';
import { describe, expect, it } from 'vitest';

import LocaleToggle from '../app/components/app/LocaleToggle.vue';
import {
  defaultLocale,
  isLocalizedPath,
  localeCookie,
} from '../app/i18n/locale';
import { workspaceTitles } from '../app/i18n/meta';
import { workspaceCopy } from '../app/i18n/workspace';
import { setSiteLocale } from './support/locale';

describe('workspace locale', () => {
  it.each([
    '/app/resumes',
    '/app/resumes/',
    '/app/new',
    '/app/new/',
    '/app/resumes/resume-1',
    '/app/resumes/resume-1/',
    '/app/resumes//',
  ])('localizes %s', (path) => {
    expect(isLocalizedPath(path)).toBe(true);
  });

  it.each([
    '/app',
    '/app/settings/sessions',
    '/authorize',
    '/app/resumes/resume-1/history',
    '/app/resumes/../settings',
    '/app/resumes/resume-1/..',
    '/app/resumes/%2e%2e',
    '/app/resumes/resume%2f1',
    '/app/resumes/resume\\1',
    '/app/resumes/resume-1?next=/app/new',
    '/app/resumes/resume-1#section',
    '/app/resumes-lookalike/resume-1',
  ])('keeps %s English-only', (path) => {
    expect(isLocalizedPath(path)).toBe(false);
  });

  it('uses Vietnamese for an invalid locale cookie on a workspace route',
    async () => {
      setSiteLocale('fr');
      const wrapper = await mountSuspended(LocaleToggle, {
        props: { label: 'Ngôn ngữ', testId: 'workspace-locale' },
        route: '/app/resumes',
      });

      expect(document.cookie).toContain(`${localeCookie}=fr`);
      expect(defaultLocale).toBe('vi');
      expect(wrapper.get('[data-testid="workspace-locale-vi"]')
        .attributes('aria-pressed')).toBe('true');
    });

  it('keeps both workspace catalogs aligned', () => {
    expect(Object.keys(workspaceCopy.vi)).toEqual(
      Object.keys(workspaceCopy.en),
    );
    expect(Object.keys(workspaceTitles.vi)).toEqual(
      Object.keys(workspaceTitles.en),
    );
  });
});

describe('modal locale controls', () => {
  it('omits test identifiers when a modal locale toggle has none', async () => {
    const wrapper = await mountSuspended(LocaleToggle, {
      props: { label: 'Language' },
      route: '/app/resumes',
    });

    expect(wrapper.attributes('data-testid')).toBeUndefined();
    expect(wrapper.findAll('[data-testid]')).toHaveLength(0);
  });
});
