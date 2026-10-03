<script setup lang="ts">
import { Flag } from '@lucide/vue';
import { computed, ref } from 'vue';

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import type { Locale } from '@/i18n/locale';
import { showcaseCopy } from '@/i18n/showcase';
import {
  isShowcaseRole,
  type ShowcaseItem,
  showcaseCardUrl,
} from '@/lib/showcaseContract';
import { galleryTemplate } from '@/templates/catalog';

// One listing: the stored preview card, a meta block (optional role chip and
// language, then the template), and a footer row outside the tile link with
// the slug as plain text (S14) and the Report icon link. It shows only the
// closed item fields, always as text, and never a contact detail, a date, or
// a per-listing count (docs/design/showcase.md, What a listing shows;
// docs/design/ui/showcase.md, Tile).
const props = defineProps<{
  readonly item: ShowcaseItem;
  readonly locale: Locale;
}>();
const copy = computed(() => showcaseCopy[props.locale]);
const templateName = computed(() => {
  const id = props.item.templateId;
  return (id === null ? undefined : galleryTemplate(id)?.name)
    ?? copy.value.customDesign;
});
const roleLabel = computed(() => (
  isShowcaseRole(props.item.role) ? copy.value.roles[props.item.role] : null
));
const reportHref = computed(() =>
  `mailto:danny@aboutme.vn?subject=${
    encodeURIComponent(copy.value.reportSubject(props.item.slug))}`);
// A card can go missing in an open tab after a color change; the muted box
// stays and the alt text still names the tile.
const imageFailed = ref(false);
</script>

<template>
  <li
    class="showcase-tile"
    :data-showcase-slug="item.slug"
  >
    <NuxtLink
      class="showcase-tile__link"
      data-showcase-tile
      external
      rel="nofollow"
      :to="`/${item.slug}`"
    >
      <span class="showcase-tile__image">
        <img
          :alt="item.imageText"
          class="showcase-tile__img"
          :class="{ 'is-failed': imageFailed }"
          decoding="async"
          height="630"
          loading="lazy"
          :src="showcaseCardUrl(item)"
          width="1200"
          @error="imageFailed = true"
        >
      </span>
      <span class="showcase-tile__meta">
        <span class="showcase-tile__line">
          <span
            v-if="roleLabel !== null"
            class="showcase-tile__role"
            data-showcase-role
          >{{ roleLabel }}</span>
          <span class="showcase-tile__language">{{
            copy.languages[item.language]
          }}</span>
        </span>
        <span class="showcase-tile__template">{{ templateName }}</span>
      </span>
    </NuxtLink>
    <div class="showcase-tile__footer">
      <span class="showcase-tile__slug">aboutme.vn/{{ item.slug }}</span>
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger as-child>
            <a
              :aria-label="copy.reportLabel(item.slug)"
              class="showcase-tile__report-link"
              data-action="showcase-report"
              :href="reportHref"
            >
              <Flag
                aria-hidden="true"
                class="size-[18px]"
              />
            </a>
          </TooltipTrigger>
          <TooltipContent side="top">
            {{ copy.report }}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </div>
  </li>
</template>

<style scoped>
.showcase-tile {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: var(--radius-dialog);
  background: var(--card);
  box-shadow: var(--shadow-product);
  transition: transform 200ms ease-out;
}

.showcase-tile:has(.showcase-tile__link:hover) {
  transform: translateY(-4px);
}

@media (prefers-reduced-motion: reduce) {
  .showcase-tile {
    transition: none;
  }

  .showcase-tile:has(.showcase-tile__link:hover) {
    transform: none;
  }
}

.showcase-tile__link {
  display: block;
  border-radius: var(--radius-dialog);
}

.showcase-tile__link:focus-visible,
.showcase-tile__report-link:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}

.showcase-tile__image {
  display: block;
  aspect-ratio: 1200 / 630;
  overflow: hidden;
  border-bottom: 1px solid var(--border);
  border-radius: 13px 13px 0 0;
  background: var(--muted);
}

.showcase-tile__img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.showcase-tile__img.is-failed {
  opacity: 0;
}

.showcase-tile__meta {
  display: grid;
  gap: 8px;
  padding: 12px 16px 4px;
}

.showcase-tile__line {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.showcase-tile__role {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding: 0 10px;
  border-radius: 9999px;
  background: var(--surface-blue);
  color: var(--foreground);
  font-size: 0.75rem;
  font-weight: 600;
}

.showcase-tile__language {
  color: var(--muted-foreground);
  font-size: 0.875rem;
}

.showcase-tile__template {
  color: var(--foreground);
  font-size: 0.875rem;
  font-weight: 500;
}

.showcase-tile__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: auto;
  padding: 4px 8px 8px 16px;
}

.showcase-tile__slug {
  min-width: 0;
  overflow: hidden;
  color: var(--muted-foreground);
  font-size: 0.8125rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.showcase-tile__report-link {
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border-radius: 10px;
  color: var(--muted-foreground);
}

.showcase-tile__report-link:hover {
  background: var(--accent);
  color: var(--foreground);
}
</style>
