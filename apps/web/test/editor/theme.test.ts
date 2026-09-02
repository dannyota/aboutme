import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { runInNewContext } from 'node:vm';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { flushPromises } from '@vue/test-utils';

import AppRoot from '../../app/app.vue';
import ThemeToggle from '../../app/components/app/ThemeToggle.vue';
import {
  createThemeController,
  type Theme,
} from '../../app/composables/useTheme';

const themeCss = resolve(process.cwd(), 'app/assets/css/theme.css');
const nuxtConfig = resolve(process.cwd(), 'nuxt.config.ts');
const themeBootstrap = resolve(process.cwd(), 'public/theme-bootstrap.js');

registerEndpoint('/api/v1/me', {
  method: 'GET',
  handler: () => ({
    data: {
      user: {
        id: 'user-1',
        email: 'dev@aboutme.invalid',
        name: 'Dev User',
        avatarKey: null,
        hasPassword: true,
      },
      csrfToken: 'csrf',
      identities: [],
    },
  }),
});

function clearThemeCookie(): void {
  document.cookie = 'aboutme-theme=; Max-Age=0; Path=/';
}

afterEach(() => {
  clearThemeCookie();
  document.body.removeAttribute('class');
  vi.unstubAllGlobals();
});

describe('theme preference boundary', () => {
  it.each([
    { preference: undefined, systemTheme: 'light' },
    { preference: 'system', systemTheme: 'dark' },
  ] as const)(
    'uses the system $systemTheme mode when preference is $preference',
    ({ preference, systemTheme }) => {
      const cookie = ref<string | undefined>(preference);
      const controller = createThemeController(cookie, ref<Theme>(systemTheme));

      expect(controller.theme.value).toBe(systemTheme);
    },
  );

  it('persists only an exact mode when toggled', () => {
    const cookie = ref<string | undefined>(undefined);
    const controller = createThemeController(cookie, ref<Theme>('dark'));

    controller.toggleTheme();
    expect(cookie.value).toBe('light');
    expect(controller.theme.value).toBe('light');

    controller.toggleTheme();
    expect(cookie.value).toBe('dark');
    expect(controller.theme.value).toBe('dark');
  });

  it(
    'renders a labelled native control with text that states the current mode',
    async () => {
      const wrapper = await mountSuspended(ThemeToggle);

      const button = wrapper.get('[data-slot="button"]');
      expect(button.attributes('aria-label')).toMatch(/^Switch to /);
      expect(button.text()).toMatch(/(Light|Dark) mode/);
    },
  );

  it(
    'keeps the theme toggle directly beside the signed-in account control',
    async () => {
      const wrapper = await mountSuspended(AppRoot, {
        route: '/app/resumes',
      });
      await flushPromises();

      const shell = wrapper.get('[data-testid="app-shell"]');
      expect(shell.find('[data-testid="account-menu"]').exists()).toBe(true);
      expect(shell.find('[aria-label^="Switch to"]').exists()).toBe(true);
      expect(
        shell.get('[data-testid="account-menu"]').attributes('aria-label'),
      ).toContain('Account settings');
    },
    15_000,
  );

  it(
    'does not add app chrome or app-surface classes on the harness route',
    async () => {
      const fetch = vi.fn();
      vi.stubGlobal('fetch', fetch);
      const wrapper = await mountSuspended(AppRoot, {
        route: '/_harness/render',
      });

      expect(wrapper.find('[data-testid="app-shell"]').exists()).toBe(false);
      expect(document.body.classList.contains('aboutme-app-body')).toBe(false);
      expect(fetch).not.toHaveBeenCalled();
    },
  );

  it(
    'leaves the future editor detail route to its integrated top bar',
    async () => {
      const wrapper = await mountSuspended(AppRoot, {
        route: '/app/resumes/resume-1',
      });

      expect(wrapper.find('[data-testid="app-shell"]').exists()).toBe(false);
    },
    15_000,
  );

  it(
    'runs the bootstrap with only the closed cookie and system APIs',
    async () => {
      const source = await readFile(themeBootstrap, 'utf8');
      const matchMedia = vi.fn(() => ({ matches: true }));
      const document = {
        cookie: 'aboutme-theme=not-a-theme',
        documentElement: { dataset: {}, style: {} },
      };

      runInNewContext(source, { document, window: { matchMedia } });

      expect(matchMedia).toHaveBeenCalledWith('(prefers-color-scheme: dark)');
      expect(document.documentElement.dataset.theme).toBe('dark');
      expect(document.documentElement.style.colorScheme).toBe('dark');
    },
  );

  it('keeps the application tokens in the theme layer', async () => {
    const tokens = await readFile(themeCss, 'utf8');

    expect(tokens).toContain('--positive: oklch(0.508 0.118 165.612)');
    expect(tokens).toContain('--chart-1: oklch(0.845 0.143 164.978)');
  });

  it(
    'sets the document language without exposing the debug overlay',
    async () => {
      const source = await readFile(nuxtConfig, 'utf8');

      expect(source).toContain('htmlAttrs: { lang: \'en\' }');
      expect(source).toContain('devtools: { enabled: false }');
    },
  );
});
