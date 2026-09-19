<script setup lang="ts">
import type { PersonalDetail } from '@aboutme/schema';
import { computed } from 'vue';

import { contactHref } from '../contactHref';
import { useResumeLng } from '../formatDate';
import Icon from './Icon.vue';

const props = defineProps<{
  detail: PersonalDetail;
  iconStyle: 'none' | 'outline';
}>();

type Labels = Record<PersonalDetail['type'], string>;
const EN_LABELS: Labels = {
  email: 'Email',
  phone: 'Phone',
  location: 'Location',
  website: 'Website',
  linkedin: 'LinkedIn',
  github: 'GitHub',
  twitter: 'X',
  custom: 'Detail',
};
// Default labels follow the resume language; a user label is kept as typed.
const VI_LABELS: Labels = {
  ...EN_LABELS,
  phone: 'Điện thoại',
  location: 'Địa chỉ',
  custom: 'Thông tin',
};
const lng = useResumeLng();
const labels = computed(() =>
  lng.value.toLowerCase().split('-')[0] === 'vi' ? VI_LABELS : EN_LABELS);
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
const href = computed(() => contactHref(props.detail));
// Display modes apply to web addresses only; an email or phone link always
// shows its value (ADR 0043).
const isLink = computed(() => href.value?.startsWith('https://') ?? false);
const iconKey = computed(() =>
  props.detail.type === 'custom' && isLink.value
    ? 'link'
    : iconKeys[props.detail.type]);
const label = computed(() =>
  props.detail.label || labels.value[props.detail.type]);
const display = computed(() =>
  isLink.value ? props.detail.display ?? 'short' : 'short');
// An icon already names a typed contact, so its default label would repeat
// it. A user-set label, a custom detail's label, or an icon-free header keep
// the label. In label display the label is the anchor text instead.
const showLabel = computed(
  () =>
    display.value !== 'label'
    && (props.iconStyle === 'none'
      || props.detail.type === 'custom'
      || Boolean(props.detail.label)),
);
// Short display drops the scheme and one trailing slash; the href keeps the
// full URL.
const shownValue = computed(() => {
  if (!isLink.value) return props.detail.value;
  if (display.value === 'label') return label.value;
  if (display.value === 'full') return props.detail.value;
  const shown = props.detail.value.slice('https://'.length).replace(/\/$/u, '');
  return shown === '' ? props.detail.value : shown;
});
</script>

<template>
  <span class="contact-chip">
    <Icon
      v-if="iconStyle === 'outline'"
      :icon-key="iconKey"
    />
    <span
      v-if="showLabel"
      class="contact-label"
    >{{ label }}:</span>
    <a
      v-if="href !== null"
      :href="href"
      rel="noopener noreferrer"
      style="color: var(--color-link); text-decoration: underline"
    >{{ shownValue }}</a>
    <span v-else>{{ detail.value }}</span>
  </span>
</template>
