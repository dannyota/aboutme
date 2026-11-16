<script setup lang="ts">
import { Button } from '@/components/ui/button';
import {
  localeNames,
  localeShortNames,
  locales,
} from '@/i18n/locale';
import { cn } from '@/lib/utils';

withDefaults(defineProps<{
  readonly label: string;
  readonly testId?: 'landing-locale' | 'workspace-locale';
  readonly compact?: boolean;
}>(), { testId: undefined, compact: false });

const { locale, setLocale } = useLocale();

const localeClass = cn(
  'h-[30px] rounded-full px-2 text-[13px] font-semibold',
  'text-muted-foreground aria-[pressed=false]:hover:text-foreground',
  'hover:no-underline focus-visible:no-underline',
  'aria-pressed:bg-foreground aria-pressed:text-background',
);
</script>

<template>
  <div
    class="flex items-center gap-0.5 rounded-full bg-surface-mist p-[3px]"
    role="group"
    :aria-label="label"
    :data-testid="testId"
  >
    <template
      v-for="option in locales"
      :key="option"
    >
      <Button
        type="button"
        :aria-label="localeNames[option]"
        :class="localeClass"
        :lang="option"
        :aria-pressed="locale === option"
        :data-testid="testId === undefined ? undefined : `${testId}-${option}`"
        size="sm"
        variant="link"
        @click="setLocale(option)"
      >
        <span
          aria-hidden="true"
          :class="compact ? 'lg:hidden' : 'sm:hidden'"
        >{{ localeShortNames[option] }}</span>
        <span
          aria-hidden="true"
          :class="compact ? 'hidden lg:inline' : 'hidden sm:inline'"
        >{{ localeNames[option] }}</span>
      </Button>
    </template>
  </div>
</template>
