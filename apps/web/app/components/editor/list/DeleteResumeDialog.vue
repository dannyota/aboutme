<script setup lang="ts">
import { computed } from 'vue';
import type { ResumeSummary } from '../../../editor/resumeApi';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';

const props = defineProps<{
  item: ResumeSummary | null;
  busy: boolean;
}>();

const emit = defineEmits<{ close: []; submit: [id: string, title: string] }>();

const description = computed(() =>
  props.item === null
    ? 'This permanently deletes the resume.'
    : `Delete "${props.item.title}"? This permanently deletes the resume.`);

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
    :description="description"
    confirm-label="Delete"
    confirm-text="DELETE"
    confirm-action="confirm-delete"
    cancel-action="cancel-delete"
    destructive
    :busy="busy"
    @cancel="emit('close')"
    @confirm="submit"
  />
</template>
