<script setup lang="ts">
import { ChevronDown, ChevronUp, Trash2 } from '@lucide/vue';
import { computed, ref, watch } from 'vue';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import IconButton from '@/components/app/IconButton.vue';
import SwitchField from '@/components/app/SwitchField.vue';
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible';

const props = withDefaults(
  defineProps<{
    readonly title: string;
    readonly subtitle?: string;
    readonly entryId: string;
    readonly hidden: boolean;
    readonly index: number;
    readonly count: number;
    readonly open?: boolean;
  }>(),
  { open: undefined },
);
const internalOpen = ref(props.open ?? true);
const resolvedOpen = computed(() => props.open ?? internalOpen.value);
const emit = defineEmits<{
  'toggleHidden': [];
  'moveUp': [];
  'moveDown': [];
  'delete': [];
  'update:open': [open: boolean];
}>();

watch(
  () => props.open,
  (open) => {
    if (open !== undefined) internalOpen.value = open;
  },
);

function updateOpen(open: boolean): void {
  if (props.open === undefined) internalOpen.value = open;
  emit('update:open', open);
}
</script>

<template>
  <Card :data-entry-id="entryId">
    <Collapsible
      v-slot="{ open: isOpen }"
      :open="resolvedOpen"
      @update:open="updateOpen"
    >
      <CardHeader class="flex-row items-center justify-between gap-2">
        <div>
          <CardTitle class="text-base">
            {{ title }}
          </CardTitle>
          <p v-if="subtitle">
            {{ subtitle }}
          </p>
        </div>
        <div class="flex items-center gap-1">
          <CollapsibleTrigger as-child>
            <IconButton
              :label="isOpen ? 'Collapse entry fields' : 'Expand entry fields'"
              size="icon-sm"
              data-action="toggle-entry-fields"
            >
              <ChevronDown />
            </IconButton>
          </CollapsibleTrigger>
          <SwitchField
            label="Hidden"
            :model-value="props.hidden"
            data-action="toggle-hidden"
            @update:model-value="emit('toggleHidden')"
          />
          <IconButton
            label="Move entry up"
            size="icon-sm"
            :disabled="index === 0"
            data-action="entry-up"
            @click="emit('moveUp')"
          >
            <ChevronUp />
          </IconButton>
          <IconButton
            label="Move entry down"
            size="icon-sm"
            :disabled="index === count - 1"
            data-action="entry-down"
            @click="emit('moveDown')"
          >
            <ChevronDown />
          </IconButton>
          <IconButton
            label="Delete entry"
            size="icon-sm"
            data-action="delete-entry"
            @click="emit('delete')"
          >
            <Trash2 />
          </IconButton>
        </div>
      </CardHeader>
      <CollapsibleContent>
        <CardContent><slot /></CardContent>
      </CollapsibleContent>
    </Collapsible>
  </Card>
</template>
