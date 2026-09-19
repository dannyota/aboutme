<script setup lang="ts">
import { computed } from 'vue';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { editorShellCopy } from '../../i18n/editor-shell';

const props = defineProps<{
  readonly estimatedPages: number | null;
  readonly zoom: 'fit' | 'full';
  readonly photoState: 'ready' | 'loading' | 'unavailable' | 'none';
}>();
const emit = defineEmits<{
  'update:zoom': [zoom: 'fit' | 'full'];
  'openPhoto': [];
}>();
const { locale } = useLocale();
const copy = computed(() => editorShellCopy[locale.value]);

const photoText = (): string =>
  props.photoState === 'loading'
    ? copy.value.photoLoading
    : props.photoState === 'unavailable'
      ? copy.value.photoUnavailable
      : '';

function onZoom(value: unknown): void {
  if (value === 'fit' || value === 'full') emit('update:zoom', value);
}
</script>

<template>
  <div
    class="flex min-h-11 items-center gap-3 border-b border-border bg-card px-4
      text-sm"
  >
    <h2
      id="editor-preview-title"
      class="font-semibold"
    >
      {{ copy.preview }}
    </h2>
    <p class="flex items-center gap-1.5 text-muted-foreground">
      <span data-estimated-pages-label>{{ copy.estimatedPages }}</span>
      <output :aria-label="copy.estimatedPageCount">
        {{ estimatedPages ?? "—" }}
      </output>
    </p>
    <template v-if="photoText() !== ''">
      <Badge
        data-photo-state
        role="status"
        variant="outline"
      >
        {{ photoText() }}
      </Badge>
      <Button
        size="sm"
        variant="link"
        @click="emit('openPhoto')"
      >
        {{ copy.openPhotoPanel }}
      </Button>
    </template>
    <ToggleGroup
      :aria-label="copy.previewZoom"
      class="ml-auto"
      :model-value="zoom"
      size="sm"
      type="single"
      variant="outline"
      @update:model-value="onZoom"
    >
      <ToggleGroupItem value="fit">
        {{ copy.fit }}
      </ToggleGroupItem>
      <ToggleGroupItem value="full">
        100%
      </ToggleGroupItem>
    </ToggleGroup>
  </div>
</template>
