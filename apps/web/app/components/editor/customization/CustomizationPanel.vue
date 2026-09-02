<script setup lang="ts">
import type { Customization } from '@aboutme/schema';
import { computed, ref } from 'vue';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import type {
  CustomizationDelta,
  CustomizationSetPath,
} from '../../../editor/commands';
import type { JsonValue } from '../../../editor/types';
import type { ResumeRecord } from '../../../stores/resumes';
import CheckboxField from '../../app/CheckboxField.vue';
import FormField from '../../app/FormField.vue';
import SelectField from '../../app/SelectField.vue';
import SwitchField from '../../app/SwitchField.vue';
import { Button } from '../../ui/button';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '../../ui/card';
import { Input } from '../../ui/input';
import InspectorPanel from '../InspectorPanel.vue';
import {
  CUSTOMIZATION_FIELDS,
  type CustomizationField,
} from './fields';
import ColorField from './ColorField.vue';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly record?: ResumeRecord;
}>();

const root = ref<HTMLElement | null>(null);
const localErrors = ref<
  Readonly<Partial<Record<CustomizationSetPath, string>>>
>({});
const record = computed(() => props.record ?? props.actions.record.value);
const customization = computed(
  () => record.value?.current.document.customization,
);
const issues = computed(() => Object.values(record.value?.issues ?? {}).flat());
const LABELS: Readonly<Partial<Record<CustomizationSetPath, string>>> = {
  'font.family': 'Font family',
  'font.baseSizePx': 'Base size (px)',
  'spacing.sectionGap': 'Section gap',
  'spacing.entryGap': 'Entry gap',
  'spacing.lineHeight': 'Line height',
  'heading.style': 'Heading style',
  'heading.showRule': 'Show heading rule',
  'layout.columns': 'Columns',
  'layout.surfaceTarget': 'Surface target',
  'sectionDisplay.skill.style': 'Skill display',
  'sectionDisplay.language.style': 'Language display',
  'pageFormat': 'Page format',
  'dateFormat': 'Date format',
};

const GROUPS = [
  { title: 'Typography', fields: ['font.family', 'font.baseSizePx'] },
  { title: 'Colors', fields: [] },
  {
    title: 'Spacing',
    fields: ['spacing.sectionGap', 'spacing.entryGap', 'spacing.lineHeight'],
  },
  {
    title: 'Headings and header',
    fields: ['heading.style', 'heading.showRule'],
  },
  {
    title: 'Layout',
    fields: [
      'layout.columns',
      'layout.surfaceTarget',
      'sectionDisplay.skill.style',
      'sectionDisplay.language.style',
      'pageFormat',
      'dateFormat',
    ],
  },
] as const;

function commit(deltas: readonly CustomizationDelta[]): void {
  props.actions.edit({ kind: 'customization', deltas });
}

function changeField(field: CustomizationField, event: Event): void {
  const value = valueFromEvent(field, event);
  if (value === undefined) {
    setLocalError(field.path, 'Enter a value within the allowed range.');
    return;
  }
  if (!isAllowed(field, value)) {
    setLocalError(field.path, 'Enter a value within the allowed range.');
    return;
  }
  setLocalError(field.path, '');
  if (value === valueAt(field.path)) return;
  commit([{ op: 'set', path: field.path, value }]);
}

function enablePageMargin(): void {
  commit([
    { op: 'set', path: 'spacing.pageMargin.x', value: 15 },
    { op: 'set', path: 'spacing.pageMargin.y', value: 15 },
  ]);
}

function unsetPageMargin(): void {
  commit([{ op: 'unset', path: 'spacing.pageMargin' }]);
}

function enableHeader(): void {
  commit([
    { op: 'set', path: 'header.align', value: 'left' },
    { op: 'set', path: 'header.detailsLayout', value: 'inline' },
    { op: 'set', path: 'header.iconStyle', value: 'outline' },
  ]);
}

function unsetHeader(): void {
  commit([{ op: 'unset', path: 'header' }]);
}

