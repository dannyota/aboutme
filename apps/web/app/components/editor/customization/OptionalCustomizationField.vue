<script setup lang="ts">
defineProps<{
  readonly action: string;
  readonly label: string;
  readonly present: boolean;
}>();

const emit = defineEmits<{
  enable: [];
  unset: [];
}>();
</script>

<template>
  <fieldset :disabled="!present">
    <legend>{{ label }}</legend>
    <slot />
  </fieldset>
  <button
    v-if="!present"
    type="button"
    :data-action="`enable-${action}`"
    @click="emit('enable')"
  >
    Enable {{ label }}
  </button>
  <button
    v-else
    type="button"
    :data-action="`unset-${action}`"
    @click="emit('unset')"
  >
    Remove {{ label }}
  </button>
</template>
