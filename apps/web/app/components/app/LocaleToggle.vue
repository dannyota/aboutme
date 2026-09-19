<script setup lang="ts">
import { Button } from '@/components/ui/button';
import {
  localeNames,
  localeShortNames,
  locales,
} from '@/i18n/locale';
import { cn } from '@/lib/utils';

defineProps<{
  readonly label: string;
  readonly testId?: 'landing-locale' | 'workspace-locale';
}>();

const { locale, setLocale } = useLocale();

const localeClass = cn(
  'px-2 font-normal text-muted-foreground hover:text-foreground',
  'aria-pressed:text-foreground aria-pressed:underline',
  'aria-pressed:decoration-2 aria-pressed:underline-offset-[6px]',
);
</script>

<template>
  <div
    class="flex items-center"
    role="group"
    :aria-label="label"
    :data-testid="testId"
  >
    <template
      v-for="(option, index) in locales"
      :key="option"
    >
      <span
        v-if="index > 0"
        aria-hidden="true"
        class="h-4 w-px bg-border"
      />
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
          class="sm:hidden"
        >{{ localeShortNames[option] }}</span>
        <span
          aria-hidden="true"
          class="hidden sm:inline"
        >{{ localeNames[option] }}</span>
      </Button>
    </template>
  </div>
</template>
