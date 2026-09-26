<script setup lang="ts">
/**
 * `PublishPreview`: the publish dialog's link-preview section
 * (docs/design/link-preview-card.md, "Publish-dialog preview"). Renders the
 * same card component, scaled down inside a neutral chat card with the
 * preview title, description, and domain, from editor data with no server
 * call.
 */
import type { Resume } from '@aboutme/schema';
import { ChevronDown } from '@lucide/vue';
import {
  computed,
  onBeforeUnmount,
  ref,
  type ComponentPublicInstance,
} from 'vue';

import { Button } from '@/components/ui/button';
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible';
import { publishCopy } from '../../i18n/publish';
import {
  previewCardText,
  previewDescription,
  previewTitle,
  SITE_NAME,
} from '../../utils/previewText';
import { clampAgainst } from '../resume/clampContrast';
import {
  CARD_LAYOUT_VERSION,
  CARD_WIDTH,
  type PreviewCardContent,
} from '../preview/cardLayout';
import PreviewCard from '../preview/PreviewCard.vue';

const props = defineProps<{
  document: Resume;
  slug: string;
  pageTitle: string;
  lng: string;
  photoUrl?: string;
}>();

const { locale } = useLocale();
const copy = computed(() => publishCopy[locale.value].preview);

// Collapsed by default; the card and chat card mount only once expanded,
// since the dialog already scrolls and the photo makes this the heaviest
// part of it.
const open = ref(false);
const expanded = ref(false);

function setOpen(next: boolean): void {
  open.value = next;
  if (next) expanded.value = true;
}

const lng = computed(() => (props.lng === '' ? 'und' : props.lng));

const cardText = computed(() => previewCardText(props.document));
const photo = computed<PreviewCardContent['photo']>(() => {
  const source = props.document.personalDetails.photo;
  if (source === undefined || props.photoUrl === undefined) return null;
  return { url: props.photoUrl, crop: source.crop ?? null };
});
const accent = computed(() => {
  const colors = props.document.customization.colors;
  const base = (colors.accent ?? colors.primary).toLowerCase();
  return clampAgainst(base, ['#ffffff'], 3) ?? '#000000';
});
const card = computed<PreviewCardContent>(() => ({
  layoutVersion: CARD_LAYOUT_VERSION,
  lng: lng.value,
  slug: props.slug,
  name: cardText.value.name,
  headline: cardText.value.headline,
  photo: photo.value,
  accent: accent.value,
}));

const title = computed(() => {
  const trimmed = props.pageTitle.trim();
  return previewTitle(
    trimmed === '' ? null : trimmed,
    props.document,
    props.slug,
  );
});
const description = computed(() =>
  previewDescription(props.document, lng.value));

// k: the image area's width divided by 1200, read with a ResizeObserver. The
// card stays hidden until the first reading.
const scale = ref(0);
const measured = computed(() => scale.value > 0);
let observer: ResizeObserver | undefined;

function setImageArea(
  element: Element | ComponentPublicInstance | null,
): void {
  observer?.disconnect();
  observer = undefined;
  if (!(element instanceof Element)) return;
  if (typeof ResizeObserver === 'undefined') {
    scale.value = element.getBoundingClientRect().width / CARD_WIDTH;
    return;
  }
  observer = new ResizeObserver(([entry]) => {
    if (entry === undefined) return;
    scale.value = entry.contentRect.width / CARD_WIDTH;
  });
  observer.observe(element);
}

onBeforeUnmount(() => observer?.disconnect());
</script>

<template>
  <Collapsible
    v-slot="{ open: isOpen }"
    :open="open"
    @update:open="setOpen"
  >
    <CollapsibleTrigger as-child>
      <Button
        type="button"
        variant="ghost"
        class="-mx-2 flex h-9 w-[calc(100%+1rem)] items-center
          justify-between px-2 text-sm font-medium"
        data-action="toggle-publish-preview"
      >
        {{ copy.heading }}
        <ChevronDown
          aria-hidden="true"
          class="size-4 shrink-0 transition-transform duration-150
            motion-reduce:transition-none"
          :class="{ 'rotate-180': isOpen }"
        />
      </Button>
    </CollapsibleTrigger>
    <CollapsibleContent>
      <figure class="mt-4 grid gap-1.5">
        <div
          v-if="expanded"
          class="w-full max-w-[360px] overflow-hidden rounded-[10px]
            border bg-muted text-left"
        >
          <div
            :ref="setImageArea"
            class="relative overflow-hidden border-b"
            :style="{
              aspectRatio: '1200 / 630',
              backgroundColor: '#fff',
              visibility: measured ? 'visible' : 'hidden',
            }"
          >
            <div
              aria-hidden="true"
              class="absolute left-0 top-0 origin-top-left"
              data-testid="publish-preview-card"
              :style="{
                width: `${CARD_WIDTH}px`,
                height: '630px',
                transform: `scale(${scale})`,
              }"
            >
              <PreviewCard :card="card" />
            </div>
          </div>
          <div
            class="grid gap-0.5 px-3 py-2.5"
            data-testid="publish-preview-text"
          >
            <p class="line-clamp-2 text-sm font-semibold text-foreground">
              {{ title }}
            </p>
            <p class="line-clamp-2 text-sm text-muted-foreground">
              {{ description }}
            </p>
            <p class="text-xs text-muted-foreground">
              {{ SITE_NAME }}
            </p>
          </div>
        </div>
        <figcaption class="text-xs text-muted-foreground">
          {{ copy.caption }}
        </figcaption>
      </figure>
    </CollapsibleContent>
  </Collapsible>
</template>
