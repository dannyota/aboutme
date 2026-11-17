<script setup lang="ts">
import { computed, nextTick, provide, ref } from 'vue';
import { guideCopy, mcpServerUrl } from '@/i18n/guide';
import { pageTitle } from '@/i18n/meta';
import GuideCanCannot from '@/components/guide/GuideCanCannot.vue';
import GuideClaudeSteps from '@/components/guide/GuideClaudeSteps.vue';
import GuideCopyBlock from '@/components/guide/GuideCopyBlock.vue';
import GuideRichText from '@/components/guide/GuideRichText.vue';
import GuideTroubleshooting from '@/components/guide/GuideTroubleshooting.vue';
import { verifyAnnounceKey } from '@/components/verify/verifyAnnounce';

const { locale } = useLocale();
const copy = computed(() => guideCopy[locale.value]);

useSiteSeo(() => ({
  path: '/guide/mcp',
  title: pageTitle(copy.value.title),
  description: copy.value.description,
  locale: locale.value,
}));

const announcement = ref('');
// Clear first: a live region announces only changes (WAI-ARIA).
provide(verifyAnnounceKey, (message: string) => {
  announcement.value = '';
  void nextTick(() => {
    announcement.value = message;
  });
});
</script>

<template>
  <main
    class="mx-auto flex w-full max-w-[60rem] flex-col gap-8 px-4 pt-8 pb-16
      min-[42rem]:gap-10 min-[42rem]:px-6 min-[42rem]:pt-12 min-[42rem]:pb-24"
    data-testid="guide-mcp-page"
  >
    <p
      aria-live="polite"
      class="sr-only"
      role="status"
    >
      {{ announcement }}
    </p>

    <header
      class="rounded-[var(--radius-panel)] bg-surface-blush px-6 py-8
        md:p-12"
      data-testid="guide-header"
    >
      <h1
        class="text-2xl font-extrabold tracking-[-0.02em] md:text-3xl"
        data-page-title
      >
        {{ copy.header.h1 }}
      </h1>
      <p class="mt-4 max-w-[40rem] text-md text-muted-foreground">
        {{ copy.header.lead }}
      </p>
      <p class="mt-3 max-w-[40rem] text-md">
        {{ copy.header.explainer }}
      </p>
      <p class="mt-4 text-[13px] font-medium text-muted-foreground">
        {{ copy.header.urlLabel }}
      </p>
      <GuideCopyBlock
        class="mt-1.5 max-w-[32rem]"
        :copied-text="copy.header.urlCopied"
        :copy-failed-text="copy.header.copyFailed"
        :copy-label="copy.header.copyUrlLabel"
        :scroll-label="copy.header.urlLabel"
        testid="guide-url"
        :text="mcpServerUrl"
      />
      <p class="mt-4 text-[13px] text-muted-foreground">
        <GuideRichText :line="copy.header.account" />
      </p>
    </header>

    <GuideCanCannot :copy="copy.canCannot" />

    <GuideClaudeSteps
      :copied-text="copy.claude.commandCopied"
      :copy="copy.claude"
      :copy-failed-text="copy.claude.commandCopyFailed"
    />

    <section
      aria-labelledby="guide-try-heading"
      class="rounded-[var(--radius-card)] border bg-card p-5
        min-[42rem]:p-7"
      data-testid="guide-try-request"
    >
      <h2
        id="guide-try-heading"
        class="text-lg font-bold"
      >
        {{ copy.tryRequest.heading }}
      </h2>
      <ul class="mt-3 space-y-3">
        <li
          v-for="(example, index) in copy.tryRequest.examples"
          :key="index"
          class="rounded-[10px] bg-muted p-3 text-md"
        >
          {{ example }}
        </li>
      </ul>
      <p class="mt-3 text-[13px] text-muted-foreground">
        {{ copy.tryRequest.after }}
      </p>
    </section>

    <section
      aria-labelledby="guide-privacy-heading"
      class="rounded-[var(--radius-card)] border bg-card p-5
        min-[42rem]:p-7"
      data-testid="guide-privacy"
    >
      <h2
        id="guide-privacy-heading"
        class="text-lg font-bold"
      >
        {{ copy.privacy.heading }}
      </h2>
      <ul class="mt-3 space-y-2">
        <li
          v-for="(point, index) in copy.privacy.points"
          :key="index"
          class="text-md"
        >
          <GuideRichText :line="point" />
        </li>
      </ul>
    </section>

    <section
      aria-labelledby="guide-other-apps-heading"
      class="rounded-[var(--radius-card)] border bg-card p-5
        min-[42rem]:p-7"
      data-testid="guide-other-apps"
    >
      <h2
        id="guide-other-apps-heading"
        class="text-lg font-bold"
      >
        {{ copy.otherApps.heading }}
      </h2>
      <p class="mt-2 text-md">
        <GuideRichText :line="copy.otherApps.requirements" />
      </p>
      <p class="mt-2 text-md text-muted-foreground">
        <GuideRichText :line="copy.otherApps.limits" />
      </p>
    </section>

    <GuideTroubleshooting :copy="copy.troubleshooting" />
  </main>
</template>
