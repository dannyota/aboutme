<script setup lang="ts">
import { ref } from 'vue';

import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../ui/alert-dialog';
import { Button } from '../ui/button';

defineProps<{
  readonly open: boolean;
  readonly title: string;
  readonly keepLabel: string;
  readonly removeLabel: string;
  readonly cancelLabel: string;
}>();
const emit = defineEmits<{
  keep: [];
  remove: [];
  cancel: [];
  closed: [];
}>();

const keepButton = ref<{ $el?: HTMLElement } | null>(null);

function onOpenAutoFocus(event: Event): void {
  event.preventDefault();
  keepButton.value?.$el?.focus();
}

function onCloseAutoFocus(event: Event): void {
  event.preventDefault();
  emit('closed');
}

function onOpenChange(open: boolean): void {
  if (!open) emit('cancel');
}
</script>

<template>
  <AlertDialog
    :open="open"
    @update:open="onOpenChange"
  >
    <AlertDialogContent
      :aria-describedby="undefined"
      data-testid="evidence-disconnect-dialog"
      @close-auto-focus="onCloseAutoFocus"
      @open-auto-focus="onOpenAutoFocus"
    >
      <AlertDialogHeader>
        <AlertDialogTitle>{{ title }}</AlertDialogTitle>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <Button
          data-action="evidence-disconnect-cancel"
          type="button"
          variant="outline"
          @click="emit('cancel')"
        >
          {{ cancelLabel }}
        </Button>
        <Button
          data-action="evidence-remove-links"
          type="button"
          variant="destructive"
          @click="emit('remove')"
        >
          {{ removeLabel }}
        </Button>
        <Button
          ref="keepButton"
          data-action="evidence-keep-links"
          type="button"
          variant="default"
          @click="emit('keep')"
        >
          {{ keepLabel }}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
