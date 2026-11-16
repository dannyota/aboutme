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
  THEME_COLOR,
  themeColorMeta,
  type Theme,
} from '../../app/composables/useTheme';
import { setSiteLocale } from '../support/locale';

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

  it('writes the selected mode to the browser cookie', async () => {
    const wrapper = await mountSuspended(ThemeToggle);

    await wrapper.get('[data-slot="button"]').trigger('click');
    await flushPromises();

    expect(document.cookie).toMatch(
      /(?:^|; )aboutme-theme=(?:light|dark)(?:;|$)/,
    );
  });

  it(
    'renders a 36 px circle named only by its label',
    async () => {
      const wrapper = await mountSuspended(ThemeToggle);

      const button = wrapper.get('[data-slot="button"]');
      expect(button.attributes('aria-label')).toMatch(/^Switch to /);
      expect(button.text()).toBe('');
      expect(button.classes()).toContain('size-9');
      expect(button.classes()).toContain('rounded-full');
    },
  );

  it('emits one theme-color per scheme until a theme is stored', () => {
    expect(themeColorMeta(undefined)).toEqual([
      {
        key: 'theme-color-light', name: 'theme-color',
        media: '(prefers-color-scheme: light)', content: '#ffffff',
      },
      {
        key: 'theme-color-dark', name: 'theme-color',
        media: '(prefers-color-scheme: dark)', content: '#0e0a0d',
      },
    ]);
    expect(themeColorMeta('dark')).toEqual([
      { key: 'theme-color', name: 'theme-color', content: '#0e0a0d' },
    ]);
    expect(themeColorMeta('light')).toEqual([
      { key: 'theme-color', name: 'theme-color', content: '#ffffff' },
    ]);
  });

  it('takes theme-color from the page ground of each theme', async () => {
    const css = await readFile(themeCss, 'utf8');
    const ground = (selector: string): string | undefined => {
      const start = css.indexOf(`${selector} {`);
      const body = css.slice(start, css.indexOf('}', start));
      return /--background:\s*(#[\da-f]{6});/iu.exec(body)?.[1]
        ?.toLowerCase();
    };
    expect(THEME_COLOR.light).toBe(ground(':root'));
    expect(THEME_COLOR.dark).toBe(ground('html[data-theme="dark"]'));
  });

  it(
    'switches theme-color to the chosen theme from the account menu',
    async () => {
      setSiteLocale('en');
      const wrapper = await mountSuspended(AppRoot, {
        route: '/app/resumes',
      });
      await flushPromises();
      await wrapper.get('[aria-label="Account menu"]').trigger('click');
      await flushPromises();
      const item = document.body.querySelector<HTMLElement>(
        '[data-testid="theme-toggle"]',
      );
      expect(item).not.toBeNull();
      item!.click();
      await flushPromises();

      await vi.waitFor(() => {
        const theme = document.documentElement.dataset.theme as Theme;
        expect([...document.head.querySelectorAll(
          'meta[name="theme-color"]',
        )].map((meta) => [
          meta.getAttribute('media'), meta.getAttribute('content'),
        ])).toEqual([[null, THEME_COLOR[theme]]]);
      });
      wrapper.unmount();
    },
    15_000,
  );

  it(
    'keeps the theme control inside the signed-in account menu',
    async () => {
      setSiteLocale('en');
      const wrapper = await mountSuspended(AppRoot, {
        route: '/app/resumes',
      });
      await flushPromises();

      const shell = wrapper.get('[data-testid="app-shell"]');
      expect(shell.find('[data-testid="account-menu"]').exists()).toBe(true);
      expect(shell.find('[aria-label^="Switch to"]').exists()).toBe(false);
      expect(
        shell.get('[data-testid="account-menu"]').attributes('aria-label'),
      ).toBe('Account menu');

      await shell.get('[aria-label="Account menu"]').trigger('click');
      await flushPromises();
      expect(document.body.querySelector('[data-testid="theme-toggle"]'))
        .not.toBeNull();
      wrapper.unmount();
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

  it('keeps the stamped-document tokens in the theme layer', async () => {
    const tokens = await readFile(themeCss, 'utf8');

    expect(tokens).toContain('--color-seal: var(--seal)');
    expect(tokens).toContain('--font-sans: var(--font-chrome);');
    expect(tokens).toContain(
      '--font-paper: "Be Vietnam Pro", "Inter", system-ui, sans-serif;',
    );
    expect(tokens).not.toMatch(/--positive(?:-foreground)?\s*:/);
    expect(tokens).not.toMatch(/--chart-[1-5]\s*:/);
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
