<script setup lang="ts">
import { computed, useId } from 'vue';

import SelectField from '@/components/app/SelectField.vue';
import SwitchField from '@/components/app/SwitchField.vue';
import { SHOWCASE_ROLES } from '@/lib/showcaseContract';
import type { PublishCopy } from '../../i18n/publish';
import type { ShowcaseBlockReason } from '../../editor/publishShowcase';

// The community showcase block of the publish dialog: the switch, its
// description, the Listed line, a showcase issue, and the optional role
// (docs/design/ui/landing-and-library.md, Publish dialog block).
const props = defineProps<{
  /** What the switch shows; false while it is disabled. */
  readonly modelValue: boolean;
  readonly role: string;
  /** Why the switch is disabled, if it is. */
  readonly reason: ShowcaseBlockReason | null;
  readonly busy: boolean;
  /** Whether the stored opt-in exists and the Listed line shows. */
  readonly listed: boolean;
  /** The server's showcase issue code, when one is showing. */
  readonly issueCode: string | null;
  readonly copy: PublishCopy;
}>();
const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  'update:role': [value: string];
}>();

const id = useId();
const helpId = `publish-showcase-help-${id}`;
const statusId = `publish-showcase-status-${id}`;
const issueId = `publish-showcase-issue-${id}`;
const showcase = computed(() => props.copy.showcase);
const description = computed(() => {
  if (props.reason === 'public') return showcase.value.needsPublic;
  if (props.reason === 'open-view') return showcase.value.needsOpenView;
  return showcase.value.help;
});
const issueMessage = computed(() => {
  if (props.issueCode === null) return null;
  if (props.issueCode === 'requires_open_view') {
    return showcase.value.issueOpenView;
  }
  return props.issueCode === 'requires_live'
    ? props.copy.issue.requires_live
    : props.copy.invalid;
});
const describedBy = computed(() => [
  helpId,
  ...(props.listed ? [statusId] : []),
  ...(issueMessage.value === null ? [] : [issueId]),
].join(' '));
const roleOptions = computed(() => [
  { value: '', label: showcase.value.noRole },
  ...SHOWCASE_ROLES.map((role) => ({
    value: role,
    label: showcase.value.roles[role],
  })),
]);
</script>

<template>
  <div
    class="grid gap-1"
    data-testid="publish-showcase"
  >
    <SwitchField
      :aria-describedby="describedBy"
      data-action="publish-showcase"
      :disabled="busy || reason !== null"
      :label="showcase.label"
      :model-value="modelValue"
      name="showcaseEnabled"
      @update:model-value="(value) => emit('update:modelValue', value)"
    />
    <p
      :id="helpId"
      class="ml-10 text-sm text-muted-foreground"
      data-testid="publish-showcase-description"
    >
      {{ description }}
    </p>
    <p
      v-if="listed"
      :id="statusId"
      class="ml-10 text-sm text-foreground"
      data-testid="publish-showcase-status"
    >
      {{ showcase.listed }}
      <a
        class="text-link underline underline-offset-4"
        data-action="publish-showcase-link"
        href="/showcase"
      >{{ showcase.listedLink }}</a>
    </p>
    <p
      v-if="issueMessage !== null"
      :id="issueId"
      class="ml-10 text-xs text-destructive"
      data-testid="publish-showcase-issue"
      role="alert"
    >
      {{ issueMessage }}
    </p>
    <SelectField
      v-if="modelValue && reason === null"
      class="publish-showcase-role ml-10 mt-2"
      :control-attrs="{ 'data-action': 'publish-showcase-role' }"
      :disabled="busy"
      :hint="showcase.roleHint"
      :label="showcase.roleLabel"
      :model-value="role"
      name="showcaseRole"
      :options="roleOptions"
      @update:model-value="(value) => emit('update:role', value)"
    />
  </div>
</template>

<style scoped>
.publish-showcase-role :deep([data-slot="native-select-wrapper"]) {
  width: 100%;
}
</style>
