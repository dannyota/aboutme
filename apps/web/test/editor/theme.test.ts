import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { runInNewContext } from 'node:vm';
import { mountSuspended } from '@nuxt/test-utils/runtime';
import { ref } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import AppRoot from '../../app/app.vue';
import ThemeToggle from '../../app/components/ui/ThemeToggle.vue';
import {
  createThemeController,
  type Theme,
} from '../../app/composables/useTheme';

const appCss = resolve(process.cwd(), 'app/assets/css/app.css');
const editorCss = resolve(process.cwd(), 'app/assets/css/editor.css');
const themeBootstrap = resolve(process.cwd(), 'public/theme-bootstrap.js');

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

      const button = wrapper.get('button');
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

      const actions = wrapper.get('.app-account-actions');
      const controls = actions.findAll(':scope > *');

      expect(controls).toHaveLength(2);
      expect(controls[0]?.classes()).toContain('account-control');
      expect(controls[1]?.attributes('aria-label')).toMatch(/^Switch to /);
      expect(actions.get('.account-control').attributes('aria-label'))
        .toContain('Account settings');
    },
  );

  it.each(['/_harness/render', '/print/resume-1', '/public-resume'])(
    'does not add app chrome or app-surface classes on bare route %s',
    async (route) => {
      const fetch = vi.fn();
      vi.stubGlobal('fetch', fetch);
      const wrapper = await mountSuspended(AppRoot, { route });

      expect(wrapper.find('.aboutme-app').exists()).toBe(false);
      expect(wrapper.find('.app-chrome').exists()).toBe(false);
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

      expect(wrapper.find('.aboutme-app').exists()).toBe(true);
      expect(wrapper.find('.app-chrome').exists()).toBe(false);
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

  it(
    'keeps app styling away from the renderer and scopes focus styling',
    async () => {
      const css = await readFile(appCss, 'utf8');

      expect(css).not.toMatch(/\.resume-(document|page)\b/);
      expect(css).toContain('.aboutme-app :focus-visible');
      expect(css).not.toMatch(
        /^(?:\.app-|\.theme-toggle|\.login|\.resume-list)/m,
      );
      expect(css).not.toContain('body.aboutme-app-body');
      expect(css).toContain('.editor-topbar');
      expect(css).not.toContain('.editor-chrome');
      expect(css).toContain('--positive: oklch(0.596 0.145 163.225)');
      expect(css).toContain('--chart-1: oklch(0.845 0.143 164.978)');
    },
  );

  it('uses Nova control states and readable inspector groups', async () => {
    const css = await readFile(editorCss, 'utf8');

    expect(css).toContain(
      '.editor-view-switcher button[aria-pressed="true"]',
    );
    expect(css).toContain('.editor-inspector fieldset');
    expect(css).toContain('.editor-inspector ol');
    expect(css).toContain('.editor-inspector input[type="checkbox"]');
  });
});
