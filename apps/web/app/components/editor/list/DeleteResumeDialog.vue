<script setup lang="ts">
import type { ResumeSummary } from '../../../editor/resumeApi';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';

const props = defineProps<{
  item: ResumeSummary | null;
  busy: boolean;
}>();

const emit = defineEmits<{ close: []; submit: [id: string, title: string] }>();

function submit(): void {
  if (props.item !== null) {
    emit('submit', props.item.id, props.item.title);
  }
}
</script>

<template>
  <ConfirmDialog
    :open="item !== null"
    title="Delete resume"
    :description="item === null
      ? 'This permanently deletes the resume. Type its title to confirm.'
      : `Delete ${item.title}? This permanently deletes the resume.`"
    confirm-label="Delete"
    confirm-input-label="Current title"
    :confirm-text="item?.title"
    confirm-action="confirm-delete"
    cancel-action="cancel-delete"
    destructive
    :busy="busy"
    @cancel="emit('close')"
    @confirm="submit"
  />
</template>
