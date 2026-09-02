<script setup lang="ts">
import { Moon, Sun } from '@lucide/vue';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

const { theme, toggleTheme } = useTheme();
const nextThemeLabel = computed(() =>
  theme.value === 'dark' ? 'light' : 'dark',
);
</script>

<template>
  <Button
    type="button"
    :aria-label="`Switch to ${nextThemeLabel} theme`"
    :class="cn('theme-toggle', $attrs.class)"
    size="sm"
    variant="ghost"
    @click="toggleTheme"
  >
    <Moon
      v-if="theme === 'dark'"
      :size="16"
      aria-hidden="true"
    />
    <Sun
      v-else
      :size="16"
      aria-hidden="true"
    />
    <span class="hidden md:inline">
      {{ theme === 'dark' ? 'Dark mode' : 'Light mode' }}
    </span>
  </Button>
</template>