function commitBoolean(field: CustomizationField, value: boolean): void {
  if (!isAllowed(field, value) || value === valueAt(field.path)) return;
  commit([{ op: 'set', path: field.path, value }]);
}

function commitEnum(
  field: CustomizationField,
  value: string | number,
): void {
  const typed = field.values?.every((item) => typeof item === 'number')
    ? Number(value)
    : value;
  if (!isAllowed(field, typed)) {
    setLocalError(field.path, 'Choose one of the available options.');
    return;
  }
  setLocalError(field.path, '');
  if (typed === valueAt(field.path)) return;
  commit([{ op: 'set', path: field.path, value: typed }]);
}

function unsetSurfaceTarget(): void {
  if (customization.value?.layout.surfaceTarget === undefined) return;
  commit([{ op: 'unset', path: 'layout.surfaceTarget' }]);
}

function valueAt(path: CustomizationSetPath): JsonValue | undefined {
  let current: unknown = customization.value;
  for (const part of path.split('.')) {
    if (current === null || typeof current !== 'object') return undefined;
    current = (current as Record<string, unknown>)[part];
  }
  return isJsonValue(current) ? current : undefined;
}

function displayValue(
  path: CustomizationSetPath,
  fallback: JsonValue,
): JsonValue {
  return valueAt(path) ?? fallback;
}

function fieldFor(path: CustomizationSetPath): CustomizationField | undefined {
  return CUSTOMIZATION_FIELDS.find((field) => field.path === path);
}

function valuesFor(field: CustomizationField): readonly (string | number)[] {
  return field.values ?? [];
}

function valueFromEvent(
  field: CustomizationField,
  event: Event,
): JsonValue | undefined {
  const target = event.target;
  if (target instanceof HTMLInputElement && field.kind === 'boolean') {
    return target.checked;
  }
  if (target instanceof HTMLInputElement && isNumeric(field.kind)) {
    const numericText = target.value.trim();
    if (numericText === '') return undefined;
    const value = Number(numericText);
    return Number.isFinite(value) ? value : undefined;
  }
  if (!(target instanceof HTMLSelectElement)) return undefined;
  if (field.values?.every((value) => typeof value === 'number') ?? false) {
    const value = Number(target.value);
    return Number.isFinite(value) ? value : undefined;
  }
  return target.value;
}

function isAllowed(field: CustomizationField, value: JsonValue): boolean {
  if (field.kind === 'boolean') return typeof value === 'boolean';
  if (field.kind === 'enum') {
    return field.values?.includes(value as never) ?? false;
  }
  if (!isNumeric(field.kind) || typeof value !== 'number') return false;
  if (field.kind === 'integer' && !Number.isInteger(value)) return false;
  return (field.minimum === undefined || value >= field.minimum)
    && (field.maximum === undefined || value <= field.maximum);
}

function isNumeric(kind: CustomizationField['kind']): boolean {
  return kind === 'integer' || kind === 'number';
}

function setLocalError(path: CustomizationSetPath, message: string): void {
  localErrors.value = { ...localErrors.value, [path]: message };
}

function fieldId(path: CustomizationSetPath): string {
  return `customization-${path.replaceAll('.', '-')}`;
}

function localError(path: CustomizationSetPath): string {
  return localErrors.value[path] ?? '';
}

function labelFor(path: CustomizationSetPath): string {
  return LABELS[path] ?? path;
}

function colorValue(path: string): string | undefined {
  const key = path.split('.')[1] as
    | 'primary' | 'text' | 'background' | 'accent' | 'surface';
  return customization.value?.colors[key];
}

function pathFor(path: string): CustomizationSetPath {
  return path as CustomizationSetPath;
}

function typedDisplay(
  path: CustomizationSetPath,
  fallback: JsonValue,
): string | number {
  const value = displayValue(path, fallback);
  return typeof value === 'string' || typeof value === 'number' ? value : '';
}

