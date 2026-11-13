<script setup lang="ts">
import type { GuideInline } from '@/i18n/guide';

defineProps<{
  readonly line: readonly GuideInline[];
}>();
</script>

<template>
  <template
    v-for="(part, index) in line"
    :key="index"
  >
    <span v-if="part.kind === 'text'">{{ part.text }}</span>
    <span
      v-else-if="part.kind === 'strong'"
      class="font-semibold"
    >{{ part.text }}</span>
    <code
      v-else-if="part.kind === 'code'"
      class="rounded-sm bg-muted px-1 py-0.5 font-mono text-[13px]"
      translate="no"
    >{{ part.text }}</code>
    <a
      v-else-if="part.kind === 'link' && part.external"
      class="text-link underline-offset-4 hover:underline"
      :href="part.href"
      rel="noopener noreferrer"
    >{{ part.text }}</a>
    <NuxtLink
      v-else-if="part.kind === 'link'"
      class="text-link underline-offset-4 hover:underline"
      :to="part.href"
    >{{ part.text }}</NuxtLink>
  </template>
</template>
