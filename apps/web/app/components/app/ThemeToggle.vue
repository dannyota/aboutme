<script setup lang="ts">
import { Moon, Sun } from '@lucide/vue';
import { Button } from '@/components/ui/button';
import { landingCopy, type LandingLocale } from '@/landing/copy';
import { cn } from '@/lib/utils';

const props = withDefaults(defineProps<{ locale?: LandingLocale }>(), {
  locale: 'en',
});
const { theme, toggleTheme } = useTheme();
const copy = computed(() => landingCopy[props.locale]);
</script>

<template>
  <Button
    type="button"
    :aria-label="theme === 'dark' ? copy.switchToLight : copy.switchToDark"
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
      {{ theme === 'dark' ? copy.darkMode : copy.lightMode }}
    </span>
  </Button>
</template>
