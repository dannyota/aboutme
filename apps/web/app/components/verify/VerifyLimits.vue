<script setup lang="ts">
/**
 * The limits section (docs/design/deployment-transparency/visual.md,
 * "Limits"): what the page proves and what it does not, plus a link to the
 * full list on GitHub. Not a card.
 */
import { Check, Minus } from '@lucide/vue';
import { computed } from 'vue';
import type { Locale } from '@/i18n/locale';
import { limitsUrl, verifyLimits } from '@/i18n/verify';
import type { ReportingPlatform } from '@/utils/verifyView';

const props = defineProps<{
  readonly locale: Locale;
  readonly reporter: ReportingPlatform;
}>();

const copy = computed(() => verifyLimits(props.locale, props.reporter));
</script>

<template>
  <section
    aria-labelledby="verify-limits-heading"
    data-testid="verify-limits"
  >
    <h2
      id="verify-limits-heading"
      class="text-[18px] font-semibold"
    >
      {{ copy.heading }}
    </h2>
    <div
      class="mt-4 grid grid-cols-1 gap-6 min-[42rem]:grid-cols-2
        min-[42rem]:gap-8"
    >
      <div>
        <h3 class="text-[14px] font-semibold">
          {{ copy.provesLabel }}
        </h3>
        <ul class="mt-2 space-y-2">
          <li
            v-for="item in copy.proves"
            :key="item"
            class="flex gap-2.5 text-[15px]"
          >
            <Check
              aria-hidden="true"
              class="mt-0.5 size-4 flex-none text-link"
            />
            {{ item }}
          </li>
        </ul>
      </div>
      <div>
        <h3 class="text-[14px] font-semibold">
          {{ copy.notLabel }}
        </h3>
        <ul class="mt-2 space-y-2">
          <li
            v-for="item in copy.not"
            :key="item"
            class="flex gap-2.5 text-[15px]"
          >
            <Minus
              aria-hidden="true"
              class="mt-0.5 size-4 flex-none text-muted-foreground"
            />
            {{ item }}
          </li>
        </ul>
      </div>
    </div>
    <p class="mt-4 text-[14px]">
      <a
        class="text-link underline-offset-4 hover:underline"
        :href="limitsUrl"
        rel="noopener noreferrer"
      >{{ copy.fullList }}</a>
    </p>
  </section>
</template>
