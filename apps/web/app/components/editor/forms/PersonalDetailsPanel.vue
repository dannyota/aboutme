<script setup lang="ts">
import type { PersonalDetail, PersonalDetails } from '@aboutme/schema';
import { computed, ref } from 'vue';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import ContactList from './ContactList.vue';
import type { FieldIntent } from './fieldIntent';
import OptionalField from './OptionalField.vue';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly personal: PersonalDetails;
}>();

const panel = ref<HTMLElement | null>(null);
const issues = computed(() =>
  Object.values(props.actions.record.value?.issues ?? {}).flat(),
);
const unmappedIssues = computed(() =>
  issues.value.filter((issue) => !isKnownIssue(issue.path)),
);
const contactIssues = computed(() =>
  issues.value.filter(
    (issue) => contactFieldForIssue(issue.path) !== undefined,
  ),
);

function editText(
  path: 'fullName' | 'headline',
  intent: FieldIntent<string>,
): void {
  props.actions.edit({
    kind: 'personalField',
    path,
    value:
      intent.kind === 'unset'
        ? { present: false }
        : { present: true, value: intent.value },
  });
}

function editDetails(details: readonly PersonalDetail[]): void {
  props.actions.edit({
    kind: 'personalField',
    path: 'details',
    value: { present: true, value: details },
  });
}

function unsetDetails(): void {
  props.actions.edit({
    kind: 'personalField',
    path: 'details',
    value: { present: false },
  });
}

function issueFor(field: 'fullName' | 'headline'): string | undefined {
  const issue = issues.value.find(
    (candidate) => textFieldForIssue(candidate.path) === field,
  );
  return issue === undefined ? undefined : messageForCode(issue.code);
}

function focusField(path: string): void {
  const textField = textFieldForIssue(path);
  if (textField !== undefined) {
    panel.value?.querySelector<HTMLElement>(
      `[data-field="${textField}"] input`,
    )?.focus();
    return;
  }
  const contactField = contactFieldForIssue(path);
  if (contactField === undefined) return;
  panel.value?.querySelector<HTMLElement>(
    `[data-detail-index="${contactField.index}"] `
    + `[data-detail-${contactField.field}]`,
  )?.focus();
}

function textFieldForIssue(path: string): 'fullName' | 'headline' | undefined {
  switch (path) {
    case 'personalDetails.fullName':
    case '/personalDetails/fullName':
      return 'fullName';
    case 'personalDetails.headline':
    case '/personalDetails/headline':
      return 'headline';
    default:
      return undefined;
  }
}

type ContactField = 'value' | 'label' | 'type' | 'is-hidden';

interface ContactIssueField {
  readonly field: ContactField;
  readonly index: number;
}

function contactFieldForIssue(path: string): ContactIssueField | undefined {
  const match
    = /^personalDetails\.details\[(\d+)\]\.(value|label|type|isHidden)$/.exec(
      path,
    );
  if (match === null) return undefined;
  const index = Number(match[1]);
  if (!Number.isSafeInteger(index)) return undefined;
  const matchedField = match[2];
  if (matchedField === undefined) return undefined;
  const field = contactFieldName(matchedField);
  if (field === undefined) return undefined;
  return { field, index };
}

function contactFieldName(field: string): ContactField | undefined {
  switch (field) {
    case 'value':
    case 'label':
    case 'type':
      return field;
    case 'isHidden':
      return 'is-hidden';
    default:
      return undefined;
  }
}

function isKnownIssue(path: string): boolean {
  return textFieldForIssue(path) !== undefined
    || contactFieldForIssue(path) !== undefined;
}

function messageForCode(code: string): string {
  switch (code) {
    case 'max_length':
      return 'This value is too long.';
    case 'format':
      return 'Enter a value in the required format.';
    default:
      return 'This value needs attention.';
  }
}
</script>

<template>
  <section
    ref="panel"
    aria-labelledby="personal-details-title"
  >
    <h2 id="personal-details-title">
      Personal details
    </h2>
    <div data-field="fullName">
      <OptionalField
        label="Full name"
        :model-value="personal.fullName"
        @intent="editText('fullName', $event)"
      />
      <button
        v-if="issueFor('fullName') !== undefined"
        type="button"
        data-issue="personalDetails.fullName"
        @click="focusField('personalDetails.fullName')"
      >
        {{ issueFor("fullName") }}
      </button>
    </div>
    <div data-field="headline">
      <OptionalField
        label="Headline"
        :model-value="personal.headline"
        @intent="editText('headline', $event)"
      />
      <button
        v-if="issueFor('headline') !== undefined"
        type="button"
        data-issue="personalDetails.headline"
        @click="focusField('personalDetails.headline')"
      >
        {{ issueFor("headline") }}
      </button>
    </div>
    <ContactList
      :details="personal.details"
      :create-entity-id="actions.createEntityId"
      @change="editDetails"
      @unset="unsetDetails"
    />
    <ul v-if="contactIssues.length > 0 || unmappedIssues.length > 0">
      <li
        v-for="issue in contactIssues"
        :key="`${issue.path}:${issue.code}`"
      >
        <button
          type="button"
          :data-issue="issue.path"
          @click="focusField(issue.path)"
        >
          {{ messageForCode(issue.code) }}
        </button>
      </li>
      <li
        v-for="issue in unmappedIssues"
        :key="`${issue.path}:${issue.code}`"
      >
        {{ messageForCode(issue.code) }}
      </li>
    </ul>
  </section>
</template>
