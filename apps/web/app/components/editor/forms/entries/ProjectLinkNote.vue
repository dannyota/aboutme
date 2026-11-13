<script setup lang="ts">
import { nextTick, ref, watch } from 'vue';

import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import { Button } from '@/components/ui/button';
import type { EditorFieldsCopy } from '@/i18n/editor-fields';

const props = defineProps<{
  readonly projectId: string;
  readonly name: string;
  readonly fallbackLanguage: string | null;
  readonly refreshing: boolean;
  readonly copy: EditorFieldsCopy['projectLinks'];
  readonly cancelLabel: string;
}>();
const emit = defineEmits<{ refresh: [] }>();
const asking = ref(false);
const refreshButton = ref<{ $el?: HTMLElement } | null>(null);
const openLink = ref<{ $el?: HTMLElement } | null>(null);
let returning: 'confirmed' | 'queued' | null = null;

function confirm(): void {
  asking.value = false;
  returning = 'confirmed';
  emit('refresh');
}

watch(() => props.refreshing, async (refreshing) => {
  if (returning === null) return;
  await nextTick();
  if (refreshing) {
    returning = 'queued';
    openLink.value?.$el?.focus();
  } else if (returning === 'queued') {
    returning = null;
    const link = openLink.value?.$el;
    if (link !== undefined && document.activeElement === link) {
      refreshButton.value?.$el?.focus();
    }
  }
}, { flush: 'post' });
</script>

<template>
  <div
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
        ref="refreshButton"
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
          ref="openLink"
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
