import type { Ref } from 'vue';
import { computed, onBeforeUnmount, onMounted, ref, watchEffect } from 'vue';

export type Theme = 'light' | 'dark';

type ThemePreference = { value: unknown };

function isTheme(value: unknown): value is Theme {
  return value === 'light' || value === 'dark';
}

/** Keeps the theme cookie boundary testable without a Nuxt app. */
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
  const systemTheme = ref<Theme>(readSystemTheme());
  const controller = createThemeController(preference, systemTheme);
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
