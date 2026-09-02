<script setup lang="ts">
import type { ResumeSummary } from '../../../editor/resumeApi';
import FormDialog from '@/components/app/FormDialog.vue';
import FormField from '@/components/app/FormField.vue';
import { Input } from '@/components/ui/input';

const props = defineProps<{
  item: ResumeSummary | null;
  busy: boolean;
}>();

const emit = defineEmits<{ close: []; submit: [id: string, title: string] }>();
const title = ref('');
const returnFocus = ref<HTMLElement | null>(null);

watch(() => props.item, (item) => {
  title.value = item?.title ?? '';
  if (item !== null) {
    returnFocus.value = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
  } else if (returnFocus.value !== null) {
    const target = returnFocus.value;
    returnFocus.value = null;
    void nextTick(() => target.focus());
  }
}, { immediate: true });

function submit(): void {
  if (props.item !== null && title.value !== props.item.title) {
    emit('submit', props.item.id, title.value);
  }
}
</script>

<template>
  <FormDialog
    :open="item !== null"
    title="Rename resume"
    description="Enter the new resume title."
    submit-label="Save"
    :submit-disabled="item === null || title === item.title"
    :busy="busy"
    @cancel="emit('close')"
    @submit="submit"
  >
    <FormField
      label="Title"
      name="title"
      required
    >
      <template #default="{ id, describedBy, invalid }">
        <Input
          :id="id"
          v-model="title"
          :aria-describedby="describedBy"
          :aria-invalid="invalid"
          name="title"
          required
          :disabled="busy"
        />
      </template>
    </FormField>
  </FormDialog>
</template>
