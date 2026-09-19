<script setup lang="ts">
import { computed } from 'vue';
import { Button, buttonVariants } from '@/components/ui/button';
import {
  isLocalizedPath,
  localeNames,
  localeShortNames,
  locales,
} from '@/i18n/locale';
import { shellCopy } from '@/i18n/shell';
import { cn } from '@/lib/utils';
import { validateReturnPath } from '@/utils/returnPath';
import AccountMenu from './AccountMenu.vue';
import ThemeToggle from './ThemeToggle.vue';

const { authState } = useAuth();
const route = useRoute();
const signedIn = computed(() => authState.value === 'authenticated');
// Only the homepage and account pages are bilingual; other routes stay
// English (app/i18n/locale.ts).
const localized = computed(() => isLocalizedPath(route.path));
// On the account pages themselves, carry a validated next along so a click
// on the header's other link does not drop it (login.vue and register.vue
// carry it the same way on their own cross-link).
const explicitNext = computed(() => (
  route.path === '/login' || route.path === '/register'
    ? validateReturnPath(route.query.next)
    : null
));
const signInLink = computed(() => (explicitNext.value
  ? `/login?next=${encodeURIComponent(explicitNext.value)}`
  : '/login'));
const createAccountLink = computed(() => (explicitNext.value
  ? `/register?next=${encodeURIComponent(explicitNext.value)}`
  : '/register'));
const { locale, setLocale } = useLocale();
const shellLocale = useRouteLocale();
const copy = computed(() => shellCopy[shellLocale.value]);
// The gallery is public, so its link renders for signed-out and signed-in
// visitors alike, unlike the account-only links below.
const onTemplatesPath = computed(() =>
  route.path === '/templates' || route.path.startsWith('/templates/'));
const onResumesPath = computed(() => route.path.startsWith('/app/resumes'));
const onSettingsPath = computed(
  () => route.path.startsWith('/app/settings/sessions'),
);
const linkClass = cn(
  'rounded-md px-2.5 py-1.5 text-sm text-muted-foreground',
  'transition-colors hover:bg-accent hover:text-accent-foreground',
  'aria-[current=page]:bg-accent aria-[current=page]:text-accent-foreground',
);
// On a signed-in phone the header cannot fit every link beside the locale
// toggle and account menu, so Templates drops there; the home page still
// links the gallery. Signed out, it is the header's only gallery link.
const templatesLinkClass = computed(() =>
  cn(linkClass, signedIn.value && 'max-sm:hidden'));
// Settings is also one tap away from the account menu, so it is the other
// link to drop on phones when signed in.
const settingsLinkClass = cn(linkClass, 'max-sm:hidden');
// State is a mark, not a hue (DESIGN.md): the chosen language is ink with an
// ink underline; the other stays pencil grey.
const localeClass = cn(
  'px-2 font-normal text-muted-foreground hover:text-foreground',
  'aria-pressed:text-foreground aria-pressed:underline',
  'aria-pressed:decoration-2 aria-pressed:underline-offset-[6px]',
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
      aria-label="Primary navigation"
      class="flex flex-1 items-center gap-1"
    >
      <NuxtLink
        :aria-current="onTemplatesPath ? 'page' : undefined"
        :class="templatesLinkClass"
        to="/templates"
      >{{ copy.templates }}</NuxtLink>
      <template v-if="signedIn">
        <NuxtLink
          :aria-current="onResumesPath ? 'page' : undefined"
          :class="linkClass"
          to="/app/resumes"
        >{{ copy.resumes }}</NuxtLink>
        <!-- The account menu also opens Settings, so it stays reachable on
             phones with this link hidden. -->
        <NuxtLink
          :aria-current="onSettingsPath ? 'page' : undefined"
          :class="settingsLinkClass"
          to="/app/settings/sessions"
        >{{ copy.settings }}</NuxtLink>
      </template>
    </nav>
    <div class="ml-auto flex items-center gap-2">
      <template v-if="!signedIn">
        <!-- On phones these pages carry their own account links. -->
        <NuxtLink
          :class="cn(
            buttonVariants({ variant: 'ghost', size: 'sm' }),
            localized && 'max-sm:hidden',
          )"
          :to="signInLink"
        >{{ copy.signIn }}</NuxtLink>
        <NuxtLink
          :class="cn(
            buttonVariants({ variant: 'secondary', size: 'sm' }),
            localized && 'max-sm:hidden',
          )"
          :to="createAccountLink"
        >{{ copy.createAccount }}</NuxtLink>
      </template>
      <div
        v-if="localized"
        class="flex items-center"
        role="group"
        :aria-label="copy.localeLabel"
        data-testid="landing-locale"
      >
        <template
          v-for="(option, index) in locales"
          :key="option"
        >
          <span
            v-if="index > 0"
            aria-hidden="true"
            class="h-4 w-px bg-border"
          />
          <Button
            type="button"
            :aria-label="localeNames[option]"
            :class="localeClass"
            :lang="option"
            :aria-pressed="locale === option"
            :data-testid="`landing-locale-${option}`"
            size="sm"
            variant="link"
            @click="setLocale(option)"
          >
            <!-- Full names return at sm; the aria-label above keeps the same
                 accessible name at every width. -->
            <span
              aria-hidden="true"
              class="sm:hidden"
            >{{ localeShortNames[option] }}</span>
            <span
              aria-hidden="true"
              class="hidden sm:inline"
            >{{ localeNames[option] }}</span>
          </Button>
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
