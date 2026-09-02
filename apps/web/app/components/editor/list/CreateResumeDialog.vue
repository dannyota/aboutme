<script setup lang="ts">
import type { OpaqueCreateOutcome } from '../../../editor/coordinator';
import FormDialog from '@/components/app/FormDialog.vue';
import FormField from '@/components/app/FormField.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';

const props = defineProps<{
  open: boolean;
  busy: boolean;
  retained: OpaqueCreateOutcome | null;
}>();

const emit = defineEmits<{
  close: [];
  submit: [title: string, lng: string | null | undefined];
  refresh: [intentId: string];
  abandon: [intentId: string];
}>();

const title = ref('');
const languageMode = ref<'absent' | 'clear' | 'value'>('absent');
const language = ref('');
const returnFocus = ref<HTMLElement | null>(null);

watch(() => props.open, (open) => {
  if (open) {
    returnFocus.value = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
  } else if (returnFocus.value !== null) {
    const target = returnFocus.value;
    returnFocus.value = null;
    void nextTick(() => target.focus());
  }
}, { immediate: true });

function refresh(): void {
  if (props.retained !== null) emit('refresh', props.retained.intent.id);
}

function abandon(): void {
  if (props.retained !== null) emit('abandon', props.retained.intent.id);
}

function submit(): void {
  const lng = languageMode.value === 'absent'
    ? undefined
    : languageMode.value === 'clear' ? null : language.value;
  emit('submit', title.value, lng);
}
</script>

<template>
  <FormDialog
    :open="open"
    title="Create resume"
    description="Create a new private resume."
    submit-label="Create"
    :busy="busy"
    @cancel="emit('close')"
    @submit="submit"
  >
    <template v-if="retained !== null">
      <p role="alert">
        We could not confirm whether this resume was created.
      </p>
    </template>
    <template v-else>
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
      <FormField label="Language">
        <template #default="{ id, describedBy }">
          <RadioGroup
            :id="id"
            v-model="languageMode"
            :aria-describedby="describedBy"
            :disabled="busy"
          >
            <div class="flex items-center gap-2">
              <RadioGroupItem
                id="language-absent"
                value="absent"
              />
              <Label for="language-absent">Leave unchanged</Label>
            </div>
            <div class="flex items-center gap-2">
              <RadioGroupItem
                id="language-clear"
                value="clear"
              />
              <Label for="language-clear">Clear language</Label>
            </div>
            <div class="flex items-center gap-2">
              <RadioGroupItem
                id="language-value"
                value="value"
              />
              <Label for="language-value">Set language</Label>
            </div>
          </RadioGroup>
        </template>
      </FormField>
      <FormField
        v-if="languageMode === 'value'"
        label="Language value"
        name="lng"
      >
        <template #default="{ id, describedBy, invalid }">
          <Input
            :id="id"
            v-model="language"
            :aria-describedby="describedBy"
            :aria-invalid="invalid"
            name="lng"
            :disabled="busy"
          />
        </template>
      </FormField>
    </template>
    <template
      v-if="retained !== null"
      #footer
    >
      <Button
        :disabled="busy"
        type="button"
        variant="outline"
        @click="refresh"
      >
        Refresh list
      </Button>
      <Button
        :disabled="busy"
        type="button"
        @click="abandon"
      >
        Abandon
      </Button>
    </template>
  </FormDialog>
</template>
