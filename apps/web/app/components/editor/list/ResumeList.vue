<script setup lang="ts">
import type { ResumeSummary } from '../../../editor/resumeApi';

const props = defineProps<{
  items: readonly ResumeSummary[];
  busyIds: readonly string[];
  removalFocusId: string | null;
  removalFocusVersion: number;
}>();
const root = ref<HTMLElement | null>(null);

watch(() => props.removalFocusVersion, () => {
  void nextTick(() => {
    const selector = props.removalFocusId === null
      ? '[data-testid="create-resume"]'
      : `[data-testid="resume-row-${props.removalFocusId}"] button`;
    root.value?.querySelector<HTMLElement>(selector)?.focus();
  });
});

defineEmits<{
  create: [];
  rename: [item: ResumeSummary];
  remove: [item: ResumeSummary];
}>();
</script>

<template>
  <section
    ref="root"
    class="resume-list"
    aria-labelledby="resume-list-title"
  >
    <div>
      <h1 id="resume-list-title">
        Resumes
      </h1>
      <button
        type="button"
        data-testid="create-resume"
        @click="$emit('create')"
      >
        Create resume
      </button>
    </div>
    <p
      v-if="items.length === 0"
      role="status"
    >
      No resumes yet.
    </p>
    <ul
      v-else
      aria-label="Your resumes"
    >
      <li
        v-for="item in items"
        :key="item.id"
        :data-testid="`resume-row-${item.id}`"
      >
        <NuxtLink :to="`/app/resumes/${encodeURIComponent(item.id)}`">
          {{ item.title }}
        </NuxtLink>
        <button
          type="button"
          :disabled="busyIds.includes(item.id)"
          :aria-label="`Rename ${item.title}`"
          @click="$emit('rename', item)"
        >
          Rename
        </button>
        <button
          type="button"
          :disabled="busyIds.includes(item.id)"
          :aria-label="`Delete ${item.title}`"
          @click="$emit('remove', item)"
        >
          Delete
        </button>
      </li>
    </ul>
  </section>
</template>
