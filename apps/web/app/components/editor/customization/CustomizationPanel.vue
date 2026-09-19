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
import FormField from '../../app/FormField.vue';
import SelectField from '../../app/SelectField.vue';
import SwitchField from '../../app/SwitchField.vue';
import { Button } from '../../ui/button';
import { Input } from '../../ui/input';
import InspectorPanel from '../InspectorPanel.vue';
import { CUSTOMIZATION_FIELDS, type CustomizationField } from './fields';
import ColorField from './ColorField.vue';
import PageSettings from './PageSettings.vue';
import { editorControlsCopy } from '../../../i18n/editor-controls';
import { enumLabel, FIELD_GROUPS, fieldLabel } from './labels';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly record?: ResumeRecord;
}>();
const { locale } = useLocale();
const copy = computed(() => editorControlsCopy[locale.value]);

type LocalError = 'validationRange' | 'chooseOption' | null;

const root = ref<HTMLElement | null>(null);
const localErrors = ref<
  Readonly<Partial<Record<CustomizationSetPath, LocalError>>>
>({});
const record = computed(() => props.record ?? props.actions.record.value);
const customization = computed(
  () => record.value?.current.document.customization,
);
const issues = computed(() => Object.values(record.value?.issues ?? {}).flat());
function commit(deltas: readonly CustomizationDelta[]): void {
  props.actions.edit({ kind: 'customization', deltas });
}

