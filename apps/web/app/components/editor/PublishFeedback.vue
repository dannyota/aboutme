<script setup lang="ts">
import { computed } from 'vue';

import { Button } from '@/components/ui/button';
import type { PublishControllerState } from '../../editor/publishController';
import {
  blockedMessage,
  failureMessage,
  issueMessage,
} from '../../editor/publishMessages';
import { isShowcaseIssuePath } from '../../editor/publishShowcase';
import { publicPageIssueField } from '../../editor/publicPageMeta';
import type { PublishCopy } from '../../i18n/publish';

const props = defineProps<{
  readonly state: PublishControllerState;
  readonly copy: PublishCopy;
  readonly busy: boolean;
}>();
const emit = defineEmits<{ 'focus-issue': [path: string] }>();

// Field issues stay with their controls; other issues keep their list.
const listedIssues = computed(() =>
  props.state.kind === 'invalid'
    ? props.state.issues.filter((issue) =>
        publicPageIssueField(issue.path) === null
        && !isShowcaseIssuePath(issue.path))
    : []);
</script>

<template>
  <p
    v-if="state.kind === 'blocked'"
    role="alert"
  >
    {{ blockedMessage(copy, state.reason) }}
  </p>
  <div
    v-if="listedIssues.length > 0"
    role="alert"
    class="grid gap-2"
  >
    <p>{{ copy.invalid }}</p>
    <Button
      v-for="issue in listedIssues"
      :key="`${issue.path}-${issue.code}`"
      type="button"
      variant="outline"
      data-action="focus-publish-issue"
      :disabled="busy"
      @click="emit('focus-issue', issue.path)"
    >
      {{ issueMessage(copy, issue.code) }}
    </Button>
  </div>
  <p
    v-if="state.kind === 'slug-taken'"
    role="alert"
  >
    {{ copy.slugTaken }}
  </p>
  <p
    v-if="state.kind === 'stale'"
    role="alert"
  >
    {{ copy.stale }}
  </p>
  <p
    v-if="
      state.kind === 'rate-limited' || state.kind === 'public-state-busy'
    "
    role="alert"
  >
    {{ copy.rateLimited }}
  </p>
  <p
    v-if="state.kind === 'unknown'"
    role="alert"
  >
    {{ copy.unknown }}
  </p>
  <p
    v-if="state.kind === 'session-lost'"
    role="alert"
  >
    {{ copy.sessionLost }}
  </p>
  <p
    v-if="state.kind === 'failed'"
    role="alert"
  >
    {{ failureMessage(copy, state.code) }}
  </p>
</template>
