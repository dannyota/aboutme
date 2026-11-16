<script setup lang="ts">
import { FileDown, Link2, Lock } from '@lucide/vue';

import AppLogo from '@/components/app/AppLogo.vue';
import { authCopy } from '@/i18n/auth';

defineProps<{ readonly testid: string }>();

const { locale } = useLocale();
const copy = computed(() => authCopy[locale.value]);
const pointIcons = [Lock, Link2, FileDown] as const;
</script>

<template>
  <main
    class="mx-auto w-full max-w-[26rem] px-6 py-16 lg:grid lg:max-w-[79rem]
      lg:grid-cols-[5fr_6fr] lg:gap-14 lg:px-8 lg:py-6"
    :data-testid="testid"
  >
    <div
      class="lg:col-start-2 lg:row-start-1 lg:self-center lg:py-12"
      data-auth-form
    >
      <div class="mx-auto w-full max-w-[26rem]">
        <slot />
      </div>
    </div>
    <div
      class="auth-panel relative hidden min-h-[38.75rem] flex-col
        overflow-hidden rounded-[var(--radius-panel)] bg-panel p-14
        text-panel-foreground lg:col-start-1 lg:row-start-1 lg:flex"
      data-testid="auth-brand-panel"
    >
      <AppLogo
        class="self-start"
        size="md"
      />
      <p
        class="mt-10 max-w-[11em] text-balance text-2xl font-extrabold
          tracking-[-0.02em]"
        data-auth-statement
      >
        {{ copy.brandPanel.statement }}
      </p>
      <ul class="mt-7 grid gap-3.5 text-md text-panel-muted">
        <li
          v-for="(point, index) in copy.brandPanel.points"
          :key="point"
          class="flex items-center gap-3"
        >
          <span
            class="grid size-7 shrink-0 place-content-center rounded-full
              bg-coin text-coin-ink shadow-[inset_0_-3px_0_var(--coin-edge)]"
            data-auth-coin
          >
            <component
              :is="pointIcons[index]"
              aria-hidden="true"
              class="size-4"
            />
          </span>
          {{ point }}
        </li>
      </ul>
      <div
        aria-hidden="true"
        class="absolute right-12 -bottom-24 w-[13.75rem]"
        data-auth-illustration
      >
        <div
          class="grid aspect-[210/297] -rotate-3 content-start gap-2
            rounded-[var(--radius-sheet)] bg-paper p-5
            shadow-[var(--shadow-paper-hero)]"
          data-auth-sheet
        >
          <span class="block h-2.5 w-2/3 rounded-full bg-paper-ink" />
          <span class="block h-1.5 w-1/2 rounded-full bg-paper-muted" />
          <span class="mt-2 block h-px rounded-full bg-paper-muted/40" />
          <span class="block h-1.5 w-full rounded-full bg-paper-muted/30" />
          <span class="block h-1.5 w-11/12 rounded-full bg-paper-muted/30" />
          <span class="block h-1.5 w-full rounded-full bg-paper-muted/30" />
          <span class="block h-1.5 w-4/5 rounded-full bg-paper-muted/30" />
          <span class="block h-1.5 w-2/3 rounded-full bg-paper-muted/30" />
          <span class="mt-2 block h-px rounded-full bg-paper-muted/40" />
          <span class="block h-1.5 w-full rounded-full bg-paper-muted/30" />
          <span class="block h-1.5 w-5/6 rounded-full bg-paper-muted/30" />
          <span class="block h-1.5 w-3/5 rounded-full bg-paper-muted/30" />
        </div>
        <span
          class="absolute -top-7 -left-7 grid size-[4.25rem]
            place-content-center rounded-full bg-coin text-coin-ink
            shadow-[inset_0_-5px_0_var(--coin-edge),var(--shadow-float)]"
          data-auth-lock
        >
          <Lock class="size-7" />
        </span>
      </div>
    </div>
  </main>
</template>

<style scoped>
@media (forced-colors: none) {
  .auth-panel :deep([data-logo-part="mark"]),
  .auth-panel :deep([data-logo-part="dot"]) {
    color: var(--logo-mark-inverse);
  }
}
</style>
