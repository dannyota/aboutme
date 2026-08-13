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
</script>

<template>
  <span class="contact-chip">
    <Icon
      v-if="iconStyle === 'outline'"
      :icon-key="iconKeys[detail.type]"
    />
    <span class="contact-label">{{ label }}:</span>
    <a
      v-if="isLink"
      :href="detail.value"
      rel="noopener noreferrer"
      style="color: var(--color-link); text-decoration: underline"
    >{{ detail.value }}</a>
    <span v-else>{{ detail.value }}</span>
  </span>
</template>
