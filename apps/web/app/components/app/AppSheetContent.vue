<script setup lang="ts">
import type { DialogContentEmits, DialogContentProps } from 'reka-ui';
import type { HTMLAttributes } from 'vue';
import { X } from '@lucide/vue';
import { reactiveOmit } from '@vueuse/core';
import { useForwardPropsEmits } from 'reka-ui';
import { Button } from '@/components/ui/button';
import { SheetClose, SheetContent } from '@/components/ui/sheet';

interface AppSheetContentProps extends DialogContentProps {
  class?: HTMLAttributes['class'];
  side?: 'top' | 'right' | 'bottom' | 'left';
  closeLabel: string;
}

defineOptions({ inheritAttrs: false });

const props = defineProps<AppSheetContentProps>();
const emits = defineEmits<DialogContentEmits>();
const delegatedProps = reactiveOmit(props, 'closeLabel');
const forwarded = useForwardPropsEmits(delegatedProps, emits);
</script>

<template>
  <SheetContent
    v-bind="{ ...$attrs, ...forwarded }"
    :show-close="false"
  >
    <slot />
    <SheetClose as-child>
      <Button
        :aria-label="closeLabel"
        class="absolute top-4 right-4 size-11 p-0"
        type="button"
        variant="ghost"
      >
        <X
          aria-hidden="true"
          class="size-4"
        />
      </Button>
    </SheetClose>
  </SheetContent>
</template>
