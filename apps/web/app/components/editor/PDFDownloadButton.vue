<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue';

import { Button } from '@/components/ui/button';
import type { PdfDownloadController } from '../../editor/pdfDownload';
import { pageSizeHelp } from '../../i18n/editor-controls';
import { pdfCopy } from '../../i18n/pdf';
import {
  PAGE_SIZE_SHORT,
  type PageFormat,
} from './customization/pageSettings';

const props = defineProps<{
  readonly controller: PdfDownloadController;
  /** The page size the PDF uses, shown beside the label. */
  readonly pageFormat?: PageFormat;
}>();

const pending = computed(() => props.controller.state.value.kind === 'pending');
const { locale } = useLocale();
const copy = computed(() => pdfCopy[locale.value]);
const pageSizeShort = computed(() => props.pageFormat === undefined
  ? undefined
  : PAGE_SIZE_SHORT[props.pageFormat]);
const message = computed(() => {
  const state = props.controller.state.value;
  return state.kind === 'pending'
    ? copy.value.downloading
    : state.kind === 'error'
      ? copy.value.error[state.code] ?? copy.value.error.generic
      : '';
});

function download(): void {
  void props.controller.download();
}

onBeforeUnmount(() => props.controller.dispose());
</script>

<template>
  <Button
    :aria-label="pageSizeShort === undefined
      ? copy.download
      : copy.ariaDownload(pageSizeShort)"
    class="editor-download-pdf-action"
    data-action="download-pdf"
    :disabled="pending"
    size="sm"
    type="button"
    :title="pageFormat === undefined
      ? undefined
      : pageSizeHelp(locale, pageFormat)"
    variant="outline"
    @click="download"
  >
    {{ pending ? copy.downloading : copy.download }}
    <span
      v-if="pageFormat !== undefined"
      aria-hidden="true"
      class="font-normal text-muted-foreground"
      data-download-pdf-size
    >{{ pageSizeShort }}</span>
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
