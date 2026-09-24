<script setup lang="ts">
/**
 * `TemplateCard` — one template in the gallery: page one of its sample (or
 * the generic filler), its name, purpose, and a tag naming what is shown. The
 * whole card links to the template page.
 */
import type { Resume } from '@aboutme/schema';
import { onMounted, ref, watch } from 'vue';

import type { Locale } from '../../i18n/locale';
import type { GalleryTemplate } from '../../templates/catalog';
import { galleryDocument } from '../../templates/documents';
import SheetThumbnail from './SheetThumbnail.vue';

const props = defineProps<{
  readonly template: GalleryTemplate;
  readonly locale: Locale;
  readonly illustrative: string;
}>();

const document = ref<Resume>();

async function load(): Promise<void> {
  document.value = (await galleryDocument(props.template, props.locale))
    .document;
}

onMounted(load);
watch(() => props.locale, load);
</script>

<template>
  <NuxtLink
    class="template-card group grid content-start gap-3 rounded-md
      focus-visible:outline-none focus-visible:ring-2
      focus-visible:ring-ring focus-visible:ring-offset-4
      focus-visible:ring-offset-background"
    :data-template="template.id"
    :to="`/templates/${template.id}`"
  >
    <SheetThumbnail
      class="transition-transform duration-200 ease-out
        group-hover:-translate-y-1 motion-reduce:transition-none
        motion-reduce:group-hover:translate-y-0"
      :document="document"
      :lng="locale"
    />
    <span class="grid gap-1">
      <span class="text-base font-semibold text-foreground">
        {{ template.name }}
      </span>
      <span class="line-clamp-2 text-sm text-muted-foreground">
        {{ template.purpose[locale] }}
      </span>
    </span>
    <span
      :class="template.sampleTags
        ? 'rounded-full bg-surface-blue px-2.5 py-0.5 text-xs font-medium '
          + 'text-foreground'
        : 'rounded-full bg-muted px-2.5 py-0.5 text-xs text-muted-foreground'"
      :data-tag-kind="template.sampleTags ? 'sample' : 'illustrative'"
      class="justify-self-start"
      data-template-tag
    >{{ template.sampleTags?.[locale]?.[locale] ?? illustrative }}</span>
  </NuxtLink>
</template>
