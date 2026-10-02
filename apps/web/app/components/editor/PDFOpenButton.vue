<script setup lang="ts">
import { computed } from 'vue';

import { Button } from '@/components/ui/button';
import type { PdfDownloadController } from '../../editor/pdfDownload';
import { pdfCopy } from '../../i18n/pdf';

const props = defineProps<{
  readonly controller: PdfDownloadController;
}>();

const pending = computed(() => props.controller.state.value.kind === 'pending');
const { locale } = useLocale();
const copy = computed(() => pdfCopy[locale.value]);
const message = computed(() => {
  const state = props.controller.state.value;
  if (state.kind === 'pending') return copy.value.openingTab;
  if (state.kind !== 'error') return '';
  return state.code === 'save-required'
    ? copy.value.openSaveRequired
    : copy.value.error[state.code] ?? copy.value.error.generic;
});

function open(): void {
  void props.controller.openInTab();
}
</script>

<template>
  <Button
    class="editor-open-pdf-action"
    data-action="open-pdf"
    :disabled="pending"
    size="sm"
    type="button"
    variant="outline"
    @click="open"
  >
    {{ pending ? copy.openingTab : copy.openTab }}
  </Button>
  <p
    :class="message === '' ? 'sr-only' : 'text-sm text-muted-foreground'"
    data-open-pdf-status
    role="status"
  >
    {{ message }}
  </p>
</template>
