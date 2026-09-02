<script setup lang="ts">
import { CheckCircle2, CircleAlert, CloudOff, LoaderCircle } from '@lucide/vue';
import { computed } from 'vue';

import { Badge } from '@/components/ui/badge';
import type { SaveState } from '../../editor/types';

const props = defineProps<{ readonly state: SaveState }>();

const text = computed<string>(() => {
  switch (props.state) {
    case 'idle':
      return 'Ready';
    case 'dirty':
      return 'Unsaved changes';
    case 'saving':
      return 'Saving';
    case 'saved':
      return 'Saved';
    case 'offline':
      return 'Offline — changes retained';
    case 'error':
      return 'Save needs attention';
    case 'conflict':
      return 'Review a conflict';
    case 'session-lost':
      return 'Sign in to continue';
    default:
      return assertNever(props.state);
  }
});

function assertNever(value: never): never {
  throw new Error(`Unexpected save state: ${String(value)}`);
}
</script>

<template>
  <Badge
    :class="state === 'saved' || state === 'idle' ? 'text-positive' : undefined"
    :data-state="state"
    role="status"
    variant="outline"
  >
    <LoaderCircle
      v-if="state === 'saving'"
      :size="16"
      aria-hidden="true"
    />
    <CloudOff
      v-else-if="state === 'offline'"
      :size="16"
      aria-hidden="true"
    />
    <CircleAlert
      v-else-if="
        state === 'error' || state === 'conflict' || state === 'session-lost'
      "
      :size="16"
      aria-hidden="true"
    />
    <CheckCircle2
      v-else
      :size="16"
      aria-hidden="true"
    />
    <span>{{ text }}</span>
  </Badge>
</template>
