<script setup lang="ts">
import { computed } from 'vue';
import { buttonVariants } from '@/components/ui/button';
import { landingCopy, landingLocales, localeNames } from '@/landing/copy';
import { cn } from '@/lib/utils';
import AccountMenu from './AccountMenu.vue';
import ThemeToggle from './ThemeToggle.vue';

const { authState } = useAuth();
const route = useRoute();
const signedIn = computed(() => authState.value === 'authenticated');
// Only the homepage is bilingual; every other route keeps English chrome.
const onLanding = computed(() => route.path === '/');
const { locale, setLocale } = useLandingLocale();
const shellLocale = computed(() => onLanding.value ? locale.value : 'en');
const copy = computed(() => landingCopy[shellLocale.value]);
const links = [
  { to: '/app/resumes', label: 'Resumes' },
  { to: '/app/settings/sessions', label: 'Settings' },
] as const;
const linkClass = cn(
  'rounded-md px-2.5 py-1.5 text-sm text-muted-foreground',
  'transition-colors hover:bg-accent hover:text-accent-foreground',
  'aria-[current=page]:bg-accent aria-[current=page]:text-accent-foreground',
);
// State is a mark, not a hue (DESIGN.md): the chosen language is ink with an
// ink underline; the other stays pencil grey.
const localeClass = cn(
  'h-8 rounded-sm px-2 text-sm text-muted-foreground transition-colors',
  'hover:text-foreground aria-pressed:text-foreground',
  'aria-pressed:underline aria-pressed:decoration-2',
  'aria-pressed:underline-offset-[6px]',
);
</script>

<template>
  <header
    class="flex min-h-14 items-center gap-4 border-b border-border bg-card
      px-[max(1rem,calc((100vw-76rem)/2))]"
    data-testid="app-shell"
  >
    <NuxtLink
      class="text-[0.925rem] font-bold tracking-tight"
      to="/"
    >aboutme</NuxtLink>
    <nav
      v-if="signedIn"
      aria-label="Primary navigation"
      class="flex flex-1 items-center gap-1"
    >
      <NuxtLink
        v-for="link in links"
        :key="link.to"
        :aria-current="route.path.startsWith(link.to) ? 'page' : undefined"
        :class="linkClass"
        :to="link.to"
      >{{ link.label }}</NuxtLink>
    </nav>
    <div class="ml-auto flex items-center gap-2">
      <template v-if="!signedIn">
        <!-- On phones the homepage hero carries both actions. -->
        <NuxtLink
          :class="cn(
            buttonVariants({ variant: 'ghost', size: 'sm' }),
            onLanding && 'max-sm:hidden',
          )"
          to="/login"
        >{{ copy.signIn }}</NuxtLink>
        <NuxtLink
          :class="cn(
            buttonVariants({ variant: 'secondary', size: 'sm' }),
            onLanding && 'max-sm:hidden',
          )"
          to="/register"
        >{{ copy.createAccount }}</NuxtLink>
      </template>
      <div
        v-if="onLanding"
        class="flex items-center"
        role="group"
        :aria-label="copy.localeLabel"
        data-testid="landing-locale"
      >
        <template
          v-for="(option, index) in landingLocales"
          :key="option"
        >
          <span
            v-if="index > 0"
            aria-hidden="true"
            class="h-4 w-px bg-border"
          />
          <button
            type="button"
            :class="localeClass"
            :lang="option"
            :aria-pressed="locale === option"
            :data-testid="`landing-locale-${option}`"
            @click="setLocale(option)"
          >
            {{ localeNames[option] }}
          </button>
        </template>
      </div>
      <AccountMenu v-if="signedIn" />
      <ThemeToggle
        v-else
        :locale="shellLocale"
      />
    </div>
  </header>
</template>
