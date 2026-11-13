<script setup lang="ts">
import { ref } from 'vue';

import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Button } from '@/components/ui/button';
import type { ProjectsCopy } from '@/i18n/projects';

// Also asks on the resume list, with the same wording.
defineProps<{
  readonly open: boolean;
  readonly copy: Pick<ProjectsCopy, 'leaveTitle' | 'stay' | 'leave'>;
}>();
const emit = defineEmits<{ stay: []; leave: [] }>();

const stayButton = ref<{ $el?: HTMLElement } | null>(null);

// Staying is the safe answer, so it takes the focus.
function onOpenAutoFocus(event: Event): void {
  event.preventDefault();
  stayButton.value?.$el?.focus();
}

function onOpenChange(open: boolean): void {
  if (!open) emit('stay');
}
</script>

<template>
  <AlertDialog
    :open="open"
    @update:open="onOpenChange"
  >
    <AlertDialogContent
      :aria-describedby="undefined"
      @open-auto-focus="onOpenAutoFocus"
    >
      <AlertDialogHeader>
        <AlertDialogTitle>{{ copy.leaveTitle }}</AlertDialogTitle>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <Button
          ref="stayButton"
          data-action="project-leave-stay"
          type="button"
          variant="outline"
          @click="emit('stay')"
        >
          {{ copy.stay }}
        </Button>
        <Button
          data-action="project-leave-confirm"
          type="button"
          variant="destructive"
          @click="emit('leave')"
        >
          {{ copy.leave }}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
