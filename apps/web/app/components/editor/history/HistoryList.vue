<script setup lang="ts">
import {
  Bot,
  CheckCheck,
  FileText,
  Pencil,
  RotateCcw,
} from '@lucide/vue';
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from 'vue';

import type { CheckpointSummary } from '../../../editor/historyApi';
import { editorHistoryCopy } from '../../../i18n/editor-history';
import {
  causeLine,
  formatAbsolute,
  formatRelativeUnderWeek,
} from './historyFormat';

const props = defineProps<{
  readonly items: readonly CheckpointSummary[];
  readonly selected: string;
}>();
const emit = defineEmits<{
  select: [id: string];
  escape: [];
}>();

const { locale } = useLocale();
const copy = computed(() => editorHistoryCopy[locale.value]);
const now = ref(new Date());
const focusedId = ref(props.selected);
const root = ref<HTMLElement | null>(null);
let timer: ReturnType<typeof setInterval> | undefined;

const ids = computed(() => ['current', ...props.items.map((item) => item.id)]);
const icons = {
  edit: Pencil,
  assistant: Bot,
  restore: RotateCcw,
  accept: CheckCheck,
};
function relative(iso: string): string | null {
  return formatRelativeUnderWeek(iso, locale.value, now.value);
}

function optionFor(id: string): HTMLElement | null {
  return (
    root.value?.querySelector<HTMLElement>(`[data-version-id="${id}"]`) ?? null
  );
}

async function focusOption(id: string): Promise<void> {
  focusedId.value = id;
  await nextTick();
  optionFor(id)?.focus();
}

function onKeydown(event: KeyboardEvent): void {
  const target = (event.target as HTMLElement).closest<HTMLElement>(
    '[role="option"]',
  );
  const id = target?.dataset.versionId;
  if (id === undefined) return;
  const index = ids.value.indexOf(id);
  const move = (next: number): void => {
    event.preventDefault();
    const bounded = Math.min(ids.value.length - 1, Math.max(0, next));
    void focusOption(ids.value[bounded]!);
  };
  if (event.key === 'ArrowDown') move(index + 1);
  else if (event.key === 'ArrowUp') move(index - 1);
  else if (event.key === 'Home') move(0);
  else if (event.key === 'End') move(ids.value.length - 1);
  else if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault();
    emit('select', id);
  } else if (event.key === 'Escape') {
    event.preventDefault();
    emit('escape');
  }
}

watch(() => props.selected, (id) => {
  focusedId.value = id;
});
watch(ids, (next) => {
  if (!next.includes(focusedId.value)) focusedId.value = props.selected;
});

onMounted(() => {
  timer = setInterval(() => {
    now.value = new Date();
  }, 60_000);
  optionFor(props.selected)?.scrollIntoView?.({ block: 'nearest' });
});
onBeforeUnmount(() => clearInterval(timer));

defineExpose({ focusOption });
</script>

<template>
  <ul
    ref="root"
    :aria-label="copy.listLabel"
    class="m-0 flex list-none flex-col gap-1 p-0"
    role="listbox"
    @keydown="onKeydown"
  >
    <li
      :aria-selected="selected === 'current'"
      class="history-option"
      data-version-id="current"
      role="option"
      :tabindex="focusedId === 'current' ? 0 : -1"
      @click="emit('select', 'current')"
    >
      <FileText
        :size="16"
        aria-hidden="true"
        class="mt-0.5 shrink-0 text-muted-foreground"
      />
      <span class="grid min-w-0">
        <span class="history-line1 truncate">{{ copy.current }}</span>
        <span class="truncate text-muted-foreground">
          {{ copy.currentDetail }}
        </span>
      </span>
    </li>
    <li
      aria-hidden="true"
      class="my-2 border-t border-border"
      role="presentation"
    />
    <li
      v-for="entry in items"
      :key="entry.id"
      :aria-selected="selected === entry.id"
      class="history-option"
      :data-version-id="entry.id"
      role="option"
      :tabindex="focusedId === entry.id ? 0 : -1"
      @click="emit('select', entry.id)"
    >
      <component
        :is="icons[entry.cause]"
        :size="16"
        aria-hidden="true"
        class="mt-0.5 shrink-0 text-muted-foreground"
      />
      <span class="grid min-w-0">
        <span class="history-line1 truncate">
          <time :datetime="new Date(entry.startedAt).toISOString()">
            {{ formatAbsolute(entry.startedAt, locale, now) }}
          </time>
          <span
            v-if="relative(entry.startedAt) !== null"
            class="font-normal text-muted-foreground"
          >
            · {{ relative(entry.startedAt) }}
          </span>
        </span>
        <span class="truncate text-muted-foreground">
          {{ causeLine(entry, copy) }}
        </span>
      </span>
    </li>
  </ul>
</template>

<style>
.history-option {
  display: flex;
  min-height: 4rem;
  cursor: pointer;
  align-items: flex-start;
  gap: 10px;
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 0.875rem;
  line-height: 1.5;
  transition: background-color 150ms;
}

.history-option:hover,
.history-option[aria-selected='true'] {
  background: var(--accent);
}

.history-option .history-line1 {
  font-weight: 600;
}

.history-option[aria-selected='true'] .history-line1 {
  font-weight: 700;
}

.history-option:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}

@media (prefers-reduced-motion: reduce) {
  .history-option {
    transition: none;
  }
}
</style>
