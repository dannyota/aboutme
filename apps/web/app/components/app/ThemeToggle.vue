<script setup lang="ts">
import { Moon, Sun } from '@lucide/vue';
import { Button } from '@/components/ui/button';
import type { Locale } from '@/i18n/locale';
import { shellCopy } from '@/i18n/shell';
import { cn } from '@/lib/utils';

const props = withDefaults(defineProps<{ locale?: Locale }>(), {
  locale: 'en',
});
const { theme, toggleTheme } = useTheme();
const stored = useStoredTheme();
const mounted = ref(false);
onMounted(() => {
  mounted.value = true;
});
const labelTheme = computed(() => mounted.value
  ? theme.value
  : stored.value ?? 'light');
const copy = computed(() => shellCopy[props.locale]);
</script>

<template>
  <Button
    type="button"
    :aria-label="labelTheme === 'dark' ? copy.switchToLight : copy.switchToDark"
    :class="cn('theme-toggle', $attrs.class)"
    size="icon"
    variant="ghost"
    @click="toggleTheme"
  >
    <Moon
      class="hidden dark:block"
      data-theme-icon="dark"
      :size="16"
      aria-hidden="true"
    />
    <Sun
      class="dark:hidden"
      data-theme-icon="light"
      :size="16"
      aria-hidden="true"
    />
  </Button>
</template>
