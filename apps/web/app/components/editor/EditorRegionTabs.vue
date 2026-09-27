<script setup lang="ts">
import { computed } from 'vue';
import { Button } from '@/components/ui/button';
import { editorShellCopy } from '../../i18n/editor-shell';

const region = defineModel<'editor' | 'preview'>({ required: true });
const { locale } = useLocale();
const copy = computed(() => editorShellCopy[locale.value]);
const tabs = computed(() => [
  { region: 'editor', action: 'show-editor', label: copy.value.edit },
  { region: 'preview', action: 'show-preview', label: copy.value.preview },
] as const);
</script>

<template>
  <!--
    Up to 72 rem the preview spans the editor area instead of holding its own
    column (DESIGN.md, Responsive behavior), so the bottom Edit/Preview tabs
    switch regions at every width below the desktop layout.
  -->
  <div
    :aria-label="copy.editorView"
    class="fixed inset-x-0 bottom-0 z-40 grid grid-cols-2 border-t
      bg-card p-2 pb-[calc(0.5rem+env(safe-area-inset-bottom))]
      min-[72rem]:hidden"
    role="tablist"
  >
    <Button
      v-for="tab in tabs"
      :key="tab.region"
      :aria-pressed="region === tab.region"
      :aria-selected="region === tab.region"
      class="aria-selected:text-link aria-selected:font-semibold"
      :data-action="tab.action"
      role="tab"
      type="button"
      :variant="region === tab.region ? 'secondary' : 'ghost'"
      @click="region = tab.region"
    >
      {{ tab.label }}
    </Button>
  </div>
</template>