function unsetColor(path: string): void {
  if (path === 'colors.accent') {
    commit([{ op: 'unset', path: 'colors.accent' }]);
  } else if (path === 'colors.surface') {
    commit([{ op: 'unset', path: 'colors.surface' }]);
  }
}

function fieldForIssue(path: string): CustomizationSetPath | undefined {
  const normalized = path
    .replace(/^\/?customization(?:[./]|$)/, '')
    .replaceAll('/', '.');
  return CUSTOMIZATION_FIELDS.find((field) => field.path === normalized)?.path;
}

function focusIssue(path: string): void {
  const field = fieldForIssue(path);
  if (field === undefined) return;
  const selector = `[data-field="${field}"] input, `
    + `[data-field="${field}"] select, `
    + `[data-field="${field}"] [role="checkbox"]`;
  root.value?.querySelector<HTMLElement>(selector)?.focus();
}

function messageForCode(code: string): string {
  switch (code) {
    case 'maximum':
    case 'minimum':
      return 'Enter a value within the allowed range.';
    case 'enum':
      return 'Choose one of the available options.';
    case 'pattern':
      return 'Enter a value in the required format.';
    default:
      return 'This value needs attention.';
  }
}

function isJsonValue(value: unknown): value is JsonValue {
  return value === null || typeof value === 'boolean'
    || typeof value === 'number'
    || typeof value === 'string' || Array.isArray(value)
    || (typeof value === 'object' && value !== null);
}

function customizationValue(): Customization | undefined {
  return customization.value;
}
</script>

