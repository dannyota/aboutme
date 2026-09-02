<script setup lang="ts">
import type { PersonalDetail } from '@aboutme/schema';
import { ref, watch } from 'vue';
import { ChevronDown, ChevronUp, Trash2 } from '@lucide/vue';
import IconButton from '../../app/IconButton.vue';
import CheckboxField from '../../app/CheckboxField.vue';
import SelectField from '../../app/SelectField.vue';
import StatusBanner from '../../app/StatusBanner.vue';
import TextField from '../../app/TextField.vue';
import { Button } from '../../ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '../../ui/card';

const props = defineProps<{
  readonly createEntityId: () => string;
  readonly details?: readonly PersonalDetail[];
}>();

const emit = defineEmits<{
  change: [details: readonly PersonalDetail[]];
  unset: [];
}>();

const details = ref<PersonalDetail[]>(copyDetails(props.details));
const limitError = ref(false);
const urlError = ref<string | null>(null);

watch(
  () => props.details,
  (next) => {
    details.value = copyDetails(next);
    limitError.value = false;
  },
);

function add(): void {
  if (details.value.length >= 16) {
    limitError.value = true;
    return;
  }
  limitError.value = false;
  replace([
    ...details.value,
    {
      id: props.createEntityId(),
      type: 'email',
      value: '',
      isHidden: false,
    },
  ]);
}

function changeLabel(id: string, value: string): void {
  const detail = detailById(id);
  if (detail === undefined || value === (detail.label ?? '')) return;
  replace(
    details.value.map((candidate) =>
      candidate.id === id
        ? value === '' && candidate.label === undefined
          ? candidate
          : { ...candidate, label: value }
        : candidate,
    ),
  );
}

function changeType(id: string, type: PersonalDetail['type']): void {
  const detail = detailById(id);
  if (detail === undefined || type === detail.type) return;
  if (isWebProfile(type)
    && detail.value !== ''
    && !detail.value.startsWith('https://')) {
    urlError.value = id;
    return;
  }
  urlError.value = null;
  replace(
    details.value.map((candidate) =>
      candidate.id === id ? { ...candidate, type } : candidate,
    ),
  );
}

function changeValue(id: string, value: string): void {
  const detail = detailById(id);
  if (detail === undefined || value === detail.value) return;
  if (
    isWebProfile(detail.type)
    && value !== ''
    && !value.startsWith('https://')
  ) {
    urlError.value = id;
    return;
  }
  urlError.value = null;
  replace(
    details.value.map((candidate) =>
      candidate.id === id ? { ...candidate, value } : candidate,
    ),
  );
}

function changeHidden(id: string, isHidden: boolean): void {
  const detail = detailById(id);
  if (detail === undefined || isHidden === detail.isHidden) return;
  replace(
    details.value.map((candidate) =>
      candidate.id === id ? { ...candidate, isHidden } : candidate,
    ),
  );
}

function unsetLabel(id: string): void {
  const detail = detailById(id);
  if (detail?.label === undefined) return;
  replace(
    details.value.map((candidate) => {
      if (candidate.id !== id) return candidate;
      const { label: _label, ...withoutLabel } = candidate;
      return withoutLabel;
    }),
  );
}

function move(id: string, direction: -1 | 1): void {
  const from = details.value.findIndex((detail) => detail.id === id);
  const to = from + direction;
  if (from < 0 || to < 0 || to >= details.value.length) return;
  const next = [...details.value];
  const [moved] = next.splice(from, 1);
  if (moved === undefined) return;
  next.splice(to, 0, moved);
  replace(next);
}

function remove(id: string): void {
  replace(details.value.filter((detail) => detail.id !== id));
}

function replace(next: readonly PersonalDetail[]): void {
  details.value = copyDetails(next);
  emit('change', details.value);
}

function detailById(id: string): PersonalDetail | undefined {
  return details.value.find((detail) => detail.id === id);
}

function copyDetails(
  value: readonly PersonalDetail[] | undefined,
): PersonalDetail[] {
  return value?.map((detail) => ({ ...detail })) ?? [];
}

function isWebProfile(type: PersonalDetail['type']): boolean {
  return (
    type === 'website'
    || type === 'linkedin'
    || type === 'github'
    || type === 'twitter'
  );
}

const typeOptions = [
  { value: 'email', label: 'Email' },
  { value: 'phone', label: 'Phone' },
  { value: 'location', label: 'Location' },
  { value: 'website', label: 'Website' },
  { value: 'linkedin', label: 'LinkedIn' },
  { value: 'github', label: 'GitHub' },
  { value: 'twitter', label: 'Twitter' },
  { value: 'custom', label: 'Custom' },
] as const;
</script>

<template>
  <div class="grid gap-4">
    <Card
      v-for="(detail, index) in details"
      :key="detail.id"
      :data-detail-index="index"
    >
      <CardHeader>
        <CardTitle
          class="text-base"
          :data-detail-id="detail.id"
        >
          Contact detail {{ index + 1 }}
        </CardTitle>
      </CardHeader>
      <CardContent class="grid gap-4">
        <SelectField
          label="Type"
          :model-value="detail.type"
          :options="typeOptions"
          :control-attrs="{ 'data-detail-type': '' }"
          @update:model-value="
            changeType(detail.id, $event as PersonalDetail['type'])
          "
        />
        <TextField
          label="Label"
          :model-value="detail.label"
          :control-attrs="{ 'data-detail-label': '' }"
          @intent="(intent) => intent.kind === 'unset'
            ? unsetLabel(detail.id) : changeLabel(detail.id, intent.value)"
        />
        <TextField
          label="Value"
          :model-value="detail.value"
          :error="urlError === detail.id ? 'Use a lowercase https:// URL.' : undefined"
          :error-attrs="{ 'data-error': 'contact-url' }"
          :control-attrs="{ 'data-detail-value': '' }"
          @intent="(intent) => intent.kind === 'unset'
            ? changeValue(detail.id, '') : changeValue(detail.id, intent.value)"
        />
        <CheckboxField
          label="Hide this detail"
          :model-value="detail.isHidden"
          :data-detail-is-hidden="true"
          role="checkbox"
          @update:model-value="changeHidden(detail.id, $event)"
        />
        <div class="flex justify-end gap-1">
          <IconButton
            label="Move up"
            size="icon-sm"
            :disabled="index === 0"
            data-action="move-detail-up"
            @click="move(detail.id, -1)"
          >
            <ChevronUp />
          </IconButton>
          <IconButton
            label="Move down"
            size="icon-sm"
            :disabled="index === details.length - 1"
            data-action="move-detail-down"
            @click="move(detail.id, 1)"
          >
            <ChevronDown />
          </IconButton>
          <IconButton
            label="Remove detail"
            size="icon-sm"
            data-action="remove-detail"
            @click="remove(detail.id)"
          >
            <Trash2 />
          </IconButton>
        </div>
      </CardContent>
    </Card>
    <div class="flex gap-2">
      <Button
        data-action="add-detail"
        size="sm"
        variant="outline"
        @click="add"
      >
        Add detail
      </Button>
      <Button
        v-if="props.details !== undefined"
        data-action="unset-details"
        size="sm"
        variant="ghost"
        @click="emit('unset')"
      >
        Remove contact list
      </Button>
    </div>
    <StatusBanner
      v-if="limitError"
      kind="error"
      data-error="detail-limit"
    >
      You can add up to 16 contact details.
    </StatusBanner>
  </div>
</template>
