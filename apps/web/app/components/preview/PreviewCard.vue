<script setup lang="ts">
/**
 * `PreviewCard`: the 1200 by 630 link-preview card
 * (docs/design/link-preview-card.md). The print worker renders it for the
 * stored card image, and the publish dialog renders it scaled down. It shows
 * only the card content, never contact details.
 */
import { computed } from 'vue';

import AppLogo from '../app/AppLogo.vue';
import {
  accentTints,
  CARD_LINK_PREFIX,
  CARD_SITE,
  fitHeadline,
  fitName,
  footerSize,
  linkSize,
} from './cardFit';
import type { PreviewCardContent } from './cardLayout';

const props = defineProps<{ card: PreviewCardContent }>();

const name = computed(() =>
  props.card.name === null ? null : fitName(props.card.name));
const headline = computed(() =>
  props.card.name === null || props.card.headline === null
    ? null
    : fitHeadline(props.card.headline));

// With no name the address already fills the name place, so the footer
// names the site alone.
const footerText = computed(() =>
  props.card.name === null
    ? CARD_SITE
    : CARD_LINK_PREFIX + props.card.slug);

const percent = (value: number): string =>
  `${Math.round(value * 100 * 1_000_000) / 1_000_000}%`;

// The resume photo's crop math (components/resume/primitives/Photo.vue): the
// image box maps the whole image onto the circle so the crop fills it.
const imageStyle = computed(() => {
  const crop = props.card.photo?.crop ?? { x: 0, y: 0, width: 1, height: 1 };
  return {
    left: percent(-crop.x / crop.width),
    top: percent(-crop.y / crop.height),
    width: percent(1 / crop.width),
    height: percent(1 / crop.height),
  };
});

const rootStyle = computed(() => {
  const tints = accentTints(props.card.accent);
  return {
    '--card-accent': props.card.accent,
    '--card-band-outer': tints.bandOuter,
    '--card-band-inner': tints.bandInner,
    '--card-dot': tints.dot,
  };
});
</script>

<template>
  <div
    class="preview-card"
    :lang="card.lng"
    :style="rootStyle"
  >
    <div
      aria-hidden="true"
      class="preview-card-band preview-card-band-left"
    />
    <div
      aria-hidden="true"
      class="preview-card-band preview-card-band-right"
    />
    <div class="preview-card-body">
      <div
        v-if="card.photo !== null"
        class="preview-card-photo"
      >
        <span class="preview-card-photo-clip">
          <img
            alt=""
            class="preview-card-photo-image"
            :src="card.photo.url"
            :style="imageStyle"
          >
        </span>
      </div>
      <p
        v-if="name !== null"
        class="preview-card-name"
        :style="{ fontSize: `${name.size}px` }"
      >
        {{ name.text }}
      </p>
      <p
        v-else
        class="preview-card-name preview-card-link"
        :style="{ fontSize: `${linkSize(card.slug)}px` }"
      >
        <span class="preview-card-link-prefix">{{ CARD_LINK_PREFIX }}</span>
        <span class="preview-card-link-slug">{{ card.slug }}</span>
      </p>
      <p
        v-if="headline !== null"
        class="preview-card-headline"
      >
        {{ headline }}
      </p>
    </div>
    <div class="preview-card-footer">
      <AppLogo
        mark-only
        size="md"
      />
      <p
        class="preview-card-footer-link"
        :style="{ fontSize: `${footerSize(footerText)}px` }"
      >
        {{ footerText }}
      </p>
    </div>
  </div>
</template>

<style src="./preview-card.css"></style>
