import type { Ref } from 'vue';
import {
  computed, onBeforeUnmount, onMounted, ref, watch, watchEffect,
} from 'vue';

export type Theme = 'light' | 'dark';

export const THEME_COLOR: Readonly<Record<Theme, string>> = {
  light: '#ffffff',
  dark: '#0e0a0d',
};

export type ThemeColorMeta = {
  readonly key: string;
  readonly name: 'theme-color';
  readonly content: string;
  readonly media?: string;
};

export function themeColorMeta(theme: Theme | undefined): ThemeColorMeta[] {
  if (theme !== undefined) {
    return [{ key: 'theme-color', name: 'theme-color',
      content: THEME_COLOR[theme] }];
  }
  return (['light', 'dark'] as const).map((scheme) => ({
    key: `theme-color-${scheme}`,
    name: 'theme-color',
    media: `(prefers-color-scheme: ${scheme})`,
    content: THEME_COLOR[scheme],
  }));
}

type ThemePreference = { value: unknown };

function isTheme(value: unknown): value is Theme {
  return value === 'light' || value === 'dark';
}

export function createThemeController(
  preference: ThemePreference,
  systemTheme: Readonly<Ref<Theme>>,
) {
  const theme = computed<Theme>(() => isTheme(preference.value)
    ? preference.value
    : systemTheme.value);

  function toggleTheme(): void {
    preference.value = theme.value === 'dark' ? 'light' : 'dark';
  }

  return { theme, toggleTheme };
}

export function useStoredTheme() {
  return useState<Theme | undefined>('aboutme-stored-theme', () => {
    const cookie = useCookie<Theme | undefined>('aboutme-theme', {
      default: () => undefined,
      path: '/',
      sameSite: 'lax',
      watch: false,
    });
    return isTheme(cookie.value) ? cookie.value : undefined;
  });
}

function readSystemTheme(): Theme {
  if (!import.meta.client) return 'light';
  return window.matchMedia('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light';
}

export function useTheme() {
  const preference = useCookie<Theme | undefined>('aboutme-theme', {
    default: () => undefined,
    path: '/',
    sameSite: 'lax',
    watch: 'shallow',
  });
  const stored = useStoredTheme();
  const systemTheme = ref<Theme>(readSystemTheme());
  const controller = createThemeController(preference, systemTheme);
  watch(preference, (value) => {
    stored.value = isTheme(value) ? value : undefined;
  });
  let mediaQuery: MediaQueryList | undefined;
  const updateSystemTheme = (): void => {
    systemTheme.value = mediaQuery?.matches ? 'dark' : 'light';
  };

  watchEffect(() => {
    if (!import.meta.client) return;
    document.documentElement.dataset.theme = controller.theme.value;
    document.documentElement.style.colorScheme = controller.theme.value;
  });

  onMounted(() => {
    mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    mediaQuery.addEventListener('change', updateSystemTheme);
    updateSystemTheme();
  });

  onBeforeUnmount(() => mediaQuery?.removeEventListener(
    'change',
    updateSystemTheme,
  ));

  return controller;
}
