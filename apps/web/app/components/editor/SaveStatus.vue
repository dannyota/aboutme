<script setup lang="ts">
import { computed } from 'vue';

import StateMark from '@/components/app/StateMark.vue';
import type { SaveState } from '../../editor/types';

const props = defineProps<{ readonly state: SaveState }>();

const mappedState = computed<'saved' | 'saving' | 'failed' | 'draft'>(() => {
  switch (props.state) {
    case 'idle':
    case 'saved':
      return 'saved';
    case 'saving':
      return 'saving';
    case 'dirty':
      return 'draft';
    case 'offline':
    case 'error':
    case 'conflict':
    case 'session-lost':
      return 'failed';
    default:
      return assertNever(props.state);
  }
});

function assertNever(value: never): never {
  throw new Error(`Unexpected save state: ${String(value)}`);
}
</script>

<template>
  <span
    :data-state="state"
    data-save-status
    role="status"
  >
    <StateMark :state="mappedState" />
  </span>
</template>
