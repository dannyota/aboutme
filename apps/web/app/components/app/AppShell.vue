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
const authRequiredPath = computed(() => requiresSession(route.path));
const showSignedOutLinks = computed(() => (
  authState.value !== 'authenticated' && !authRequiredPath.value
));
const localized = computed(() => isLocalizedPath(route.path));
const hidePhoneAccountLinks = localized;
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
const onTemplatesPath = computed(() =>
  route.path === '/templates' || route.path.startsWith('/templates/'));
const onGuidePath = computed(() => route.path === '/guide/mcp');
const onShowcasePath = computed(() => route.path === '/showcase');
const onResumesPath = computed(() => route.path.startsWith('/app/resumes'));
const onProjectsPath = computed(
  () => route.path.startsWith('/app/projects'),
);
const onViewsPath = computed(() => route.path.startsWith('/app/views'));
const onSettingsPath = computed(
  () => route.path.startsWith('/app/settings/sessions'),
);
const TEMPLATE_PAGE_PATH = /^\/templates\/[a-z0-9]+(?:-[a-z0-9]+)*$/u;
const onMarketingPath = computed(() => (
  route.path === '/'
  || route.path === '/templates'
  || route.path === '/terms'
  || route.path === '/privacy'
  || route.path === '/verify'
  || route.path === '/showcase'
  || route.path === '/guide/mcp'
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
const templatesLinkClass = computed(() => cn(
  linkClass,
  signedIn.value ? 'max-sm:hidden' : 'max-lg:px-2',
));
const compactLogo = computed(() => !signedIn.value);
const headerClass = computed(() => cn(
  'flex min-h-14 items-center gap-4 border-b border-border bg-card',
  'px-[max(1rem,calc((100vw-76rem)/2))]',
  compactLogo.value && 'max-lg:gap-2',
));
const guideLinkClass = computed(() => cn(
  linkClass,
  signedIn.value ? 'max-[64rem]:hidden' : 'max-[44rem]:hidden max-lg:px-2',
));
const settingsLinkClass = cn(linkClass, 'max-sm:hidden');
const viewsLinkClass = cn(linkClass, 'max-sm:hidden');
const projectsLinkClass = cn(linkClass, 'max-md:hidden');
</script>

<template>
  <header
    :class="headerClass"
    data-testid="app-shell"
  >
    <NuxtLink
      class="flex shrink-0 items-center"
      to="/"
    >
      <AppLogo
        class="max-[64rem]:hidden"
        size="sm"
      />
      <AppLogo
        class="min-[64rem]:hidden"
        mark-only
        size="sm"
      />
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
      <NuxtLink
        :aria-current="onShowcasePath ? 'page' : undefined"
        :class="templatesLinkClass"
        to="/showcase"
      >{{ copy.community }}</NuxtLink>
      <NuxtLink
        :aria-current="onGuidePath ? 'page' : undefined"
        :class="guideLinkClass"
        to="/guide/mcp"
      >{{ copy.guideMcp }}</NuxtLink>
      <template v-if="signedIn">
        <NuxtLink
          :aria-current="onResumesPath ? 'page' : undefined"
          :class="linkClass"
          to="/app/resumes"
        >{{ copy.resumes }}</NuxtLink>
        <NuxtLink
          :aria-current="onProjectsPath ? 'page' : undefined"
          :class="projectsLinkClass"
          to="/app/projects"
        >{{ copy.projects }}</NuxtLink>
        <NuxtLink
          :aria-current="onViewsPath ? 'page' : undefined"
          :class="viewsLinkClass"
          to="/app/views"
        >{{ copy.views }}</NuxtLink>
        <NuxtLink
          :aria-current="onSettingsPath ? 'page' : undefined"
          :class="settingsLinkClass"
          to="/app/settings/sessions"
        >{{ copy.settings }}</NuxtLink>
      </template>
      <a
        v-else
        :class="cn(linkClass, 'max-[72rem]:hidden')"
        data-testid="app-shell-open-source"
        href="https://github.com/dannyota/aboutme"
        rel="noopener noreferrer"
      >{{ copy.openSource }}</a>
    </nav>
    <div class="ml-auto flex items-center gap-2">
      <LocaleToggle
        v-if="localized"
        :compact="!signedIn"
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
