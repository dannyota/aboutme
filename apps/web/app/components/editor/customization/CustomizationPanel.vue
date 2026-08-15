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
import {
  CUSTOMIZATION_FIELDS,
  type CustomizationField,
} from './fields';
import ColorField from './ColorField.vue';
import OptionalCustomizationField from './OptionalCustomizationField.vue';

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
const scalarFields = CUSTOMIZATION_FIELDS.filter(
  (field) => !isSpecialField(field.path),
);

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

function changePageMargin(path: CustomizationSetPath, event: Event): void {
  const field = fieldFor(path);
  if (field === undefined) return;
  changeField(field, event);
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

function changeSurfaceTarget(event: Event): void {
  const field = fieldFor('layout.surfaceTarget');
  if (field === undefined) return;
  const value = valueFromEvent(field, event);
  if (value === undefined || !isAllowed(field, value)) return;
  if (value === (customization.value?.layout.surfaceTarget ?? 'none')) return;
  commit([{ op: 'set', path: 'layout.surfaceTarget', value }]);
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

function valuesForPath(
  path: CustomizationSetPath,
): readonly (string | number)[] {
  const field = fieldFor(path);
  return field === undefined ? [] : valuesFor(field);
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

function isSpecialField(path: CustomizationSetPath): boolean {
  return path.startsWith('colors.')
    || path.startsWith('spacing.pageMargin.')
    || path.startsWith('header.')
    || path === 'layout.surfaceTarget';
}

function setLocalError(path: CustomizationSetPath, message: string): void {
  localErrors.value = { ...localErrors.value, [path]: message };
}

function fieldId(path: CustomizationSetPath): string {
  return `customization-${path.replaceAll('.', '-')}`;
}

function errorId(path: CustomizationSetPath): string {
  return `${fieldId(path)}-error`;
}

function localError(path: CustomizationSetPath): string {
  return localErrors.value[path] ?? '';
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
    + `[data-field="${field}"] select`;
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
  <section
    v-if="customizationValue() !== undefined"
    ref="root"
    aria-labelledby="customization-title"
  >
    <h2 id="customization-title">
      Customization
    </h2>

    <div
      v-for="field in scalarFields"
      :key="field.path"
      :data-field="field.path"
    >
      <label
        v-if="field.kind === 'boolean'"
        :for="fieldId(field.path)"
      >
        {{ field.path }}
        <input
          :id="fieldId(field.path)"
          type="checkbox"
          :checked="displayValue(field.path, false) === true"
          :aria-invalid="localError(field.path) === '' ? undefined : 'true'"
          :aria-describedby="localError(field.path) === ''
            ? undefined : errorId(field.path)"
          @change="changeField(field, $event)"
        >
      </label>
      <label
        v-else-if="field.kind === 'enum'"
        :for="fieldId(field.path)"
      >
        {{ field.path }}
        <select
          :id="fieldId(field.path)"
          :value="displayValue(field.path, '')"
          :aria-invalid="localError(field.path) === '' ? undefined : 'true'"
          :aria-describedby="localError(field.path) === ''
            ? undefined : errorId(field.path)"
          @change="changeField(field, $event)"
        >
          <option
            v-for="value in valuesFor(field)"
            :key="String(value)"
            :value="value"
          >
            {{ value }}
          </option>
        </select>
      </label>
      <label
        v-else
        :for="fieldId(field.path)"
      >
        {{ field.path }}
        <input
          :id="fieldId(field.path)"
          type="number"
          :min="field.minimum"
          :max="field.maximum"
          :step="field.kind === 'integer' ? 1 : 'any'"
          :value="displayValue(field.path, 0)"
          :aria-invalid="localError(field.path) === '' ? undefined : 'true'"
          :aria-describedby="localError(field.path) === ''
            ? undefined : errorId(field.path)"
          @change="changeField(field, $event)"
        >
      </label>
      <p
        v-if="localError(field.path) !== ''"
        :id="errorId(field.path)"
        :data-error-for="field.path"
        role="alert"
      >
        {{ localError(field.path) }}
      </p>
    </div>

    <div data-field="colors.primary">
      <ColorField
        field-id="colors.primary"
        label="Primary color"
        :model-value="customizationValue()?.colors.primary"
        required
        @set="commit([{ op: 'set', path: 'colors.primary', value: $event }])"
      />
    </div>
    <div data-field="colors.text">
      <ColorField
        field-id="colors.text"
        label="Text color"
        :model-value="customizationValue()?.colors.text"
        required
        @set="commit([{ op: 'set', path: 'colors.text', value: $event }])"
      />
    </div>
    <div data-field="colors.background">
      <ColorField
        field-id="colors.background"
        label="Background color"
        :model-value="customizationValue()?.colors.background"
        required
        @set="commit([{ op: 'set', path: 'colors.background', value: $event }])"
      />
    </div>
    <div data-field="colors.accent">
      <ColorField
        field-id="colors.accent"
        label="Accent color"
        :fallback="customizationValue()?.colors.primary"
        :model-value="customizationValue()?.colors.accent"
        unset-action="unset-accent"
        @set="commit([{ op: 'set', path: 'colors.accent', value: $event }])"
        @unset="commit([{ op: 'unset', path: 'colors.accent' }])"
      />
    </div>
    <div data-field="colors.surface">
      <ColorField
        field-id="colors.surface"
        label="Surface color"
        :fallback="customizationValue()?.colors.background"
        :model-value="customizationValue()?.colors.surface"
        unset-action="unset-surface"
        @set="commit([{ op: 'set', path: 'colors.surface', value: $event }])"
        @unset="commit([{ op: 'unset', path: 'colors.surface' }])"
      />
    </div>

    <OptionalCustomizationField
      action="page-margin"
      label="Page margins"
      :present="customizationValue()?.spacing.pageMargin !== undefined"
      @enable="enablePageMargin"
      @unset="unsetPageMargin"
    >
      <label
        data-field="spacing.pageMargin.x"
        :for="fieldId('spacing.pageMargin.x')"
      >
        Horizontal margin
        <input
          :id="fieldId('spacing.pageMargin.x')"
          type="number"
          :min="fieldFor('spacing.pageMargin.x')?.minimum"
          :max="fieldFor('spacing.pageMargin.x')?.maximum"
          step="any"
          :value="displayValue('spacing.pageMargin.x', 15)"
          :aria-invalid="localError('spacing.pageMargin.x') === ''
            ? undefined : 'true'"
          :aria-describedby="localError('spacing.pageMargin.x') === ''
            ? undefined : errorId('spacing.pageMargin.x')"
          @change="changePageMargin('spacing.pageMargin.x', $event)"
        >
      </label>
      <p
        v-if="localError('spacing.pageMargin.x') !== ''"
        :id="errorId('spacing.pageMargin.x')"
        data-error-for="spacing.pageMargin.x"
        role="alert"
      >
        {{ localError('spacing.pageMargin.x') }}
      </p>
      <label
        data-field="spacing.pageMargin.y"
        :for="fieldId('spacing.pageMargin.y')"
      >
        Vertical margin
        <input
          :id="fieldId('spacing.pageMargin.y')"
          type="number"
          :min="fieldFor('spacing.pageMargin.y')?.minimum"
          :max="fieldFor('spacing.pageMargin.y')?.maximum"
          step="any"
          :value="displayValue('spacing.pageMargin.y', 15)"
          :aria-invalid="localError('spacing.pageMargin.y') === ''
            ? undefined : 'true'"
          :aria-describedby="localError('spacing.pageMargin.y') === ''
            ? undefined : errorId('spacing.pageMargin.y')"
          @change="changePageMargin('spacing.pageMargin.y', $event)"
        >
      </label>
      <p
        v-if="localError('spacing.pageMargin.y') !== ''"
        :id="errorId('spacing.pageMargin.y')"
        data-error-for="spacing.pageMargin.y"
        role="alert"
      >
        {{ localError('spacing.pageMargin.y') }}
      </p>
    </OptionalCustomizationField>

    <OptionalCustomizationField
      action="header"
      label="Header"
      :present="customizationValue()?.header !== undefined"
      @enable="enableHeader"
      @unset="unsetHeader"
    >
      <label
        data-field="header.align"
        :for="fieldId('header.align')"
      >
        Header alignment
        <select
          :id="fieldId('header.align')"
          :value="displayValue('header.align', 'left')"
          :aria-invalid="localError('header.align') === '' ? undefined : 'true'"
          :aria-describedby="localError('header.align') === ''
            ? undefined : errorId('header.align')"
          @change="changeField(fieldFor('header.align')!, $event)"
        >
          <option
            v-for="value in valuesForPath('header.align')"
            :key="String(value)"
            :value="value"
          >
            {{ value }}
          </option>
        </select>
      </label>
      <p
        v-if="localError('header.align') !== ''"
        :id="errorId('header.align')"
        data-error-for="header.align"
        role="alert"
      >
        {{ localError('header.align') }}
      </p>
      <label
        data-field="header.detailsLayout"
        :for="fieldId('header.detailsLayout')"
      >
        Contact layout
        <select
          :id="fieldId('header.detailsLayout')"
          :value="displayValue('header.detailsLayout', 'inline')"
          :aria-invalid="localError('header.detailsLayout') === ''
            ? undefined : 'true'"
          :aria-describedby="localError('header.detailsLayout') === ''
            ? undefined : errorId('header.detailsLayout')"
          @change="changeField(fieldFor('header.detailsLayout')!, $event)"
        >
          <option
            v-for="value in valuesForPath('header.detailsLayout')"
            :key="String(value)"
            :value="value"
          >
            {{ value }}
          </option>
        </select>
      </label>
      <p
        v-if="localError('header.detailsLayout') !== ''"
        :id="errorId('header.detailsLayout')"
        data-error-for="header.detailsLayout"
        role="alert"
      >
        {{ localError('header.detailsLayout') }}
      </p>
      <label
        data-field="header.iconStyle"
        :for="fieldId('header.iconStyle')"
      >
        Icon style
        <select
          :id="fieldId('header.iconStyle')"
          :value="displayValue('header.iconStyle', 'outline')"
          :aria-invalid="localError('header.iconStyle') === ''
            ? undefined : 'true'"
          :aria-describedby="localError('header.iconStyle') === ''
            ? undefined : errorId('header.iconStyle')"
          @change="changeField(fieldFor('header.iconStyle')!, $event)"
        >
          <option
            v-for="value in valuesForPath('header.iconStyle')"
            :key="String(value)"
            :value="value"
          >
            {{ value }}
          </option>
        </select>
      </label>
      <p
        v-if="localError('header.iconStyle') !== ''"
        :id="errorId('header.iconStyle')"
        data-error-for="header.iconStyle"
        role="alert"
      >
        {{ localError('header.iconStyle') }}
      </p>
    </OptionalCustomizationField>

    <div data-field="layout.surfaceTarget">
      <label :for="fieldId('layout.surfaceTarget')">
        Surface target
        <select
          :id="fieldId('layout.surfaceTarget')"
          :value="displayValue('layout.surfaceTarget', 'none')"
          :aria-invalid="localError('layout.surfaceTarget') === ''
            ? undefined : 'true'"
          :aria-describedby="localError('layout.surfaceTarget') === ''
            ? undefined : errorId('layout.surfaceTarget')"
          @change="changeSurfaceTarget"
        >
          <option
            v-for="value in valuesForPath('layout.surfaceTarget')"
            :key="String(value)"
            :value="value"
          >
            {{ value }}
          </option>
        </select>
      </label>
      <p
        v-if="localError('layout.surfaceTarget') !== ''"
        :id="errorId('layout.surfaceTarget')"
        data-error-for="layout.surfaceTarget"
        role="alert"
      >
        {{ localError('layout.surfaceTarget') }}
      </p>
      <button
        type="button"
        data-action="unset-surface-target"
        @click="unsetSurfaceTarget"
      >
        Remove surface target
      </button>
    </div>

    <ul v-if="issues.length > 0">
      <li
        v-for="issue in issues"
        :key="`${issue.path}:${issue.code}`"
      >
        <button
          v-if="fieldForIssue(issue.path) !== undefined"
          type="button"
          :data-issue="issue.path"
          @click="focusIssue(issue.path)"
        >
          {{ messageForCode(issue.code) }}
        </button>
        <span v-else>{{ messageForCode(issue.code) }}</span>
      </li>
    </ul>
  </section>
</template>
