<script setup lang="ts">
import { X } from '@lucide/vue';
import { computed, nextTick, ref } from 'vue';

import FormField from '@/components/app/FormField.vue';
import { Input } from '@/components/ui/input';
import type { ProjectsCopy } from '@/i18n/projects';
import {
  characterCount,
  TECHNOLOGY_LENGTH,
  TECHNOLOGY_LIMIT,
} from './projectDocument';

const props = defineProps<{
  readonly id: string;
  readonly modelValue: readonly string[];
  readonly copy: ProjectsCopy;
  readonly error?: string;
}>();
const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>();

const entry = ref('');
const tooLong = ref(false);
const input = ref<{ $el?: HTMLInputElement } | null>(null);
const full = computed(() => props.modelValue.length >= TECHNOLOGY_LIMIT);
const hint = computed(() => full.value
  ? props.copy.technologiesFull(TECHNOLOGY_LIMIT)
  : props.copy.technologiesHint);
const shownError = computed(() => props.error ?? (tooLong.value
  ? props.copy.technologyTooLong(TECHNOLOGY_LENGTH)
  : undefined));

function add(values: readonly string[]): void {
  const next = [...props.modelValue];
  for (const raw of values) {
    const value = raw.trim();
    if (value === '' || next.includes(value)) continue;
    if (next.length >= TECHNOLOGY_LIMIT) break;
    if (characterCount(value) > TECHNOLOGY_LENGTH) {
      tooLong.value = true;
      entry.value = value;
      if (next.length !== props.modelValue.length) {
        emit('update:modelValue', next);
      }
      return;
    }
    next.push(value);
  }
  tooLong.value = false;
  entry.value = '';
  if (next.length !== props.modelValue.length) {
    emit('update:modelValue', next);
  }
}

function onInput(value: string | number): void {
  const text = String(value);
  tooLong.value = false;
  if (!text.includes(',')) {
    entry.value = text;
    return;
  }
  const parts = text.split(',');
  const last = parts.pop() ?? '';
  add(parts);
  if (!tooLong.value) entry.value = last;
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Enter' || event.key === ',') {
    event.preventDefault();
    add([entry.value]);
  } else if (
    event.key === 'Backspace'
    && entry.value === ''
    && props.modelValue.length > 0
  ) {
    event.preventDefault();
    emit('update:modelValue', props.modelValue.slice(0, -1));
  }
}

function remove(index: number): void {
  emit('update:modelValue',
    props.modelValue.filter((_, position) => position !== index));
  void nextTick(() => input.value?.$el?.focus());
}
</script>

<template>
  <FormField
    :id="id"
    :label="copy.technologies"
    :hint="hint"
    :error="shownError"
    name="technologies"
  >
    <template #default="{ id: inputId, describedBy, invalid }">
      <ul
        v-if="modelValue.length > 0"
        :aria-label="copy.technologies"
        class="flex flex-wrap gap-1.5"
        data-testid="project-technology-chips"
      >
        <li
          v-for="(item, index) in modelValue"
          :key="`${index}-${item}`"
          class="inline-flex max-w-full items-center gap-1 rounded-full
            border border-border bg-secondary py-0.5 pr-1 pl-2.5 text-sm
            text-secondary-foreground"
          data-technology
        >
          <span class="min-w-0 break-all">{{ item }}</span>
          <button
            type="button"
            :aria-label="copy.removeTechnology(item)"
            class="inline-flex size-6 shrink-0 items-center justify-center
              rounded-full hover:bg-accent focus-visible:ring-2
              focus-visible:ring-ring focus-visible:outline-none"
            @click="remove(index)"
          >
            <X
              aria-hidden="true"
              class="size-3.5"
            />
          </button>
        </li>
      </ul>
      <Input
        :id="inputId"
        ref="input"
        :model-value="entry"
        :aria-describedby="describedBy"
        :aria-invalid="invalid"
        autocomplete="off"
        data-testid="project-technology-input"
        @update:model-value="onInput"
        @keydown="onKeydown"
        @blur="add([entry])"
      />
    </template>
  </FormField>
</template>
