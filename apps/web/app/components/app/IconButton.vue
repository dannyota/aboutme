<script setup lang="ts">
import type { ButtonVariants } from '@/components/ui/button';
import { Button } from '@/components/ui/button';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';

defineOptions({ inheritAttrs: false });

const props = withDefaults(
  defineProps<{
    readonly label: string;
    readonly variant?: ButtonVariants['variant'];
    readonly size?: 'icon' | 'icon-sm';
    readonly pressed?: boolean;
    readonly disabled?: boolean;
    readonly class?: string;
    /** An icon button never submits a form unless asked to. */
    readonly type?: 'button' | 'submit' | 'reset';
  }>(),
  { variant: 'ghost', size: 'icon', type: 'button' },
);
</script>

<template>
  <TooltipProvider>
    <Tooltip>
      <TooltipTrigger as-child>
        <Button
          v-bind="$attrs"
          :aria-label="label"
          :aria-pressed="pressed"
          :class="cn(props.class)"
          :disabled="disabled"
          :size="size"
          :type="type"
          :variant="variant"
        >
          <slot />
        </Button>
      </TooltipTrigger><TooltipContent>{{ label }}</TooltipContent>
    </Tooltip>
  </TooltipProvider>
</template>
