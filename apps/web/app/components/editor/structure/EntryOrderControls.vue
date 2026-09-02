<script setup lang="ts">
import type { Section } from '@aboutme/schema';
import IconButton from '@/components/app/IconButton.vue';

const props = defineProps<{
  readonly disabled: boolean;
  readonly entries: readonly { readonly id: string }[];
  readonly sectionKey: string;
  readonly sectionType: Section['sectionType'];
}>();

const emit = defineEmits<{
  reorder: [
    action: {
      readonly entryIds: readonly string[];
      readonly sectionKey: string;
      readonly sectionType: Section['sectionType'];
    },
  ];
}>();

function move(entryId: string, direction: -1 | 1): void {
  const index = props.entries.findIndex((entry) => entry.id === entryId);
  const nextIndex = index + direction;
  if (index < 0 || nextIndex < 0 || nextIndex >= props.entries.length) return;
  const entryIds = props.entries.map((entry) => entry.id);
  const [moved] = entryIds.splice(index, 1);
  if (moved === undefined) return;
  entryIds.splice(nextIndex, 0, moved);
  emit('reorder', {
    entryIds,
    sectionKey: props.sectionKey,
    sectionType: props.sectionType,
  });
}
</script>

<template>
  <div
    :data-entry-order="sectionKey"
    aria-label="Entry order"
  >
    <ol>
      <li
        v-for="(entry, index) in entries"
        :key="entry.id"
      >
        <span class="text-xs text-muted-foreground">{{ entry.id }}</span>
        <IconButton
          label="Move entry up"
          data-action="entry-up"
          :disabled="disabled || index === 0"
          @click="move(entry.id, -1)"
        />
        <IconButton
          label="Move entry down"
          data-action="entry-down"
          :disabled="disabled || index === entries.length - 1"
          @click="move(entry.id, 1)"
        />
      </li>
    </ol>
  </div>
</template>
