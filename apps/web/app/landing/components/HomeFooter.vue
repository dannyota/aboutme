<script setup lang="ts">
import AppLogo from '@/components/app/AppLogo.vue';
import type { LegalCopy } from '@/i18n/legal';

import type { LandingCopy } from '../copy';

defineProps<{
  readonly copy: LandingCopy;
  readonly legal: LegalCopy;
}>();
</script>

<template>
  <footer
    class="footer home-inverse"
    data-testid="landing-footer"
  >
    <div class="foot">
      <AppLogo class="foot-logo" />
      <nav :aria-label="copy.footer.navLabel">
        <NuxtLink
          data-testid="landing-terms-link"
          to="/terms"
        >{{ legal.termsLink }}</NuxtLink>
        <NuxtLink
          data-testid="landing-privacy-link"
          to="/privacy"
        >{{ legal.privacyLink }}</NuxtLink>
        <NuxtLink
          data-testid="landing-verify-link"
          to="/verify"
        >{{ legal.verifyLink }}</NuxtLink>
        <NuxtLink
          data-testid="landing-guide-link"
          to="/guide/mcp"
        >{{ legal.guideLink }}</NuxtLink>
      </nav>
      <p data-testid="landing-license">
        {{ copy.footer.license }}
      </p>
    </div>
  </footer>
</template>

<style scoped>
.footer {
  --ring: var(--ring-inverse);
  --logo-mark: var(--logo-mark-inverse);
  padding: 44px 0 40px;
  background: var(--footer);
  color: var(--panel-foreground);
}

@media (forced-colors: none) {
  .footer :deep([data-logo-part="mark"]),
  .footer :deep([data-logo-part="dot"]) {
    color: var(--logo-mark-inverse);
  }
}

.foot {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px 28px;
  max-width: 1264px;
  margin: 0 auto;
  padding: 0 16px;
  font: 500 15px/1.6 var(--font-chrome);
}

.foot-logo {
  height: 28px;
}

nav {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 26px;
}

nav a {
  text-decoration: none;
}

nav a:hover {
  text-decoration: underline;
  text-underline-offset: 4px;
}

p {
  width: 100%;
  margin-top: 18px;
  padding-top: 20px;
  border-top: 1px solid var(--hairline-inverse);
  color: var(--panel-muted);
  font-size: 14px;
  line-height: 1.5;
}

@media (min-width: 48rem) {
  .foot {
    padding: 0 24px;
  }
}

@media (min-width: 64rem) {
  .foot {
    padding: 0 32px;
  }
}
</style>
