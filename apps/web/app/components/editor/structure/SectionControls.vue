<script setup lang="ts">
import type { Section } from '@aboutme/schema';

type SectionAction = {
  readonly key: string;
  readonly sectionType: Section['sectionType'];
};

const props = defineProps<{
  readonly column: 'main' | 'sidebar';
  readonly disabled: boolean;
  readonly index: number;
  readonly section: Section;
  readonly sectionKey: string;
  readonly sectionCount: number;
  readonly sidebarCount: number;
}>();

const emit = defineEmits<{
  delete: [action: SectionAction];
  metadata: [
    action: SectionAction & {
      readonly field: 'displayName' | 'iconKey';
      readonly value: string | null;
    },
  ];
  move: [
    action: SectionAction & {
      readonly column: 'main' | 'sidebar';
      readonly index: number;
    },
  ];
  reorder: [
    action: SectionAction & {
      readonly column: 'main' | 'sidebar';
      readonly index: number;
    },
  ];
}>();

function action(): SectionAction {
  return { key: props.sectionKey, sectionType: props.section.sectionType };
}

function changeDisplayName(event: Event): void {
  const target = event.target;
  if (!(target instanceof HTMLInputElement)) return;
  if (target.value === (props.section.displayName ?? '')) return;
  emit('metadata', { ...action(), field: 'displayName', value: target.value });
}

function changeIconKey(event: Event): void {
  const target = event.target;
  if (!(target instanceof HTMLInputElement)) return;
  const value = target.value === '' ? null : target.value;
  if (value === (props.section.iconKey ?? null)) return;
  emit('metadata', { ...action(), field: 'iconKey', value });
}
</script>

<template>
  <fieldset :disabled="disabled">
    <legend>{{ sectionKey }}</legend>
    <label>
      Section name
      <input
        :value="section.displayName ?? ''"
        data-action="displayName"
        @change="changeDisplayName"
      >
    </label>
    <label>
      Icon key
      <input
        :value="section.iconKey ?? ''"
        data-action="iconKey"
        @change="changeIconKey"
      >
    </label>
    <div aria-label="Section placement controls">
      <button
        type="button"
        data-action="move-up"
        :disabled="index === 0"
        @click="emit('move', { ...action(), column, index: index - 1 })"
      >
        Move up
      </button>
      <button
        type="button"
        data-action="move-down"
        :disabled="index === sectionCount - 1"
        @click="emit('move', { ...action(), column, index: index + 1 })"
      >
        Move down
      </button>
      <button
        v-if="column === 'sidebar'"
        type="button"
        data-action="move-main"
        @click="emit('move', { ...action(), column: 'main', index: 0 })"
      >
        Move to main
      </button>
      <button
        v-else
        type="button"
        data-action="move-sidebar"
        @click="
          emit('move', {
            ...action(),
            column: 'sidebar',
            index: sidebarCount,
          })
        "
      >
        Move to sidebar
      </button>
      <button
        type="button"
        data-action="reorder"
        :disabled="index === 0"
        @click="emit('reorder', { ...action(), column, index: 0 })"
      >
        Move to start
      </button>
      <button
        type="button"
        data-action="delete"
        @click="emit('delete', action())"
      >
        Delete section
      </button>
    </div>
  </fieldset>
</template>
