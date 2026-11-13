<script setup lang="ts">
import { computed } from 'vue';

import { Button } from '@/components/ui/button';

import type { SessionCheck } from '../../composables/sessionCheck';
import StatusBanner from '../app/StatusBanner.vue';
import { editorShellCopy } from '../../i18n/editor-shell';

const props = defineProps<{
  readonly sessionCheck: SessionCheck;
}>();
const { locale } = useLocale();
const copy = computed(() => editorShellCopy[locale.value]);

function retry(): void {
  void props.sessionCheck.retry();
}
</script>

<template>
  <StatusBanner
    data-session-check
    kind="error"
    :title="copy.sessionCheckTitle"
  >
    <p>{{ copy.sessionCheckDescription }}</p>
    <Button
      data-action="retry-session-check"
      :disabled="sessionCheck.busy.value"
      size="sm"
      type="button"
      @click="retry"
    >
      {{ copy.tryAgain }}
    </Button>
  </StatusBanner>
</template>
