<script setup lang="ts">
import type { PersonalDetail, PersonalDetails } from '@aboutme/schema';
import { computed, ref } from 'vue';
import { Button } from '@/components/ui/button';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import InspectorPanel from '../InspectorPanel.vue';
import TextField from '../../app/TextField.vue';
import ContactList from './ContactList.vue';
import type { FieldIntent } from './fieldIntent';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly personal: PersonalDetails;
}>();

const panel = ref<{ $el?: HTMLElement } | null>(null);
const contactList = ref<{
  focusField?: (
    index: number,
    field: ContactField,
  ) => Promise<void>;
} | null>(null);
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

async function focusField(path: string): Promise<void> {
  const textField = textFieldForIssue(path);
  if (textField !== undefined) {
    panel.value?.$el?.querySelector<HTMLElement>(
      `[data-field="${textField}"] [data-field-input]`,
    )?.focus();
    return;
  }
  const contactField = contactFieldForIssue(path);
  if (contactField === undefined) return;
  if (contactList.value?.focusField !== undefined) {
    await contactList.value.focusField(
      contactField.index,
      contactField.field,
    );
    return;
  }
  panel.value?.$el?.querySelector<HTMLElement>(
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
  <InspectorPanel
    ref="panel"
    title="Personal details"
    title-id="personal-details-title"
  >
    <TextField
      :error="issueFor('fullName')"
      label="Full name"
      :model-value="personal.fullName"
      name="fullName"
      @intent="editText('fullName', $event)"
    />
    <TextField
      :error="issueFor('headline')"
      label="Headline"
      :model-value="personal.headline"
      name="headline"
      @intent="editText('headline', $event)"
    />
    <h3 class="sr-only">
      Contact details
    </h3>
    <ContactList
      ref="contactList"
      :details="personal.details"
      :create-entity-id="actions.createEntityId"
      @change="editDetails"
      @unset="unsetDetails"
    />
    <ul
      v-if="contactIssues.length > 0 || unmappedIssues.length > 0"
      class="grid gap-1"
    >
      <li
        v-for="issue in contactIssues"
        :key="`${issue.path}:${issue.code}`"
      >
        <Button
          :data-issue="issue.path"
          size="sm"
          variant="link"
          @click="focusField(issue.path)"
        >
          {{ messageForCode(issue.code) }}
        </Button>
      </li>
      <li
        v-for="issue in unmappedIssues"
        :key="`${issue.path}:${issue.code}`"
        class="text-sm text-destructive"
      >
        {{ messageForCode(issue.code) }}
      </li>
    </ul>
  </InspectorPanel>
</template>
