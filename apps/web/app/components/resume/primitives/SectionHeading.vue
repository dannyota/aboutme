<script setup lang="ts">
import Icon from './Icon.vue';

defineProps<{ displayName?: string; iconKey?: string }>();
</script>

<template>
  <header class="section-heading">
    <Icon
      v-if="iconKey"
      :icon-key="iconKey"
    />
    <h2 v-if="displayName">
      {{ displayName }}
    </h2>
  </header>
</template>
