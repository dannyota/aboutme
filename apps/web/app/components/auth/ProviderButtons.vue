<script setup lang="ts">
/**
 * `ProviderButtons` — one sign-in link per enabled provider.
 *
 * Each is a plain `<a href>` to `/api/v1/auth/{provider}/start`, never a
 * fetch: the start endpoint sets a cookie and redirects, which needs a real
 * top-level navigation. Google follows its sign-in branding guidelines: the
 * standard four-colour "G" drawn inline (no external request under the CSP),
 * Roboto Medium, and Google's light and dark button colours.
 */
import { Button } from '@/components/ui/button';
import type { LoginProvider } from '@/composables/useCapabilities';
import { authCopy } from '@/i18n/auth';
import type { Locale } from '@/i18n/locale';
import { cn } from '@/lib/utils';

const props = withDefaults(
  defineProps<{
    providers: readonly LoginProvider[];
    next?: string | null;
    locale?: Locale;
  }>(),
  { next: null, locale: 'en' },
);

const copy = computed(() => authCopy[props.locale]);

function startHref(provider: LoginProvider): string {
  const base = `/api/v1/auth/${provider}/start`;
  return props.next ? `${base}?next=${encodeURIComponent(props.next)}` : base;
}

// Google's standard "G" mark, drawn inline so the button makes no request.
const googleMark = [
  {
    fill: '#EA4335',
    d: 'M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 '
      + '0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 '
      + '17.74 9.5 24 9.5z',
  },
  {
    fill: '#4285F4',
    d: 'M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 '
      + '2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 '
      + '7.09-17.65z',
  },
  {
    fill: '#FBBC05',
    d: 'M10.53 '
      + '28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 '
      + '16.46 0 20.12 0 24c0 3.88.92 7.54 2.56 10.78l7.97-6.19z',
  },
  {
    fill: '#34A853',
    d: 'M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 '
      + '2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 '
      + '42.62 14.62 48 24 48z',
  },
] as const;

const googleClass = cn(
  'h-10 w-full rounded-[4px] border px-3 text-sm font-medium',
  'font-[family-name:Roboto]',
  'border-[#747775] bg-white text-[#1F1F1F] hover:bg-[#F2F2F2]',
  'dark:border-[#8E918F] dark:bg-[#131314] dark:text-[#E3E3E3]',
  'dark:hover:bg-[#1F1F1F]',
);
</script>

<template>
  <ul
    class="grid gap-2"
    data-testid="provider-buttons"
  >
    <li
      v-for="provider in providers"
      :key="provider"
    >
      <Button
        v-if="provider === 'google'"
        as="a"
        :class="googleClass"
        data-provider="google"
        :href="startHref(provider)"
        variant="outline"
      >
        <svg
          aria-hidden="true"
          class="size-[18px]"
          viewBox="0 0 48 48"
        >
          <path
            v-for="part in googleMark"
            :key="part.fill"
            :d="part.d"
            :fill="part.fill"
          />
        </svg>
        {{ copy.providers.google }}
      </Button>
      <Button
        v-else
        as="a"
        class="w-full"
        :data-provider="provider"
        :href="startHref(provider)"
        variant="outline"
      >
        {{ copy.providers[provider] }}
      </Button>
    </li>
  </ul>
</template>
