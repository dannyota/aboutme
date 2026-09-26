<script setup lang="ts">
import type { components } from '@/api/generated/openapi';
import { platformNames } from '@/i18n/views';
import type { ViewsDetailCopy } from '@/i18n/views';

type ViewPreviews = components['schemas']['ViewPreviews'];

const props = defineProps<{
  readonly previews: readonly ViewPreviews[];
  readonly copy: ViewsDetailCopy;
}>();
</script>

<template>
  <section
    v-if="props.previews.length > 0"
    data-testid="views-link-previews"
  >
    <ul class="space-y-1 text-sm">
      <li
        v-for="item in props.previews"
        :key="item.platform"
      >
        {{ props.copy.previewLine(platformNames[item.platform], item.fetches) }}
      </li>
    </ul>
    <p class="mt-2 text-xs text-muted-foreground">
      {{ props.copy.previewsNote }}
    </p>
  </section>
</template>
