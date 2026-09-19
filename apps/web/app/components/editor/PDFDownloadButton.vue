<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue';

import { Button } from '@/components/ui/button';
import type { PdfDownloadController } from '../../editor/pdfDownload';
import {
  PAGE_SIZE_LABELS,
  PAGE_SIZE_SHORT,
  type PageFormat,
} from './customization/pageSettings';

const props = defineProps<{
  readonly controller: PdfDownloadController;
  /** The page size the PDF uses, shown beside the label. */
  readonly pageFormat?: PageFormat;
}>();

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
    :aria-label="pageFormat === undefined
      ? 'Download PDF'
      : `Download PDF, ${PAGE_SIZE_SHORT[pageFormat]}`"
    class="editor-download-pdf-action"
    data-action="download-pdf"
    :disabled="pending"
    size="sm"
    type="button"
    :title="pageFormat === undefined
      ? undefined
      : `PDF page size: ${PAGE_SIZE_LABELS[pageFormat]}`"
    variant="outline"
    @click="download"
  >
    {{ pending ? "Downloading PDF…" : "Download PDF" }}
    <span
      v-if="pageFormat !== undefined"
      aria-hidden="true"
      class="font-normal text-muted-foreground"
      data-download-pdf-size
    >{{ PAGE_SIZE_SHORT[pageFormat] }}</span>
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
