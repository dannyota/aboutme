<script setup lang="ts">
import type { PersonalDetail } from '@aboutme/schema';
import { computed } from 'vue';

import { contactHref } from '../contactHref';
import { useResumeLng } from '../formatDate';
import Icon from './Icon.vue';

const props = withDefaults(defineProps<{
  detail: PersonalDetail;
  iconStyle: 'none' | 'outline';
  row?: boolean;
}>(), { row: false });

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
const isLink = computed(() => href.value?.startsWith('https://') ?? false);
const iconKey = computed(() =>
  props.detail.type === 'custom' && isLink.value
    ? 'link'
    : iconKeys[props.detail.type]);
const label = computed(() =>
  props.detail.label || labels.value[props.detail.type]);
const display = computed(() =>
  isLink.value ? props.detail.display ?? 'short' : 'short');
const showLabel = computed(
  () =>
    display.value !== 'label'
    && (props.iconStyle === 'none'
      || props.detail.type === 'custom'
      || Boolean(props.detail.label)),
);
const shownValue = computed(() => {
  if (!isLink.value) return props.detail.value;
  if (display.value === 'label') return label.value;
  if (display.value === 'full') return props.detail.value;
  const shown = props.detail.value.slice('https://'.length).replace(/\/$/u, '');
  return shown === '' ? props.detail.value : shown;
});
</script>

<template>
  <div
    v-if="row"
    class="contact-chip contact-chip--sheets"
  >
    <span class="contact-label"><Icon
      v-if="iconStyle === 'outline'"
      :icon-key="iconKey"
    />{{ label }}</span>
    <span class="contact-value"><a
      v-if="href !== null"
      :href="href"
      rel="noopener noreferrer"
      style="color: var(--color-link); text-decoration: underline"
    >{{ shownValue }}</a><template v-else>{{ detail.value }}</template></span>
  </div>
  <span
    v-else
    class="contact-chip"
  >
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
