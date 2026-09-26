<script setup lang="ts">
import { computed } from 'vue';
import { buttonVariants } from '@/components/ui/button';
import {
  isLocalizedPath,
} from '@/i18n/locale';
import { shellCopy } from '@/i18n/shell';
import { cn } from '@/lib/utils';
import { requiresSession, validateReturnPath } from '@/utils/returnPath';
import AccountMenu from './AccountMenu.vue';
import AppLogo from './AppLogo.vue';
import LocaleToggle from './LocaleToggle.vue';
import ThemeToggle from './ThemeToggle.vue';

const { authState } = useAuth();
const route = useRoute();
const signedIn = computed(() => authState.value === 'authenticated');
// `/app/**` and `/authorize` redirect an anonymous visitor away
// (middleware/signed-in.global.ts), so a signed-out header is never this
// route's real state: it is either authenticated, waiting on `/me`, or
// already on its way out. Showing the signed-out links there at all, even
// for the one render after `/me` settles and before that redirect lands,
// would widen the header past the viewport at phone width, so this hides
// them unconditionally rather than only while loading.
const authRequiredPath = computed(() => requiresSession(route.path));
const showSignedOutLinks = computed(() => (
  authState.value !== 'authenticated' && !authRequiredPath.value
));
// Localized routes show the language control (app/i18n/locale.ts).
const localized = computed(() => isLocalizedPath(route.path));
const hidePhoneAccountLinks = localized;
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
// The gradient CTA reads "Create your resume" on the pages that sell the
// product; every other route (account flows, settings, authorize) keeps the
// generic "Create account" label (DESIGN.md; ADR 0050).
const TEMPLATE_PAGE_PATH = /^\/templates\/[a-z0-9]+(?:-[a-z0-9]+)*$/u;
const onMarketingPath = computed(() => (
  route.path === '/'
  || route.path === '/templates'
  || route.path === '/terms'
  || route.path === '/privacy'
  || route.path === '/verify'
  || TEMPLATE_PAGE_PATH.test(route.path)
));
const ctaLabel = computed(() => (onMarketingPath.value
  ? copy.value.createResume
  : copy.value.createAccount));
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
</script>

<template>
  <header
    class="flex min-h-14 items-center gap-4 border-b border-border bg-card
      px-[max(1rem,calc((100vw-76rem)/2))]"
    data-testid="app-shell"
  >
    <NuxtLink
      class="flex items-center"
      to="/"
    >
      <AppLogo size="sm" />
    </NuxtLink>
    <nav
      :aria-label="copy.primaryNavigation"
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
      <a
        v-else
        :class="cn(linkClass, 'max-[56rem]:hidden')"
        data-testid="app-shell-open-source"
        href="https://github.com/dannyota/aboutme"
        rel="noopener noreferrer"
      >{{ copy.openSource }}</a>
    </nav>
    <div class="ml-auto flex items-center gap-2">
      <LocaleToggle
        v-if="localized"
        :label="copy.localeLabel"
        test-id="landing-locale"
        @pointerdown.prevent
      />
      <AccountMenu v-if="signedIn" />
      <ThemeToggle
        v-else
        :locale="shellLocale"
      />
      <template v-if="showSignedOutLinks">
        <!-- Settings and consent have no in-page account links on phones. -->
        <NuxtLink
          :class="cn(
            buttonVariants({ variant: 'ghost', size: 'sm' }),
            hidePhoneAccountLinks && 'max-[44rem]:hidden',
          )"
          :to="signInLink"
        >{{ copy.signIn }}</NuxtLink>
        <NuxtLink
          :class="cn(
            buttonVariants({ variant: 'default', size: 'sm' }),
            hidePhoneAccountLinks && 'max-[44rem]:hidden',
          )"
          :to="createAccountLink"
        >{{ ctaLabel }}</NuxtLink>
      </template>
    </div>
  </header>
</template>
