<script setup lang="ts">
import { computed } from 'vue';
import { buttonVariants } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import AccountMenu from './AccountMenu.vue';
import ThemeToggle from './ThemeToggle.vue';

const { authState } = useAuth();
const route = useRoute();
const signedIn = computed(() => authState.value === 'authenticated');
const links = [
  { to: '/app/resumes', label: 'Resumes' },
  { to: '/app/settings/sessions', label: 'Settings' },
] as const;
const linkClass = cn(
  'rounded-md px-2.5 py-1.5 text-sm text-muted-foreground',
  'transition-colors hover:bg-accent hover:text-accent-foreground',
  'aria-[current=page]:bg-accent aria-[current=page]:text-accent-foreground',
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
    <div
      v-if="signedIn"
      class="ml-auto flex items-center gap-2"
    >
      <AccountMenu />
    </div>
    <div
      v-else
      class="ml-auto flex items-center gap-2"
    >
      <NuxtLink
        :class="buttonVariants({ variant: 'ghost', size: 'sm' })"
        to="/login"
      >Sign in</NuxtLink>
      <NuxtLink
        :class="buttonVariants({ variant: 'secondary', size: 'sm' })"
        to="/register"
      >Create account</NuxtLink>
      <ThemeToggle />
    </div>
  </header>
</template>
