<script setup lang="ts">
import { TEMPLATES, type TemplatePreset } from '@aboutme/schema/templates';
import { computed, ref } from 'vue';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
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
    <StatusBanner
      v-if="status() !== ''"
      kind="info"
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
        v-for="preset in TEMPLATES"
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
