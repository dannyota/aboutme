<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue';
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';

const props = withDefaults(
  defineProps<{
    readonly open: boolean;
    readonly title: string;
    readonly description: string;
    readonly confirmLabel: string;
    readonly cancelLabel?: string;
    readonly destructive?: boolean;
    readonly busy?: boolean;
    readonly confirmText?: string;
    readonly confirmInputLabel?: string;
    readonly confirmAction?: string;
    readonly cancelAction?: string;
    readonly class?: string;
  }>(),
  { cancelLabel: 'Cancel' },
);
const emit = defineEmits<{ confirm: []; cancel: [] }>();
const typed = ref('');
const confirmId = `confirm-${useId()}`;
const confirmButton = ref<{ $el?: HTMLElement } | null>(null);
const cancelButton = ref<{ $el?: HTMLElement } | null>(null);
const inputLabel = computed(
  () => props.confirmInputLabel ?? `Type ${props.confirmText ?? ''} to confirm`,
);
const canConfirm = computed(
  () =>
    !props.busy
    && (props.confirmText === undefined || typed.value === props.confirmText),
);
watch(
  () => props.open,
  (open) => {
    if (!open) typed.value = '';
  },
);
function onOpenChange(open: boolean): void {
  if (!open && !props.busy) emit('cancel');
}
function onOpenAutoFocus(event: Event): void {
  event.preventDefault();
  (props.destructive ? cancelButton.value : confirmButton.value)?.$el?.focus();
}
function onEscape(event: Event): void {
  if (props.busy) event.preventDefault();
}
</script>

<template>
  <AlertDialog
    :open="open"
    @update:open="onOpenChange"
  >
    <AlertDialogContent
      :class="cn(props.class)"
      :aria-busy="busy || undefined"
      @escape-key-down="onEscape"
      @open-auto-focus="onOpenAutoFocus"
    >
      <AlertDialogHeader>
        <AlertDialogTitle>{{ title }}</AlertDialogTitle>
        <AlertDialogDescription>{{ description }}</AlertDialogDescription>
      </AlertDialogHeader>
      <div
        v-if="confirmText !== undefined"
        class="grid gap-1.5"
      >
        <Label :for="confirmId">{{ inputLabel }}</Label>
        <Input
          :id="confirmId"
          v-model="typed"
          autocomplete="off"
          :disabled="busy"
        />
      </div>
      <AlertDialogFooter>
        <Button
          ref="cancelButton"
          :data-action="cancelAction"
          :disabled="busy"
          type="button"
          variant="outline"
          @click="emit('cancel')"
        >
          {{ cancelLabel }}
        </Button>
        <Button
          ref="confirmButton"
          :data-action="confirmAction"
          :disabled="!canConfirm"
          type="button"
          :variant="destructive ? 'destructive' : 'default'"
          @click="emit('confirm')"
        >
          {{ confirmLabel }}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
