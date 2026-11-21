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
import StatusBanner from '../../app/StatusBanner.vue';

const props = defineProps<{
  readonly open: boolean;
  readonly busy: boolean;
  readonly title: string;
  readonly body: string;
  readonly error: string | null;
  readonly cancelLabel: string;
  readonly confirmLabel: string;
  readonly busyLabel: string;
  readonly confirmAction: string;
}>();
const emit = defineEmits<{ confirm: []; cancel: [] }>();
const cancelButton = ref<{ $el?: HTMLElement } | null>(null);

function onOpenChange(open: boolean): void {
  if (!open && !props.busy) emit('cancel');
}
function guardBusy(event: Event): void {
  if (props.busy) event.preventDefault();
}
function onOpenAutoFocus(event: Event): void {
  event.preventDefault();
  cancelButton.value?.$el?.focus();
}
</script>

<template>
  <AlertDialog
    :open="open"
    @update:open="onOpenChange"
  >
    <AlertDialogContent
      :aria-busy="busy || undefined"
      class="max-w-[32rem] sm:max-w-[32rem]"
      @escape-key-down="guardBusy"
      @interact-outside="guardBusy"
      @open-auto-focus="onOpenAutoFocus"
    >
      <AlertDialogHeader>
        <AlertDialogTitle>{{ title }}</AlertDialogTitle>
        <AlertDialogDescription class="leading-normal">
          {{ body }}
        </AlertDialogDescription>
      </AlertDialogHeader>
      <StatusBanner
        v-if="error !== null"
        kind="error"
      >
        {{ error }}
      </StatusBanner>
      <AlertDialogFooter>
        <Button
          ref="cancelButton"
          :disabled="busy"
          type="button"
          variant="outline"
          @click="emit('cancel')"
        >
          {{ cancelLabel }}
        </Button>
        <Button
          :data-action="confirmAction"
          :disabled="busy"
          type="button"
          variant="destructive"
          @click="emit('confirm')"
        >
          {{ busy ? busyLabel : confirmLabel }}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
