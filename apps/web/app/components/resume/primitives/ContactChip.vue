<script setup lang="ts">
import type { PersonalDetail } from '@aboutme/schema';
import { computed } from 'vue';

import Icon from './Icon.vue';

const props = defineProps<{
  detail: PersonalDetail;
  iconStyle: 'none' | 'outline';
}>();

const labels: Record<PersonalDetail['type'], string> = {
  email: 'Email',
  phone: 'Phone',
  location: 'Location',
  website: 'Website',
  linkedin: 'LinkedIn',
  github: 'GitHub',
  twitter: 'Twitter',
  custom: 'Detail',
};
const iconKeys: Record<PersonalDetail['type'], string> = {
  email: 'mail',
  phone: 'phone',
  location: 'map-pin',
  website: 'globe',
  linkedin: 'linkedin',
  github: 'github',
  twitter: 'twitter',
  custom: 'user',
};
const linkTypes = new Set<PersonalDetail['type']>([
  'website',
  'linkedin',
  'github',
  'twitter',
]);
const isLink = computed(
  () =>
    linkTypes.has(props.detail.type)
    && props.detail.value.startsWith('https://'),
);
const label = computed(() => props.detail.label || labels[props.detail.type]);
// An icon already names a typed contact, so its default label would repeat
// it. A user-set label, a custom detail's label, or an icon-free header keep
// the label.
const showLabel = computed(
  () =>
    props.iconStyle === 'none'
    || props.detail.type === 'custom'
    || Boolean(props.detail.label),
);
// A link shows its address without the scheme or a trailing slash; the href
// keeps the full URL.
const shownValue = computed(() => {
  if (!isLink.value) return props.detail.value;
  const shown = props.detail.value.slice('https://'.length).replace(/\/$/u, '');
  return shown === '' ? props.detail.value : shown;
});
</script>

<template>
  <span class="contact-chip">
    <Icon
      v-if="iconStyle === 'outline'"
      :icon-key="iconKeys[detail.type]"
    />
    <span
      v-if="showLabel"
      class="contact-label"
    >{{ label }}:</span>
    <a
      v-if="isLink"
      :href="detail.value"
      rel="noopener noreferrer"
      style="color: var(--color-link); text-decoration: underline"
    >{{ shownValue }}</a>
    <span v-else>{{ detail.value }}</span>
  </span>
</template>
