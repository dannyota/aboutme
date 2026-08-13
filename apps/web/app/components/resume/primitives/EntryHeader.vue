<script setup lang="ts">
defineProps<{
  title?: string;
  titleLink?: string;
  subtitle?: string;
  subtitleLink?: string;
  meta?: readonly string[];
}>();
</script>

<template>
  <header
    v-if="
      title
        || subtitle
        || (meta && meta.length > 0)
        || $slots['meta-widget']
    "
    class="entry-header"
  >
    <div
      v-if="title"
      class="entry-title"
    >
      <a
        v-if="titleLink"
        :href="titleLink"
        rel="noopener noreferrer"
        style="color: var(--color-link); text-decoration: underline"
      >{{ title }}</a>
      <strong v-else>{{ title }}</strong>
    </div>
    <div
      v-if="subtitle"
      class="entry-subtitle"
    >
      <a
        v-if="subtitleLink"
        :href="subtitleLink"
        rel="noopener noreferrer"
        style="color: var(--color-link); text-decoration: underline"
      >{{ subtitle }}</a>
      <span v-else>{{ subtitle }}</span>
    </div>
    <div
      v-if="(meta && meta.length > 0) || $slots['meta-widget']"
      class="entry-meta"
    >
      <template v-if="meta && meta.length > 0">
        <template
          v-for="(value, index) in meta"
          :key="index"
        >
          <span
            v-if="index > 0"
            aria-hidden="true"
          > · </span>
          <span>{{ value }}</span>
        </template>
      </template>
      <slot name="meta-widget" />
    </div>
  </header>
</template>
