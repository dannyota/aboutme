<script setup lang="ts">
/**
 * `AuthLayout` — shared frame for the six account pages. The form stays
 * first in the DOM; a brand panel with no focusable element follows it and
 * renders only from 1024 px (DESIGN.md "Authenticated chrome and editor").
 * The renderer never sees this chrome (ADR 0050 renderer isolation).
 */
import { FileDown, Link2, Lock } from '@lucide/vue';

import AppLogo from '@/components/app/AppLogo.vue';
import { authCopy } from '@/i18n/auth';

defineProps<{ readonly testid: string }>();

const { locale } = useLocale();
const copy = computed(() => authCopy[locale.value]);
</script>

<template>
  <main
    class="mx-auto w-full max-w-[26rem] px-6 py-16
      lg:max-w-5xl lg:px-8"
    :data-testid="testid"
  >
    <div
      class="lg:grid lg:grid-cols-2 lg:overflow-hidden
        lg:rounded-[var(--radius-feature)] lg:border lg:border-border
        lg:bg-card lg:shadow-[var(--shadow-product)]"
    >
      <div
        class="lg:col-start-2 lg:row-start-1 lg:px-14 lg:py-14"
        data-auth-form
      >
        <div class="mx-auto w-full max-w-[26rem]">
          <slot />
        </div>
      </div>
      <div
        class="hidden flex-col gap-12 bg-surface-blue p-12
          lg:col-start-1 lg:row-start-1 lg:flex"
        data-testid="auth-brand-panel"
      >
        <div>
          <AppLogo size="md" />
          <p
            class="mt-8 max-w-sm text-balance text-2xl font-bold
              tracking-tight"
          >
            {{ copy.brandPanel.statement }}
          </p>
          <ul class="mt-6 grid gap-3 text-md text-muted-foreground">
            <li class="flex items-center gap-3">
              <Lock
                aria-hidden="true"
                class="size-5 text-link"
              />
              {{ copy.brandPanel.points[0] }}
            </li>
            <li class="flex items-center gap-3">
              <Link2
                aria-hidden="true"
                class="size-5 text-brand-indigo"
              />
              {{ copy.brandPanel.points[1] }}
            </li>
            <li class="flex items-center gap-3">
              <FileDown
                aria-hidden="true"
                class="size-5 text-brand-purple"
              />
              {{ copy.brandPanel.points[2] }}
            </li>
          </ul>
        </div>
        <div
          aria-hidden="true"
          class="relative isolate mt-auto self-center"
          data-auth-illustration
        >
          <div
            class="pointer-events-none absolute -inset-8 -z-10
              bg-(image:--gradient-hero-glow)"
          />
          <div
            class="grid aspect-[210/297] w-44 -rotate-3 content-start
              gap-2 rounded-[var(--radius-sheet)] bg-white p-4
              shadow-[var(--shadow-paper)]"
          >
            <span class="block h-2.5 w-2/3 rounded-full bg-paper-ink" />
            <span class="block h-1.5 w-1/2 rounded-full bg-paper-muted" />
            <span class="mt-2 block h-px rounded-full bg-paper-muted/40" />
            <span
              class="block h-1.5 w-full rounded-full bg-paper-muted/30"
            />
            <span
              class="block h-1.5 w-11/12 rounded-full bg-paper-muted/30"
            />
            <span
              class="block h-1.5 w-full rounded-full bg-paper-muted/30"
            />
            <span
              class="block h-1.5 w-4/5 rounded-full bg-paper-muted/30"
            />
            <span
              class="block h-1.5 w-2/3 rounded-full bg-paper-muted/30"
            />
            <span class="mt-2 block h-px rounded-full bg-paper-muted/40" />
            <span
              class="block h-1.5 w-full rounded-full bg-paper-muted/30"
            />
            <span
              class="block h-1.5 w-5/6 rounded-full bg-paper-muted/30"
            />
            <span
              class="block h-1.5 w-3/5 rounded-full bg-paper-muted/30"
            />
          </div>
        </div>
      </div>
    </div>
  </main>
</template>
