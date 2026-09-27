<script setup lang="ts">
/**
 * "Connect Claude" (docs/design/mcp-guide.md, "Page structure" and
 * "Layout"): an `h3` for Claude on the web, desktop, and mobile as a six-
 * step ordered list and a plan note, then an `h3` for Claude Code as three
 * steps with two command blocks. Steps use 24 px number disks:
 * `--surface-blue` fill, a 1 px `--border` ring, and `--foreground` 13 px
 * weight-600 numerals; the ring keeps the disk visible under
 * `forced-colors: active`. Each list numbers from 1.
 */
import type { GuideCopy } from '@/i18n/guide';
import GuideCopyBlock from './GuideCopyBlock.vue';
import GuideRichText from './GuideRichText.vue';

defineProps<{
  readonly copy: GuideCopy['claude'];
  readonly copiedText: string;
  readonly copyFailedText: string;
}>();
</script>

<template>
  <section
    aria-labelledby="guide-claude-heading"
    class="rounded-[var(--radius-feature)] border bg-card p-5
      shadow-[var(--shadow-product)] min-[42rem]:p-7"
    data-testid="guide-claude"
  >
    <h2
      id="guide-claude-heading"
      class="text-[18px] font-semibold"
    >
      {{ copy.heading }}
    </h2>
    <p class="mt-1 max-w-[40rem] text-[15px] text-muted-foreground">
      {{ copy.intro }}
    </p>

    <h3 class="mt-5 text-[15px] font-semibold">
      {{ copy.webHeading }}
    </h3>
    <ol class="mt-3 space-y-3">
      <li
        v-for="(step, index) in copy.webSteps"
        :key="index"
        class="flex gap-3"
      >
        <span
          class="guide-disk flex size-6 flex-none items-center justify-center
            rounded-full border border-border bg-surface-blue text-[13px]
            font-semibold text-foreground"
          data-testid="guide-claude-web-disk"
        >{{ index + 1 }}</span>
        <p class="min-w-0 flex-1 text-[15px]">
          <GuideRichText :line="step" />
        </p>
      </li>
    </ol>
    <p
      class="mt-3 text-[13px] text-muted-foreground"
      data-testid="guide-plan-note"
    >
      <GuideRichText :line="copy.planNote" />
    </p>

    <h3 class="mt-6 text-[15px] font-semibold">
      {{ copy.codeHeading }}
    </h3>
    <ol class="mt-3 space-y-4">
      <li class="flex gap-3">
        <span
          class="guide-disk flex size-6 flex-none items-center justify-center
            rounded-full border border-border bg-surface-blue text-[13px]
            font-semibold text-foreground"
        >{{ 1 }}</span>
        <div class="min-w-0 flex-1">
          <p class="text-[15px]">
            <GuideRichText :line="copy.addStep.line" />
          </p>
          <GuideCopyBlock
            class="mt-2"
            :copied-text="copiedText"
            :copy-failed-text="copyFailedText"
            :copy-label="copy.copyCommandLabel"
            prompt
            :scroll-label="copy.addStep.scrollLabel"
            testid="guide-command-add"
            :text="copy.addStep.command"
          />
        </div>
      </li>
      <li class="flex gap-3">
        <span
          class="guide-disk flex size-6 flex-none items-center justify-center
            rounded-full border border-border bg-surface-blue text-[13px]
            font-semibold text-foreground"
        >{{ 2 }}</span>
        <div class="min-w-0 flex-1">
          <p class="text-[15px]">
            <GuideRichText :line="copy.loginStep.line" />
          </p>
          <GuideCopyBlock
            class="mt-2"
            :copied-text="copiedText"
            :copy-failed-text="copyFailedText"
            :copy-label="copy.copyCommandLabel"
            prompt
            :scroll-label="copy.loginStep.scrollLabel"
            testid="guide-command-login"
            :text="copy.loginStep.command"
          />
        </div>
      </li>
      <li class="flex gap-3">
        <span
          class="guide-disk flex size-6 flex-none items-center justify-center
            rounded-full border border-border bg-surface-blue text-[13px]
            font-semibold text-foreground"
        >{{ 3 }}</span>
        <p class="min-w-0 flex-1 text-[15px]">
          <GuideRichText :line="copy.removeStep" />
        </p>
      </li>
    </ol>
  </section>
</template>

<style scoped>
@media (forced-colors: active) {
  .guide-disk {
    border-color: CanvasText;
  }
}
</style>
