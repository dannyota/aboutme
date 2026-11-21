<script setup lang="ts">
defineProps<{
  readonly title: string;
  readonly titleId: string;
  readonly description?: string;
}>();
</script>

<template>
  <section
    :aria-labelledby="titleId"
    class="flex flex-col gap-4"
  >
    <div
      v-if="$slots['heading-end']"
      class="flex items-baseline justify-between gap-2"
    >
      <h2
        :id="titleId"
        class="text-lg font-semibold"
      >
        {{ title }}
      </h2>
      <slot name="heading-end" />
    </div>
    <h2
      v-else
      :id="titleId"
      class="text-lg font-semibold"
    >
      {{ title }}
    </h2>
    <p
      v-if="description"
      class="text-muted-foreground"
    >
      {{ description }}
    </p>
    <div
      v-if="$slots.actions"
      class="flex items-center gap-2"
    >
      <slot name="actions" />
    </div>
    <slot />
  </section>
</template>
