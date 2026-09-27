<script setup lang="ts">
/**
 * "What your assistant can do" (docs/design/mcp-guide.md, "Layout"): a Can
 * and a Cannot list, one column below 42 rem with Can first, two 32 px-gap
 * columns from 42 rem. Nothing here uses seal red: the Cannot list must not
 * read as an error or a public-state mark. Can takes a Lucide `check` in
 * `--link`; Cannot takes a Lucide `minus` in `--muted-foreground`; the two
 * shapes stay distinct so the meaning survives forced colors.
 */
import { Check, Minus } from '@lucide/vue';
import type { GuideCopy } from '@/i18n/guide';

defineProps<{
  readonly copy: GuideCopy['canCannot'];
}>();
</script>

<template>
  <section
    aria-labelledby="guide-can-cannot-heading"
    class="rounded-[var(--radius-feature)] border bg-card p-5
      shadow-[var(--shadow-product)] min-[42rem]:p-7"
    data-testid="guide-can-cannot"
  >
    <h2
      id="guide-can-cannot-heading"
      class="text-[18px] font-semibold"
    >
      {{ copy.heading }}
    </h2>
    <div
      class="mt-4 grid grid-cols-1 gap-6 min-[42rem]:grid-cols-2
        min-[42rem]:gap-8"
    >
      <div data-testid="guide-can">
        <p class="text-[15px] font-semibold">
          {{ copy.canHeading }}
        </p>
        <ul class="mt-2 space-y-2">
          <li
            v-for="(item, index) in copy.can"
            :key="index"
            class="flex items-start gap-2 text-[15px]"
          >
            <Check
              aria-hidden="true"
              class="mt-0.5 size-4 flex-none text-link"
            />
            <span>{{ item }}</span>
          </li>
        </ul>
      </div>
      <div data-testid="guide-cannot">
        <p class="text-[15px] font-semibold">
          {{ copy.cannotHeading }}
        </p>
        <ul class="mt-2 space-y-2">
          <li
            v-for="(item, index) in copy.cannot"
            :key="index"
            class="flex items-start gap-2 text-[15px]"
          >
            <Minus
              aria-hidden="true"
              class="mt-0.5 size-4 flex-none text-muted-foreground"
            />
            <span>{{ item }}</span>
          </li>
        </ul>
      </div>
    </div>
    <p class="mt-4 text-[13px] text-muted-foreground">
      {{ copy.scope }}
    </p>
  </section>
</template>
