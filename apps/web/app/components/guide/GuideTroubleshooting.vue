<script setup lang="ts">
/**
 * Troubleshooting (docs/design/mcp-guide.md, "Page structure" and
 * "Layout"): a `dl` of six question and answer pairs, the question a 15 px
 * weight-600 `dt`, the answer a 15 px `dd`, 16 px between pairs.
 */
import type { GuideCopy } from '@/i18n/guide';
import GuideRichText from './GuideRichText.vue';

defineProps<{
  readonly copy: GuideCopy['troubleshooting'];
}>();
</script>

<template>
  <section
    aria-labelledby="guide-troubleshooting-heading"
    class="rounded-[var(--radius-feature)] border bg-card p-5
      shadow-[var(--shadow-product)] min-[42rem]:p-7"
    data-testid="guide-troubleshooting"
  >
    <h2
      id="guide-troubleshooting-heading"
      class="text-[18px] font-semibold"
    >
      {{ copy.heading }}
    </h2>
    <dl class="mt-4 space-y-4">
      <div
        v-for="(pair, index) in copy.pairs"
        :key="index"
      >
        <dt class="text-[15px] font-semibold">
          {{ pair.question }}
        </dt>
        <dd class="mt-1 text-[15px]">
          <GuideRichText :line="pair.answer" />
        </dd>
      </div>
    </dl>
  </section>
</template>
