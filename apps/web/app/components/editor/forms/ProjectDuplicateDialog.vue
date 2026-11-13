<script setup lang="ts">
import { ref } from 'vue';

import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Button } from '@/components/ui/button';

// From your projects for a project the resume already has as an unlinked
// entry (docs/design/ui/projects.md "In the resume editor"). Link that
// entry is the default and takes the focus; Cancel or dismissing sends
// nothing.
defineProps<{
  readonly open: boolean;
  readonly title: string;
  readonly question: string;
  readonly linkLabel: string;
  readonly addLabel: string;
  readonly cancelLabel: string;
}>();
const emit = defineEmits<{ link: []; add: []; cancel: [] }>();

const linkButton = ref<{ $el?: HTMLElement } | null>(null);

function onOpenAutoFocus(event: Event): void {
  event.preventDefault();
  linkButton.value?.$el?.focus();
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
    <AlertDialogContent @open-auto-focus="onOpenAutoFocus">
      <AlertDialogHeader>
        <AlertDialogTitle>{{ title }}</AlertDialogTitle>
        <AlertDialogDescription class="break-words">
          {{ question }}
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <Button
          data-action="cancel-duplicate-entry"
          type="button"
          variant="ghost"
          @click="emit('cancel')"
        >
          {{ cancelLabel }}
        </Button>
        <Button
          data-action="add-duplicate-entry"
          type="button"
          variant="outline"
          @click="emit('add')"
        >
          {{ addLabel }}
        </Button>
        <Button
          ref="linkButton"
          data-action="link-duplicate-entry"
          type="button"
          @click="emit('link')"
        >
          {{ linkLabel }}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
