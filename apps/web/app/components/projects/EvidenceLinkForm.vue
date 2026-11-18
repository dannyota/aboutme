<script setup lang="ts">
import { computed } from 'vue';

import FormField from '@/components/app/FormField.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import type { Locale } from '@/i18n/locale';
import { projectEvidenceCopy } from '@/i18n/project-evidence';
import type { EvidencePicker } from './evidencePickerState';
import { evidenceNoticeText } from './evidenceText';

const props = defineProps<{
  readonly picker: EvidencePicker;
  readonly locale: Locale;
}>();

const copy = computed(() => projectEvidenceCopy[props.locale]);
const error = computed(() => {
  const notice = props.picker.linkError.value;
  return notice === null ? undefined : evidenceNoticeText(notice, props.locale);
});
const disabled = computed(() => props.picker.blocked.value
  || props.picker.atLimit.value
  || props.picker.link.value.trim() === '');
</script>

<template>
  <form
    class="grid gap-4"
    novalidate
    @submit.prevent="picker.attachLink()"
  >
    <FormField
      id="evidence-link"
      v-slot="{ id: fieldId, describedBy, invalid }"
      :error="error"
      :hint="copy.pasteHint"
      :label="copy.pasteLabel"
      name="link"
    >
      <Input
        :id="fieldId"
        :aria-describedby="describedBy"
        :aria-invalid="invalid"
        autocomplete="off"
        inputmode="url"
        maxlength="300"
        :model-value="picker.link.value"
        type="url"
        @update:model-value="picker.setLink(String($event))"
      />
    </FormField>
    <div>
      <Button
        :aria-describedby="picker.unsaved.value
          ? 'evidence-save-first'
          : undefined"
        data-action="evidence-attach-link"
        :disabled="disabled"
        type="submit"
      >
        {{ copy.attach }}
      </Button>
    </div>
  </form>
</template>
