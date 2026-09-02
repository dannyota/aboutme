<script setup lang="ts">
import type { Section } from '@aboutme/schema';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import FormField from '@/components/app/FormField.vue';

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
  <div class="grid gap-3">
    <div class="flex items-center justify-between gap-2">
      <Badge variant="outline">
        {{ sectionKey }}
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
    <FormField
      v-slot="{ id }"
      label="Icon key"
      name="iconKey"
    >
      <Input
        :id="id"
        data-action="iconKey"
        :disabled="disabled"
        :model-value="section.iconKey ?? ''"
        @change="changeIconKey"
      />
    </FormField>
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
        @click="emit('move', { ...action(), column: 'main', index: 0 })"
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
