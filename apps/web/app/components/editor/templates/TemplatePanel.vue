<script setup lang="ts">
import { TEMPLATES, type TemplatePreset } from '@aboutme/schema/templates';
import { Search, X } from '@lucide/vue';
import { computed, onScopeDispose, ref, watch } from 'vue';
import IconButton from '@/components/app/IconButton.vue';
import FormField from '@/components/app/FormField.vue';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import InspectorPanel from '../InspectorPanel.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import type {
  TemplateGroupCommand,
  TemplateGroupState,
} from '../../../editor/templateGroup';
import { templateUndoAvailable } from '../../../editor/templateGroup';
import type { ResumeRecord } from '../../../stores/resumes';
import TemplatePartialDialog from './TemplatePartialDialog.vue';
import TemplateThumbnail from './TemplateThumbnail.vue';
import { defaultSectionNames } from '../sectionTypes';
import { galleryTemplate } from '../../../templates/catalog';
import { matchesTemplateSearch } from '../../../templates/search';
import type { ResumeSnapshot } from '../../../editor/types';
import { editorControlsCopy } from '../../../i18n/editor-controls';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly group?: TemplateGroupCommand;
  readonly record?: ResumeRecord;
  readonly state?: TemplateGroupState;
}>();
const { locale } = useLocale();
const copy = computed(() => editorControlsCopy[locale.value].controls);

const notice = ref(false);
const moved = ref({ main: [] as string[], sidebar: [] as string[] });

// The panel's search box: filters the preset list by name, style, sample
// tag, role, and filter chip, in either site language (DESIGN.md, editor
// Templates panel).
const search = ref('');
const searchInput = ref<{ $el?: HTMLElement } | null>(null);
const visibleTemplates = computed(() => TEMPLATES.filter((preset) => {
  const entry = galleryTemplate(preset.id);
  return entry === undefined || matchesTemplateSearch(entry, search.value);
}));
function clearSearch(): void {
  search.value = '';
  searchInput.value?.$el?.focus();
}
function onSearchKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && search.value !== '') {
    search.value = '';
  }
}

// The visible message updates immediately and may quote the query; the
// role="status" line is announced to screen readers only once typing
// pauses, with stable wording that never repeats the query back.
const SEARCH_ANNOUNCE_DELAY_MS = 500;
const announcedCount = ref<number | null>(null);
let announceTimer: ReturnType<typeof setTimeout> | undefined;

function clearAnnounceTimer(): void {
  if (announceTimer === undefined) return;
  clearTimeout(announceTimer);
  announceTimer = undefined;
}

watch(search, (value) => {
  clearAnnounceTimer();
  if (value.trim() === '') {
    announcedCount.value = null;
    return;
  }
  announceTimer = setTimeout(() => {
    announcedCount.value = visibleTemplates.value.length;
    announceTimer = undefined;
  }, SEARCH_ANNOUNCE_DELAY_MS);
});
onScopeDispose(clearAnnounceTimer, true);

const searchMessage = computed(() => {
  if (search.value.trim() === '' || visibleTemplates.value.length > 0) {
    return '';
  }
  return copy.value.templateSearchNoMatch(search.value);
});
// Visible and immediate: tracks the filtered list with no delay. The
// no-match case is covered by searchMessage above instead.
const searchCount = computed(() => {
  const count = visibleTemplates.value.length;
  if (search.value.trim() === '' || count === 0) return '';
  return copy.value.templateSearchCount(count);
});
// Screen-reader only and debounced, so a live region does not announce
// every keystroke.
const searchStatus = computed(() => {
  if (search.value.trim() === '' || announcedCount.value === null) return '';
  return announcedCount.value === 0
    ? copy.value.templateSearchNoMatchAnnounced
    : copy.value.templateSearchCount(announcedCount.value);
});

const movedText = computed(() => [
  moved.value.sidebar.length > 0
    ? copy.value.movedToSidebar(moved.value.sidebar.join(', '))
    : '',
  moved.value.main.length > 0
    ? copy.value.movedToMain(moved.value.main.join(', '))
    : '',
].filter(Boolean).join(' '));
const record = computed(() => props.record ?? props.actions.record.value);
const group = computed(() => props.group ?? groupFrom(record.value));
const state = computed(() => props.state ?? record.value?.templateState);
const preview = computed(() => group.value?.intendedFinal);
function templateDescription(id: string, fallback: string): string {
  return galleryTemplate(id)?.purpose[locale.value] ?? fallback;
}
const canUndo = computed(() => {
  const undo = state.value?.kind === 'complete' ? state.value.undo : undefined;
  const current = record.value?.current;
  return (
    undo !== undefined
    && current !== undefined
    && templateUndoAvailable(undo, current)
  );
});

function apply(preset: Readonly<TemplatePreset>): void {
  const before = record.value?.current;
  const result = props.actions.applyTemplate(preset);
  if (result.kind === 'no-change') {
    notice.value = true;
    moved.value = { main: [], sidebar: [] };
  }
  if (result.kind === 'enqueued') {
    notice.value = false;
    moved.value
      = before === undefined
        ? { main: [], sidebar: [] }
        : movedSections(before, result.group.intendedFinal);
  }
}

/** Names the sections a template moved to the other column. */
function movedSections(before: ResumeSnapshot, after: ResumeSnapshot): {
  main: string[];
  sidebar: string[];
} {
  const columnOf = (snapshot: ResumeSnapshot, key: string) =>
    snapshot.document.customization.layout.sections.sidebar.includes(key)
      ? 'sidebar'
      : 'main';
  const name = (key: string): string => {
    const section = after.document.content[key];
    if (section === undefined) return key;
    return (
      section.displayName?.trim() || defaultSectionNames[section.sectionType]
    );
  };
  const { main, sidebar } = after.document.customization.layout.sections;
  const toSidebar = sidebar.filter((key) => columnOf(before, key) === 'main');
  const toMain = main.filter((key) => columnOf(before, key) === 'sidebar');
  return { main: toMain.map(name), sidebar: toSidebar.map(name) };
}

