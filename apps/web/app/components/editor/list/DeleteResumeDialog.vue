<script setup lang="ts">
import { computed } from 'vue';
import type { ResumeSummary } from '../../../editor/resumeApi';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import LocaleToggle from '@/components/app/LocaleToggle.vue';
import { resumeListCopy } from '@/i18n/resume-list';
import { shellCopy } from '@/i18n/shell';

const props = defineProps<{
  item: ResumeSummary | null;
  busy: boolean;
}>();

const emit = defineEmits<{ close: []; submit: [id: string, title: string] }>();
const locale = useRouteLocale();
const copy = computed(() => resumeListCopy[locale.value]);

const description = computed(() =>
  props.item === null
    ? copy.value.deleteDescriptionGeneric
    : copy.value.deleteDescription(props.item.title));

function submit(): void {
  if (props.item !== null) {
    emit('submit', props.item.id, props.item.title);
  }
}
</script>

<template>
  <ConfirmDialog
    :open="item !== null"
    :title="copy.deleteTitle"
    :description="description"
    :confirm-label="copy.confirmDelete"
    :cancel-label="copy.cancel"
    confirm-text="DELETE"
    :confirm-input-label="copy.deleteInput"
    confirm-action="confirm-delete"
    cancel-action="cancel-delete"
    destructive
    :busy="busy"
    @cancel="emit('close')"
    @confirm="submit"
  >
    <template #header-actions>
      <LocaleToggle
        :label="shellCopy[locale].localeLabel"
        @pointerdown.prevent
      />
    </template>
  </ConfirmDialog>
</template>
