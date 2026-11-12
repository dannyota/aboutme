<script setup lang="ts">
import { ChevronDown, ChevronUp, Library, Trash2 } from '@lucide/vue';
import { computed, nextTick, ref, watch } from 'vue';
import { editorFieldsCopy } from '@/i18n/editor-fields';
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
    /** The library project a linked entry comes from. */
    readonly projectName?: string;
    readonly selected?: boolean;
  }>(),
  { open: undefined, projectName: undefined, selected: false },
);
const internalOpen = ref(props.open ?? true);
const { locale } = useLocale();
const copy = computed(() => editorFieldsCopy[locale.value].entryCard);
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

const card = ref<{ $el?: HTMLElement } | null>(null);
watch(
  () => props.selected,
  async (selected) => {
    if (!selected) return;
    if (props.open === undefined) internalOpen.value = true;
    await nextTick();
    const element = card.value?.$el;
    element?.scrollIntoView?.({ block: 'nearest' });
    element?.focus();
  },
  { immediate: true },
);

function updateOpen(open: boolean): void {
  if (props.open === undefined) internalOpen.value = open;
  emit('update:open', open);
}
</script>

<template>
  <Card
    ref="card"
    class="data-[selected]:ring-2 data-[selected]:ring-ring"
    :data-entry-id="entryId"
    :data-selected="selected ? '' : undefined"
    :tabindex="selected ? -1 : undefined"
  >
    <Collapsible
      v-slot="{ open: isOpen }"
      :open="resolvedOpen"
      @update:open="updateOpen"
    >
      <CardHeader class="flex-row items-center justify-between gap-2">
        <div class="min-w-0">
          <CardTitle class="text-base break-words">
            {{ title }}
          </CardTitle>
          <p v-if="subtitle">
            {{ subtitle }}
          </p>
          <p
            v-if="projectName !== undefined"
            class="flex min-w-0 items-center gap-1 text-sm
              text-muted-foreground"
            data-entry-project
          >
            <Library
              aria-hidden="true"
              class="size-4 shrink-0"
            />
            <span class="sr-only">{{ copy.libraryMark }}</span>
            <span class="truncate">{{ projectName }}</span>
          </p>
        </div>
        <div class="flex items-center gap-1">
          <CollapsibleTrigger as-child>
            <IconButton
              :label="isOpen ? copy.collapse : copy.expand"
              size="icon-sm"
              data-action="toggle-entry-fields"
            >
              <ChevronDown />
            </IconButton>
          </CollapsibleTrigger>
          <SwitchField
            :label="copy.hidden"
            :model-value="props.hidden"
            data-action="toggle-hidden"
            @update:model-value="emit('toggleHidden')"
          />
          <IconButton
            :label="copy.moveUp"
            size="icon-sm"
            :disabled="index === 0"
            data-action="entry-up"
            @click="emit('moveUp')"
          >
            <ChevronUp />
          </IconButton>
          <IconButton
            :label="copy.moveDown"
            size="icon-sm"
            :disabled="index === count - 1"
            data-action="entry-down"
            @click="emit('moveDown')"
          >
            <ChevronDown />
          </IconButton>
          <IconButton
            :label="copy.delete"
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