function changeField(field: CustomizationField, event: Event): void {
  const value = valueFromEvent(field, event);
  if (value === undefined) {
    setLocalError(field.path, 'validationRange');
    return;
  }
  if (!isAllowed(field, value)) {
    setLocalError(field.path, 'validationRange');
    return;
  }
  setLocalError(field.path, null);
  if (value === valueAt(field.path)) return;
  commit([{ op: 'set', path: field.path, value }]);
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

function commitEnum(field: CustomizationField, value: string | number): void {
  const typed = field.values?.every((item) => typeof item === 'number')
    ? Number(value)
    : value;
  if (!isAllowed(field, typed)) {
    setLocalError(field.path, 'chooseOption');
    return;
  }
  setLocalError(field.path, null);
  if (field.path === 'font.textAlign') {
    commitTextAlign(typed);
    return;
  }
  if (typed === valueAt(field.path)) return;
  commit([{ op: 'set', path: field.path, value: typed }]);
}

// Absent `font.textAlign` means left (ADR 0041), so Left clears the key.
function commitTextAlign(value: string | number): void {
  const stored = valueAt('font.textAlign');
  if (value === 'justify') {
    if (stored !== 'justify') {
      commit([{ op: 'set', path: 'font.textAlign', value }]);
    }
  } else if (stored !== undefined) {
    commit([{ op: 'unset', path: 'font.textAlign' }]);
  }
}

const photoPositionHint = computed(() => copy.value.controls.photoPositionHint);
const photoPosition = computed(
  () => customization.value?.header?.photoPosition ?? 'top',
);
const hasPhoto = computed(
  () => record.value?.current.document.personalDetails.photo !== undefined,
);

// Absent means top (ADR 0044). Left or right on a resume with no header
// creates it with the Header switch's defaults; Top clears the stored value.
function commitPhotoPosition(value: string): void {
  if (value !== 'top' && value !== 'left' && value !== 'right') return;
  const header = customization.value?.header;
  const current = header?.photoPosition ?? 'top';
  if (value === current) return;
  if (value === 'top') {
    commit([{ op: 'unset', path: 'header.photoPosition' }]);
  } else if (header === undefined) {
    commit([
      { op: 'set', path: 'header.align', value: 'left' },
      { op: 'set', path: 'header.detailsLayout', value: 'inline' },
      { op: 'set', path: 'header.iconStyle', value: 'outline' },
      { op: 'set', path: 'header.photoPosition', value },
    ]);
  } else {
    commit([{ op: 'set', path: 'header.photoPosition', value }]);
  }
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
  return (
    (field.minimum === undefined || value >= field.minimum)
    && (field.maximum === undefined || value <= field.maximum)
  );
}

function isNumeric(kind: CustomizationField['kind']): boolean {
  return kind === 'integer' || kind === 'number';
}

function setLocalError(path: CustomizationSetPath, error: LocalError): void {
  localErrors.value = { ...localErrors.value, [path]: error };
}

function fieldId(path: CustomizationSetPath): string {
  return `customization-${path.replaceAll('.', '-')}`;
}

function localError(path: CustomizationSetPath): string {
  const error = localErrors.value[path];
  return error === null || error === undefined
    ? ''
    : copy.value.controls[error];
}

function labelFor(path: CustomizationSetPath): string {
  return fieldLabel(locale.value, path);
}

function isDeferredPath(path: string): boolean {
  return (
    path === 'header.align'
    || path === 'header.detailsLayout'
    || path === 'header.iconStyle'
    || path === 'header.photoPosition'
  );
}

function colorValue(path: string): string | undefined {
  const key = path.split('.')[1] as
    'primary' | 'text' | 'background' | 'accent' | 'surface';
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
  const selector
    = `[data-field="${field}"] input, `
      + `[data-field="${field}"] select, `
      + `[data-field="${field}"] [role="checkbox"], `
      + `[data-field="${field}"] [role="switch"]`;
  root.value?.querySelector<HTMLElement>(selector)?.focus();
}

function messageForCode(code: string): string {
  switch (code) {
    case 'maximum':
    case 'minimum':
      return copy.value.controls.validationRange;
    case 'enum':
      return copy.value.controls.chooseOption;
    case 'pattern':
      return copy.value.controls.validationFormat;
    default:
      return copy.value.controls.validationAttention;
  }
}

function isJsonValue(value: unknown): value is JsonValue {
  return (
    value === null
    || typeof value === 'boolean'
    || typeof value === 'number'
    || typeof value === 'string'
    || Array.isArray(value)
    || (typeof value === 'object' && value !== null)
  );
}

function customizationValue(): Customization | undefined {
  return customization.value;
}
</script>

<template>
  <InspectorPanel
    v-if="customizationValue() !== undefined"
    :title="copy.controls.customization"
    title-id="customization-title"
  >
    <div
      ref="root"
      class="grid gap-8"
    >
      <fieldset
        v-for="group in FIELD_GROUPS"
        :key="group.id"
        :data-customization-group="group.hook"
        class="grid gap-4"
      >
        <legend class="text-sm font-medium">
          {{ copy.groups[group.id] }}
        </legend>
        <div class="grid gap-4">
          <PageSettings
            v-if="group.id === 'page'"
            :customization="customizationValue()!"
            @commit="commit"
          />
          <template v-else-if="group.id === 'colors'">
            <div
              v-for="color in [
                ['colors.primary', true],
                ['colors.text', true],
                ['colors.background', true],
                ['colors.accent', false],
                ['colors.surface', false],
              ]"
              :key="color[0]"
              :data-field="color[0]"
            >
              <ColorField
                :field-id="color[0]"
                :label="labelFor(pathFor(color[0]))"
                :fallback="
                  color[0] === 'colors.accent'
                    ? customizationValue()?.colors.primary
                    : customizationValue()?.colors.background
                "
                :model-value="colorValue(color[0])"
                :required="color[1]"
                :unset-action="
                  color[0] === 'colors.accent'
                    ? 'unset-accent'
                    : 'unset-surface'
                "
                @set="
                  commit([
                    {
                      op: 'set',
                      path: pathFor(color[0]),
                      value: $event,
                    },
                  ])
                "
                @unset="unsetColor(color[0])"
              />
            </div>
          </template>
          <template v-else>
            <template
              v-for="path in group.paths"
              :key="path"
            >
              <template v-if="!isDeferredPath(path)">
                <div
                  v-if="fieldFor(path)?.kind === 'boolean'"
                  :data-field="path"
                >
                  <SwitchField
                    :id="fieldId(pathFor(path))"
                    :label="labelFor(pathFor(path))"
                    :model-value="displayValue(pathFor(path), false) === true"
                    @update:model-value="commitBoolean(fieldFor(path)!, $event)"
                  />
                </div>
                <SelectField
                  v-else-if="fieldFor(path)?.kind === 'enum'"
                  :id="fieldId(pathFor(path))"
                  :error="localError(pathFor(path)) || undefined"
                  :label="labelFor(pathFor(path))"
                  :model-value="
                    typedDisplay(
                      pathFor(path),
                      path === 'layout.surfaceTarget'
                        ? 'none'
                        : path === 'font.textAlign'
                          ? 'left'
                          : '',
                    )
                  "
                  :name="path"
                  :options="
                    valuesFor(fieldFor(path)!).map((value) => ({
                      value,
                      label: enumLabel(locale, path, value),
                    }))
                  "
                  @update:model-value="commitEnum(fieldFor(path)!, $event)"
                />
                <FormField
                  v-else
                  :id="fieldId(pathFor(path))"
                  v-slot="{ id, describedBy, invalid }"
                  :error="localError(pathFor(path)) || undefined"
                  :label="labelFor(pathFor(path))"
                  :name="path"
                >
                  <Input
                    :id="id"
                    :aria-describedby="describedBy"
                    :aria-invalid="invalid"
                    :max="fieldFor(pathFor(path))?.maximum"
                    :min="fieldFor(pathFor(path))?.minimum"
                    :model-value="typedDisplay(pathFor(path), 0)"
                    :step="
                      fieldFor(pathFor(path))?.kind === 'integer' ? 1 : 'any'
                    "
                    type="number"
                    @change="changeField(fieldFor(pathFor(path))!, $event)"
                  />
                </FormField>
              </template>
            </template>
            <SwitchField
              v-if="group.id === 'headings'"
              data-action="header"
              :label="copy.controls.header"
              :model-value="customizationValue()?.header !== undefined"
              @update:model-value="$event ? enableHeader() : unsetHeader()"
            />
            <div
              v-if="
                group.id === 'headings'
                  && customizationValue()?.header !== undefined
              "
              class="grid gap-4"
            >
              <template
                v-for="path in [
                  'header.align',
                  'header.detailsLayout',
                  'header.iconStyle',
                ]"
                :key="path"
              >
                <SelectField
                  :id="fieldId(pathFor(path))"
                  :error="localError(pathFor(path)) || undefined"
                  :label="labelFor(pathFor(path))"
                  :model-value="
                    typedDisplay(
                      pathFor(path),
                      path === 'header.align'
                        ? 'left'
                        : path === 'header.detailsLayout'
                          ? 'inline'
                          : 'outline',
                    )
                  "
                  :name="path"
                  :options="
                    valuesFor(fieldFor(pathFor(path))!).map((value) => ({
                      value,
                      label: enumLabel(locale, path, value),
                    }))
                  "
                  @update:model-value="
                    commitEnum(fieldFor(pathFor(path))!, $event)
                  "
                />
              </template>
            </div>
            <SelectField
              v-if="group.id === 'headings'"
              :id="fieldId('header.photoPosition')"
              :hint="hasPhoto ? undefined : photoPositionHint"
              :label="labelFor('header.photoPosition')"
              :model-value="photoPosition"
              name="header.photoPosition"
              :options="
                valuesFor(fieldFor('header.photoPosition')!).map((value) => ({
                  value,
                  label: enumLabel(locale, '', value),
                }))
              "
              @update:model-value="commitPhotoPosition"
            />
            <Button
              v-if="
                group.id === 'layout'
                  && customizationValue()?.layout.surfaceTarget !== undefined
              "
              variant="ghost"
              size="sm"
              data-action="unset-surface-target"
              @click="unsetSurfaceTarget"
            >
              {{ copy.controls.removeSurfaceTarget }}
            </Button>
          </template>
        </div>
      </fieldset>
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
