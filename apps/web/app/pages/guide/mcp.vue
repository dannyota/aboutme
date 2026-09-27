<script setup lang="ts">
/**
 * /guide/mcp: "Kết nối trợ lý AI với aboutme.vn" / "Connect your AI
 * assistant to aboutme.vn" (docs/design/mcp-guide.md). Server-rendered,
 * reads no API, and shows the same content signed in or out. Sections
 * follow "Page structure" in order: header block, what the assistant can
 * do, Connect Claude, try a request, privacy and control, other apps, and
 * troubleshooting.
 */
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
// Clearing first lets a second identical message announce again, the same
// pattern the verify page uses for its one shared live region.
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
      class="rounded-[var(--radius-feature)] border bg-surface-blue p-5
        min-[42rem]:p-7"
      data-testid="guide-header"
    >
      <h1
        class="text-xl font-bold min-[42rem]:text-2xl"
        data-page-title
      >
        {{ copy.header.h1 }}
      </h1>
      <p class="mt-2 max-w-[40rem] text-md text-muted-foreground">
        {{ copy.header.lead }}
      </p>
      <p class="mt-3 max-w-[40rem] text-[15px]">
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
      :copy-failed-text="copy.header.copyFailed"
    />

    <section
      aria-labelledby="guide-try-heading"
      class="rounded-[var(--radius-feature)] border bg-card p-5
        shadow-[var(--shadow-product)] min-[42rem]:p-7"
      data-testid="guide-try-request"
    >
      <h2
        id="guide-try-heading"
        class="text-[18px] font-semibold"
      >
        {{ copy.tryRequest.heading }}
      </h2>
      <ul class="mt-3 space-y-3">
        <li
          v-for="(example, index) in copy.tryRequest.examples"
          :key="index"
          class="border-l-2 border-brand-indigo bg-muted p-3 text-[15px]"
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
      class="rounded-[var(--radius-feature)] border bg-card p-5
        shadow-[var(--shadow-product)] min-[42rem]:p-7"
      data-testid="guide-privacy"
    >
      <h2
        id="guide-privacy-heading"
        class="text-[18px] font-semibold"
      >
        {{ copy.privacy.heading }}
      </h2>
      <ul class="mt-3 space-y-2">
        <li
          v-for="(point, index) in copy.privacy.points"
          :key="index"
          class="text-[15px]"
        >
          <GuideRichText :line="point" />
        </li>
      </ul>
    </section>

    <section
      aria-labelledby="guide-other-apps-heading"
      class="rounded-[var(--radius-feature)] border bg-card p-5
        shadow-[var(--shadow-product)] min-[42rem]:p-7"
      data-testid="guide-other-apps"
    >
      <h2
        id="guide-other-apps-heading"
        class="text-[18px] font-semibold"
      >
        {{ copy.otherApps.heading }}
      </h2>
      <p class="mt-2 text-[15px]">
        <GuideRichText :line="copy.otherApps.requirements" />
      </p>
      <p class="mt-2 text-[15px] text-muted-foreground">
        <GuideRichText :line="copy.otherApps.limits" />
      </p>
    </section>

    <GuideTroubleshooting :copy="copy.troubleshooting" />
  </main>
</template>