<template>
  <InspectorPanel
    v-if="customizationValue() !== undefined"
    title="Customization"
    title-id="customization-title"
  >
    <div ref="root">
      <Card
        v-for="group in GROUPS"
        :key="group.title"
      >
        <CardHeader><CardTitle>{{ group.title }}</CardTitle></CardHeader>
        <CardContent class="grid gap-4">
          <template v-if="group.title === 'Colors'">
            <div
              v-for="color in [
                ['colors.primary', 'Primary color', true],
                ['colors.text', 'Text color', true],
                ['colors.background', 'Background color', true],
                ['colors.accent', 'Accent color', false],
                ['colors.surface', 'Surface color', false],
              ]"
              :key="color[0]"
              :data-field="color[0]"
            >
              <ColorField
                :field-id="color[0]"
                :label="color[1]"
                :fallback="color[0] === 'colors.accent'
                  ? customizationValue()?.colors.primary
                  : customizationValue()?.colors.background"
                :model-value="colorValue(color[0])"
                :required="color[2]"
                :unset-action="color[0] === 'colors.accent'
                  ? 'unset-accent' : 'unset-surface'"
                @set="commit([{
                  op: 'set', path: pathFor(color[0]), value: $event,
                }])"
                @unset="unsetColor(color[0])"
              />
            </div>
          </template>
          <template v-else>
            <template
              v-for="path in group.fields"
              :key="path"
            >
              <CheckboxField
                v-if="fieldFor(path)?.kind === 'boolean'"
                :id="fieldId(path)"
                :label="labelFor(path)"
                :model-value="displayValue(path, false) === true"
                :name="path"
                @update:model-value="commitBoolean(fieldFor(path)!, $event)"
              />
              <SelectField
                v-else-if="fieldFor(path)?.kind === 'enum'"
                :id="fieldId(path)"
                :error="localError(path) || undefined"
                :label="labelFor(path)"
                :model-value="typedDisplay(
                  path, path === 'layout.surfaceTarget' ? 'none' : '',
                )"
                :name="path"
                :options="valuesFor(fieldFor(path)!).map((value) => ({
                  value, label: String(value),
                }))"
                @update:model-value="commitEnum(fieldFor(path)!, $event)"
              />
              <FormField
                v-else
                :id="fieldId(path)"
                v-slot="{ id, describedBy, invalid }"
                :error="localError(path) || undefined"
                :label="labelFor(path)"
                :name="path"
              >
                <Input
                  :id="id"
                  :aria-describedby="describedBy"
                  :aria-invalid="invalid"
                  :max="fieldFor(path)?.maximum"
                  :min="fieldFor(path)?.minimum"
                  :model-value="typedDisplay(path, 0)"
                  :step="fieldFor(path)?.kind === 'integer' ? 1 : 'any'"
                  type="number"
                  @change="changeField(fieldFor(path)!, $event)"
                />
              </FormField>
            </template>
            <SwitchField
              v-if="group.title === 'Spacing'"
              data-action="page-margin"
              label="Page margins"
              :model-value="customizationValue()?.spacing.pageMargin
                !== undefined"
              @update:model-value="$event
                ? enablePageMargin() : unsetPageMargin()"
            />
            <div
              v-if="group.title === 'Spacing'
                && customizationValue()?.spacing.pageMargin !== undefined"
              class="grid gap-4"
            >
              <FormField
                v-for="path in ['spacing.pageMargin.x', 'spacing.pageMargin.y']"
                :id="fieldId(pathFor(path))"
                :key="path"
                v-slot="{ id, describedBy, invalid }"
                :error="localError(pathFor(path)) || undefined"
                :label="path.endsWith('.x')
                  ? 'Horizontal margin' : 'Vertical margin'"
                :name="path"
              >
                <Input
                  :id="id"
                  :aria-describedby="describedBy"
                  :aria-invalid="invalid"
                  :max="fieldFor(pathFor(path))?.maximum"
                  :min="fieldFor(pathFor(path))?.minimum"
                  :model-value="typedDisplay(pathFor(path), 15)"
                  step="any"
                  type="number"
                  @change="changeField(fieldFor(pathFor(path))!, $event)"
                />
              </FormField>
            </div>
            <SwitchField
              v-if="group.title === 'Headings and header'"
              data-action="header"
              label="Header"
              :model-value="customizationValue()?.header !== undefined"
              @update:model-value="$event
                ? enableHeader() : unsetHeader()"
            />
            <div
              v-if="group.title === 'Headings and header'
                && customizationValue()?.header !== undefined"
              class="grid gap-4"
            >
              <template
                v-for="path in [
                  'header.align', 'header.detailsLayout', 'header.iconStyle',
                ]"
                :key="path"
              >
                <SelectField
                  :id="fieldId(pathFor(path))"
                  :error="localError(pathFor(path)) || undefined"
                  :label="path === 'header.align'
                    ? 'Header alignment'
                    : path === 'header.detailsLayout'
                      ? 'Contact layout' : 'Icon style'"
                  :model-value="typedDisplay(
                    pathFor(path), path === 'header.align' ? 'left'
                      : path === 'header.detailsLayout' ? 'inline' : 'outline',
                  )"
                  :name="path"
                  :options="valuesFor(fieldFor(pathFor(path))!).map(
                    (value) => ({
                      value, label: String(value),
                    }))"
                  @update:model-value="commitEnum(
                    fieldFor(pathFor(path))!, $event,
                  )"
                />
              </template>
            </div>
            <Button
              v-if="group.title === 'Layout'
                && customizationValue()?.layout.surfaceTarget !== undefined"
              variant="ghost"
              size="sm"
              data-action="unset-surface-target"
              @click="unsetSurfaceTarget"
            >
              Remove surface target
            </Button>
          </template>
        </CardContent>
      </Card>
    </div>

    <ul v-if="issues.length > 0">
      <li
        v-for="issue in issues"
        :key="`${issue.path}:${issue.code}`"
      >
        <Button
          v-if="fieldForIssue(issue.path) !== undefined"
          type="button"
          variant="ghost"
          size="sm"
          :data-issue="issue.path"
          @click="focusIssue(issue.path)"
        >
          {{ messageForCode(issue.code) }}
        </Button>
        <span v-else>{{ messageForCode(issue.code) }}</span>
      </li>
    </ul>
  </InspectorPanel>
</template>
