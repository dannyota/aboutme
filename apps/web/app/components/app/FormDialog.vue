<script setup lang="ts">
import { ref } from 'vue';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { cn } from '@/lib/utils';

const props = withDefaults(
  defineProps<{
    readonly open: boolean;
    readonly title: string;
    readonly description?: string;
    readonly submitLabel: string;
    readonly cancelLabel?: string;
    readonly busy?: boolean;
    readonly submitDisabled?: boolean;
    readonly restoreFocus?: boolean;
    readonly showCloseButton?: boolean;
    readonly submitAction?: string;
    readonly cancelAction?: string;
    readonly class?: string;
  }>(),
  { cancelLabel: 'Cancel', restoreFocus: true, showCloseButton: true },
);
const emit = defineEmits<{ submit: []; cancel: [] }>();
const form = ref<HTMLFormElement | null>(null);
function onSubmit(): void {
  if (!props.busy) emit('submit');
}
function onOpenChange(open: boolean): void {
  if (!open && !props.busy) emit('cancel');
}
function onOpenAutoFocus(event: Event): void {
  event.preventDefault();
  form.value
    ?.querySelector<HTMLElement>('input, select, textarea, button')
    ?.focus();
}
function onCloseAutoFocus(event: Event): void {
  if (!props.restoreFocus) event.preventDefault();
}
</script>

<template>
  <Dialog
    :open="open"
    @update:open="onOpenChange"
  >
    <DialogContent
      :class="cn(props.class)"
      :show-close-button="showCloseButton && !busy"
      @close-auto-focus="onCloseAutoFocus"
      @open-auto-focus="onOpenAutoFocus"
    >
      <DialogHeader>
        <DialogTitle>{{ title }}</DialogTitle>
        <DialogDescription v-if="description">
          {{
            description
          }}
        </DialogDescription>
      </DialogHeader>
      <form
        ref="form"
        novalidate
        @submit.prevent="onSubmit"
      >
        <slot />
        <DialogFooter>
          <slot name="footer">
            <Button
              :data-action="cancelAction"
              :disabled="busy"
              type="button"
              variant="outline"
              @click="emit('cancel')"
            >
              {{ cancelLabel }}
            </Button>
            <Button
              :data-action="submitAction"
              :disabled="busy || submitDisabled"
              type="submit"
            >
              {{ submitLabel }}
            </Button>
          </slot>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
