<script setup lang="ts">
import AccountControl from './AccountControl.vue';
import ThemeToggle from './ThemeToggle.vue';

// Until /me resolves the shell is the signed-out variant (design: web,
// "Application surfaces"); an app route redirects on its own if the session
// is absent, so this never shows a signed-in link to an anonymous visitor.
const { authState } = useAuth();
const signedIn = computed(() => authState.value === 'authenticated');
</script>

<template>
  <header class="app-chrome">
    <NuxtLink
      class="app-brand"
      to="/"
    >
      aboutme
    </NuxtLink>
    <nav
      v-if="signedIn"
      class="app-nav"
      aria-label="Primary navigation"
    >
      <NuxtLink to="/app/resumes"> Resumes </NuxtLink>
      <NuxtLink to="/app/settings/sessions"> Settings </NuxtLink>
    </nav>
    <div
      v-if="signedIn"
      class="app-account-actions"
    >
      <AccountControl />
      <ThemeToggle />
    </div>
    <div
      v-else
      class="app-account-actions"
    >
      <NuxtLink
        class="app-entry-link"
        to="/login"
      >
        Sign in
      </NuxtLink>
      <NuxtLink
        class="app-entry-link app-entry-link--primary"
        to="/register"
      >
        Create account
      </NuxtLink>
      <ThemeToggle />
    </div>
  </header>
</template>
