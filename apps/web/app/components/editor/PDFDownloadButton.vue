<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue';

import { Button } from '@/components/ui/button';
import type { PdfDownloadController } from '../../editor/pdfDownload';

const props = defineProps<{ readonly controller: PdfDownloadController }>();

const pending = computed(() => props.controller.state.value.kind === 'pending');
const message = computed(() => {
  const state = props.controller.state.value;
  return state.kind === 'pending'
    ? 'Downloading PDF…'
    : state.kind === 'error'
      ? state.message
      : '';
});

function download(): void {
  void props.controller.download();
}

onBeforeUnmount(() => props.controller.dispose());
</script>

<template>
  <Button
    aria-label="Download PDF"
    class="editor-download-pdf-action"
    data-action="download-pdf"
    :disabled="pending"
    size="sm"
    type="button"
    variant="outline"
    @click="download"
  >
    {{ pending ? "Downloading PDF…" : "Download PDF" }}
  </Button>
  <p
    aria-live="polite"
    class="sr-only"
    data-download-pdf-status
    role="status"
  >
    {{ message }}
  </p>
</template>
