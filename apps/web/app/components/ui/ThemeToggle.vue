<script setup lang="ts">
import { Moon, Sun } from '@lucide/vue';

const { theme, toggleTheme } = useTheme();
const nextThemeLabel = computed(() =>
  theme.value === 'dark' ? 'light' : 'dark',
);
</script>

<template>
  <button
    class="theme-toggle"
    type="button"
    :aria-label="`Switch to ${nextThemeLabel} theme`"
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
    <span>{{ theme === 'dark' ? 'Dark mode' : 'Light mode' }}</span>
  </button>
</template>
