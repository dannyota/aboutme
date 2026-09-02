<script setup lang="ts">
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';

const props = defineProps<{
  readonly estimatedPages: number | null;
  readonly zoom: 'fit' | 'full';
  readonly photoState: 'ready' | 'loading' | 'unavailable' | 'none';
}>();
const emit = defineEmits<{
  'update:zoom': [zoom: 'fit' | 'full'];
  'openPhoto': [];
}>();

const photoText = (): string =>
  props.photoState === 'loading'
    ? 'Photo is loading. The preview is shown without it.'
    : props.photoState === 'unavailable'
      ? 'Photo unavailable. The preview is shown without it.'
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
      Preview
    </h2>
    <p class="flex items-center gap-1.5 text-muted-foreground">
      <span data-estimated-pages-label>Estimated pages</span>
      <output aria-label="Estimated page count">
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
        Open photo panel
      </Button>
    </template>
    <ToggleGroup
      aria-label="Preview zoom"
      class="ml-auto"
      :model-value="zoom"
      size="sm"
      type="single"
      variant="outline"
      @update:model-value="onZoom"
    >
      <ToggleGroupItem value="fit">
        Fit
      </ToggleGroupItem>
      <ToggleGroupItem value="full">
        100%
      </ToggleGroupItem>
    </ToggleGroup>
  </div>
</template>
