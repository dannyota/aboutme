<script setup lang="ts">
import type { Section } from '@aboutme/schema';
import { computed } from 'vue';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import FormField from '@/components/app/FormField.vue';
import SelectField from '@/components/app/SelectField.vue';
import { sectionIconOptions, sectionTypeLabels } from '../sectionTypes';

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
  /** Sections in the main column; a moved section goes after them. */
  readonly mainCount: number;
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

// Keep an icon set elsewhere (an agent or an older editor) selectable.
const iconOptions = computed(() => {
  const current = props.section.iconKey;
  if (
    current === undefined
    || sectionIconOptions.some((option) => option.value === current)
  ) {
    return sectionIconOptions;
  }
  return [...sectionIconOptions, { value: current, label: 'Current icon' }];
});

function action(): SectionAction {
  return { key: props.sectionKey, sectionType: props.section.sectionType };
}

function changeDisplayName(event: Event): void {
  const target = event.target;
  if (!(target instanceof HTMLInputElement)) return;
  if (target.value === (props.section.displayName ?? '')) return;
  emit('metadata', { ...action(), field: 'displayName', value: target.value });
}

function changeIconKey(next: string): void {
  const value = next === '' ? null : next;
  if (value === (props.section.iconKey ?? null)) return;
  emit('metadata', { ...action(), field: 'iconKey', value });
}
</script>

<template>
  <div class="grid gap-3">
    <div class="flex items-center justify-between gap-2">
      <Badge variant="outline">
        {{ sectionTypeLabels[section.sectionType] }}
      </Badge>
    </div>
    <FormField
      v-slot="{ id }"
      label="Section name"
      name="displayName"
    >
      <Input
        :id="id"
        data-action="displayName"
        :disabled="disabled"
        :model-value="section.displayName ?? ''"
        @change="changeDisplayName"
      />
    </FormField>
    <SelectField
      :control-attrs="{ 'data-action': 'iconKey' }"
      :disabled="disabled"
      label="Heading icon"
      :model-value="section.iconKey ?? ''"
      name="iconKey"
      :options="iconOptions"
      @update:model-value="changeIconKey"
    />
    <div
      aria-label="Section placement controls"
      class="flex flex-wrap gap-2"
    >
      <Button
        type="button"
        data-action="move-up"
        :disabled="disabled || index === 0"
        size="sm"
        variant="outline"
        @click="emit('move', { ...action(), column, index: index - 1 })"
      >
        Move up
      </Button>
      <Button
        type="button"
        data-action="move-down"
        :disabled="disabled || index === sectionCount - 1"
        size="sm"
        variant="outline"
        @click="emit('move', { ...action(), column, index: index + 1 })"
      >
        Move down
      </Button>
      <Button
        v-if="column === 'sidebar'"
        type="button"
        data-action="move-main"
        :disabled="disabled"
        size="sm"
        variant="outline"
        @click="
          emit('move', { ...action(), column: 'main', index: mainCount })
        "
      >
        Move to main
      </Button>
      <Button
        v-else
        type="button"
        data-action="move-sidebar"
        :disabled="disabled"
        size="sm"
        variant="outline"
        @click="
          emit('move', {
            ...action(),
            column: 'sidebar',
            index: sidebarCount,
          })
        "
      >
        Move to sidebar
      </Button>
      <Button
        type="button"
        data-action="reorder"
        :disabled="disabled || index === 0"
        size="sm"
        variant="outline"
        @click="emit('reorder', { ...action(), column, index: 0 })"
      >
        Move to start
      </Button>
      <Button
        type="button"
        data-action="delete"
        :disabled="disabled"
        size="sm"
        variant="ghost"
        class="text-destructive"
        @click="emit('delete', action())"
      >
        Delete section
      </Button>
    </div>
  </div>
</template>