function status(): string {
  if (state.value === undefined || state.value === null) {
    return notice.value ? copy.value.noChanges : '';
  }
  switch (state.value.kind) {
    case 'queued':
    case 'running':
      return copy.value.templateSaving;
    case 'complete':
      return copy.value.templateSaved;
    case 'partial':
      return copy.value.templateNeedsAttention;
    default:
      return assertNever(state.value);
  }
}

// A switch keeps the owner's page format, so only the date format can change.
function hasFormatWarning(preset: Readonly<TemplatePreset>): boolean {
  const customization = record.value?.current.document.customization;
  return (
    customization !== undefined
    && preset.customization.dateFormat !== customization.dateFormat
  );
}

function hasBaseSizeWarning(preset: Readonly<TemplatePreset>): boolean {
  return preset.customization.font.baseSizePx === 10;
}

function hasMarginWarning(preset: Readonly<TemplatePreset>): boolean {
  const margin = preset.customization.spacing.pageMargin;
  return margin !== undefined && (margin.x < 5 || margin.y < 5);
}

function groupFrom(
  record: ResumeRecord | undefined,
): TemplateGroupCommand | undefined {
  const candidates = [record?.attempt?.queueItem, ...(record?.pending ?? [])];
  return candidates.find(
    (candidate): candidate is TemplateGroupCommand =>
      candidate?.kind === 'templateGroup',
  );
}

function assertNever(value: never): never {
  throw new Error(`Unexpected template state: ${String(value)}`);
}
</script>

<template>
  <InspectorPanel
    :title="copy.templates"
    title-id="template-title"
  >
    <FormField
      id="template-search"
      v-slot="{ id: searchId }"
      :label="copy.templateSearchLabel"
    >
      <div class="relative">
        <Search
          aria-hidden="true"
          class="pointer-events-none absolute left-3 top-1/2 size-4
            -translate-y-1/2 text-muted-foreground"
        />
        <Input
          :id="searchId"
          ref="searchInput"
          v-model="search"
          class="pl-9 pr-9"
          data-testid="template-search-input"
          :placeholder="copy.templateSearchPlaceholder"
          type="text"
          @keydown="onSearchKeydown"
        />
        <IconButton
          v-if="search !== ''"
          class="absolute right-1 top-1/2 -translate-y-1/2"
          data-testid="template-search-clear"
          :label="copy.templateSearchClear"
          size="icon-sm"
          variant="ghost"
          @click="clearSearch"
        >
          <X aria-hidden="true" />
        </IconButton>
      </div>
    </FormField>
    <p
      v-if="searchMessage !== ''"
      class="text-sm"
      data-testid="template-search-message"
    >
      {{ searchMessage }}
    </p>
    <!-- Screen readers get the count from the status line below, after the
      typing pause; hiding this copy stops browse mode reading it twice. -->
    <p
      v-if="searchCount !== ''"
      aria-hidden="true"
      class="text-sm text-muted-foreground"
      data-testid="template-search-count"
    >
      {{ searchCount }}
    </p>
    <p
      class="sr-only"
      data-testid="template-search-status"
      role="status"
    >
      {{ searchStatus }}
    </p>
    <StatusBanner
      v-if="status() !== ''"
      kind="info"
      testid="template-status"
    >
      {{ status() }}
    </StatusBanner>
    <p
      v-if="movedText !== ''"
      class="text-sm"
      data-testid="template-moved-sections"
      role="status"
    >
      {{ movedText }}
    </p>
    <ul :aria-label="copy.templatePresets">
      <li
        v-for="preset in visibleTemplates"
        :key="preset.id"
        :data-template="preset.id"
      >
        <Card>
          <CardHeader>
            <CardTitle>{{ preset.name }}</CardTitle>
          </CardHeader>
          <CardContent class="flex items-start gap-4">
            <TemplateThumbnail :preset="preset" />
            <div class="grid gap-2">
              <p>{{ templateDescription(preset.id, preset.description) }}</p>
              <ul
                :aria-label="copy.templateWarnings"
                class="text-xs text-muted-foreground"
              >
                <li v-if="hasFormatWarning(preset)">
                  {{ copy.templateFormatWarning }}
                </li>
                <li v-if="hasBaseSizeWarning(preset)">
                  {{ copy.templateSizeWarning }}
                </li>
                <li v-if="hasMarginWarning(preset)">
                  {{ copy.templateMarginWarning }}
                </li>
              </ul>
            </div>
          </CardContent>
          <CardFooter>
            <Button
              size="sm"
              type="button"
              @click="apply(preset)"
            >
              {{ copy.apply }}
            </Button>
          </CardFooter>
        </Card>
      </li>
    </ul>
    <p
      v-if="preview !== undefined && state?.kind !== 'partial'"
      data-template-preview
    >
      {{ copy.templateChangesReady }}
    </p>
    <Button
      v-if="canUndo && state?.kind === 'complete'"
      type="button"
      data-action="undo-template"
      @click="actions.undoTemplate()"
    >
      {{ copy.undoTemplate }}
    </Button>
    <TemplatePartialDialog
      v-if="group !== undefined && state?.kind === 'partial'"
      :actions="actions"
      :group="group"
      :state="state"
    />
  </InspectorPanel>
</template>
