<script setup lang="ts">
import { nextTick, ref } from 'vue';

import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import { Button } from '@/components/ui/button';
import type { EditorFieldsCopy } from '@/i18n/editor-fields';

// The note above a linked entry's fields (docs/design/ui/projects.md "In
// the resume editor"). Refresh asks first, because it replaces text the
// owner may have shortened for this resume.
defineProps<{
  readonly projectId: string;
  readonly name: string;
  /** The default language's name when the project page falls back to it. */
  readonly fallbackLanguage: string | null;
  readonly refreshing: boolean;
  readonly copy: EditorFieldsCopy['projectLinks'];
  readonly cancelLabel: string;
}>();
const emit = defineEmits<{ refresh: [] }>();
const asking = ref(false);
const note = ref<HTMLElement>();

async function confirm(): Promise<void> {
  asking.value = false;
  emit('refresh');
  await nextTick();
  note.value
    ?.querySelector<HTMLElement>('[data-action="open-project"]')?.focus();
}
</script>

<template>
  <div
    ref="note"
    class="grid gap-2 rounded-md bg-surface-blue p-3 text-sm text-foreground"
    data-project-note
  >
    <p class="break-words">
      {{ copy.note(name) }}
    </p>
    <p
      v-if="fallbackLanguage !== null"
      data-project-fallback
    >
      {{ copy.pageUses(fallbackLanguage) }}
    </p>
    <div class="flex flex-wrap gap-2">
      <Button
        data-action="refresh-project-entry"
        :disabled="refreshing"
        size="sm"
        type="button"
        variant="outline"
        @click="asking = true"
      >
        {{ copy.refresh }}
      </Button>
      <Button
        as-child
        size="sm"
        variant="ghost"
      >
        <NuxtLink
          data-action="open-project"
          :to="`/app/projects/${encodeURIComponent(projectId)}`"
        >
          {{ copy.openProject }}
        </NuxtLink>
      </Button>
    </div>
    <ConfirmDialog
      v-if="asking"
      :open="true"
      :title="copy.refresh"
      :description="copy.refreshQuestion"
      :confirm-label="copy.replace"
      :cancel-label="cancelLabel"
      confirm-action="confirm-project-refresh"
      cancel-action="cancel-project-refresh"
      @confirm="confirm"
      @cancel="asking = false"
    />
  </div>
</template>
